package service

import (
	"context"
	"log"
	"strconv"
	"time"

	"github.com/luhao/contextGraph/internal/model"
	emailpkg "github.com/luhao/contextGraph/pkg/email"
	apperr "github.com/luhao/contextGraph/pkg/errors"
	"github.com/luhao/contextGraph/pkg/idgen"
	"github.com/luhao/contextGraph/pkg/utils"
	"github.com/luhao/contextGraph/pkg/verify"
	"gorm.io/gorm"
)

type ctxKey string // 1. 定义一个未导出的自定义类型

// 2. 定义该类型的常量 (使用该类型作为 key)
const (
    UserAgentKey ctxKey = "UserAgent"
    ClientIPKey  ctxKey = "ClientIP"
)

type userRepo interface {
	StoreVerificationCode(ctx context.Context, email, code string, reqType string) error
	GetVerificationCode(ctx context.Context, email string, reqType string) (string, error)
	DeleteVerificationCodeIfExist(ctx context.Context, email string, reqType string) error
	StoreSessionData(ctx context.Context, userID string, sessionID string, data *model.SessionUserData, expiration time.Duration) error
	IsEmailRegistered(email string) (bool, error)
	CountUserSessions(ctx context.Context, userID string) (int64, error)
	DeleteSessionData(ctx context.Context, rdbKey string) error
	DeleteSessionDataByUserID(ctx context.Context, userID string) error
	DeleteVersionInfo(ctx context.Context, userID string) error
	GetUserByEmail(email string) (*model.User, error)
	CreateUser(ctx context.Context, user *model.User) error
	UpdateUserPassword(ctx context.Context, tx *gorm.DB, userID int64, newHashedPassword string) (int64, error)
	GetUserByID(userID int64) (*model.User, error)
	CheckRateLimit(ctx context.Context, key string, limit int, window time.Duration) (bool, error)
	BeginTX(ctx context.Context) *gorm.DB
}

type emailPublisher interface {
	PublishEmailTask(ctx context.Context, to, subject, htmlBody string) error
}

const codeExpiryMinutes = 1 // 验证码有效期（分钟），同时用于 Redis TTL 和邮件模板显示

type AuthService struct {
	userRepo    userRepo
	emailRepo   emailPublisher
	emailSender *emailpkg.Sender
}

func NewAuthService(userRepo userRepo, emailRepo emailPublisher, emailSender *emailpkg.Sender) *AuthService {
	return &AuthService{userRepo: userRepo, emailRepo: emailRepo, emailSender: emailSender}
}

func (s *AuthService) SendCode(ctx context.Context, email string, reqType string) error {
	if !verify.IsValidEmail(email) || !verify.HasValidDomain(ctx, email) {
		return apperr.EmailFormatError()
	}

	// 限流
	if err := s.checkLimit(ctx, "limit:send_code:"+email, 2, time.Minute); err != nil {
		return err
	}

	// 验证用户是否已注册
	isRegistered, err := s.userRepo.IsEmailRegistered(email)
	if err != nil {
		return err
	}
	if isRegistered && reqType == "register" {
		return apperr.EmailExists()
	}
	if !isRegistered && reqType == "reset_password" {
		return apperr.NotFound("email not registered")
	}

	// generate a 6-digit code
	code := idgen.RandomNumbers(6)

	// 1. 先存 Redis（必须在发邮件之前）
	err = s.userRepo.StoreVerificationCode(ctx, email, code, reqType)
	if err != nil {
		return err
	}

	// 2. 再异步发邮件
	subject := "ContextGraph 验证码"
	htmlBody := emailpkg.BuildVerificationEmailHTML(code, codeExpiryMinutes)
	err = s.emailRepo.PublishEmailTask(ctx, email, subject, htmlBody)
	if err != nil {
		// publish 失败，fallback 同步发送
		log.Printf("email publish failed, fallback to sync send: %v", err)
		if sendErr := s.emailSender.Send(email, subject, htmlBody); sendErr != nil {
			log.Printf("sync email send also failed: %v", sendErr)
			return apperr.InternalError("Failed to send verification email")
		}
	}

	return nil
}

func (s *AuthService) VerifyCode(ctx context.Context, email, code, reqType string) (string, error) {
	// 限流
	if err := s.checkLimit(ctx, "limit:verify_code:"+email, 5, time.Minute); err != nil {
		return "", err
	}
	
	storedCode , err := s.userRepo.GetVerificationCode(ctx, email, reqType)
	if err != nil {
		return "", err
	}

	if storedCode != code {
		return "", apperr.InvalidCode()
	}

	token, err := utils.GenerateVerificationToken(email)
	if err != nil {
		return "", err
	}

	if err = s.userRepo.DeleteVerificationCodeIfExist(ctx, email, reqType); err != nil {
		return "", err
	}


	return token, nil
}

func (s *AuthService) Register(ctx context.Context,user *model.User) (string, string,error) {
	if err := s.checkLimit(ctx, "limit:register:"+user.Email, 2, time.Minute); err != nil {
		return "", "", err
	}

	if isRegistered, err := s.userRepo.IsEmailRegistered(user.Email); err != nil {
		return "", "", err
	} else if isRegistered {
		return "", "", apperr.EmailExists()
	}
	
	password := user.Password
	hashedPassword, err := utils.HashPassword(password)
	if err != nil {
		return "", "", err
	}
	user.Password = hashedPassword;

	err = s.userRepo.CreateUser(ctx, user)
	if err != nil {
		return "", "", err
	}
	// 生成认证 JWT Token
	token, err := utils.GenerateAuthToken(user.ID, user.Email, user.Username, user.Plan, user.Auth_version)
	if err != nil {
		return "", "", apperr.PartialSuccess("User created, please log in again")
	}

	// 生成唯一的 session ID
	sessionID := idgen.UUID()

	session_data := model.SessionUserData{
		UserID:  user.ID,
		Email:    user.Email,
		Username: user.Username,
		Plan:     user.Plan,
		UA:       safeGetStringFromContext(ctx, UserAgentKey),
		IP:       safeGetStringFromContext(ctx, ClientIPKey),
		AuthVersion: user.Auth_version,
	}

	// 序列化为 JSON
	userID_str := strconv.FormatInt(user.ID, 10)
	err = s.userRepo.StoreSessionData(ctx, userID_str, sessionID, &session_data, 7*24*time.Hour)
	if err != nil {
		return "", "", apperr.PartialSuccess("User created, please log in again")
	}

	return token, sessionID, nil
}

func (s *AuthService) Login(ctx context.Context, email, password string) (string, string, error) {
	//检查限流
	if err := s.checkLimit(ctx, "limit:login:"+email, 5, time.Minute); err != nil {
		return "", "", err
	}

	user, err := s.userRepo.GetUserByEmail(email)
	if err != nil {
		return "", "", err
	}


	if !utils.CheckPasswordHash(password, user.Password) {
		return "", "", apperr.PasswordWrong()
	}

	userID_str := strconv.FormatInt(user.ID, 10)
	sessionCount, err := s.userRepo.CountUserSessions(ctx, userID_str)
	if err != nil {
		return "", "", err
	}
	if sessionCount >= 25 {
		return "", "", apperr.Forbidden("Maximum concurrent sessions reached, please logout from other devices first")
	}

	// 生成认证 JWT Token
	token, err := utils.GenerateAuthToken(user.ID, user.Email, user.Username, user.Plan, user.Auth_version)
	if err != nil {
		return "", "", err
	}

	// 生成唯一的 session ID
	sessionID := idgen.UUID()

	session_data := model.SessionUserData{
		UserID:  user.ID,
		Email:    user.Email,
		Username: user.Username,
		Plan:     user.Plan,
		UA:       safeGetStringFromContext(ctx, UserAgentKey),
		IP:       safeGetStringFromContext(ctx, ClientIPKey),
		AuthVersion: user.Auth_version,
	}

	// 序列化为 JSON
	err = s.userRepo.StoreSessionData(ctx, userID_str, sessionID, &session_data, 7*24*time.Hour)
	if err != nil {
		return "", "", err
	}

	return token, sessionID, nil
}

func (s *AuthService) Logout(ctx context.Context, userID int64, sessionID string) error {
	rdbKey := "user:" + strconv.FormatInt(userID, 10) + ":refresh:" + sessionID
	err := s.userRepo.DeleteSessionData(ctx, rdbKey)
	if err != nil {
		return err
	}
	return nil
}

func (s *AuthService) ResetPassword(ctx context.Context, email, newPassword string) (string, string, error) {
	if err := s.checkLimit(ctx, "limit:reset_password:"+email, 5, time.Hour); err != nil {
		return "", "", err
	}

	user, err := s.userRepo.GetUserByEmail(email)
	if err != nil {
		return "", "", err
	}

	if utils.CheckPasswordHash(newPassword, user.Password) {
		return "", "", apperr.PasswordSameAsOld()
	}

	hashedPassword, err := utils.HashPassword(newPassword)
	if err != nil {
		return "", "", err
	}

	tx := s.userRepo.BeginTX(ctx)
	if tx.Error != nil {
		return "", "", tx.Error
	}
	defer tx.Rollback()

	// 更新密码, 增加 auth_version
	NewAuthVersion, err := s.userRepo.UpdateUserPassword(ctx, tx, user.ID, hashedPassword)
	if err != nil {
		return "", "", err
	}

	// 删除版本信息
	err = s.userRepo.DeleteVersionInfo(ctx, strconv.FormatInt(user.ID, 10))
	if err != nil {
		// 删除版本信息失败
		log.Printf("Failed to delete version info for user %d: %v", user.ID, err)
		return "", "", err
	}

	// 取消已经有的所有 session
	err = s.userRepo.DeleteSessionDataByUserID(ctx, strconv.FormatInt(user.ID, 10))
	if err != nil {
		// 删除 session 失败
		log.Printf("Failed to delete session data for user %d: %v", user.ID, err)
		return "", "", err
	}

	if err = tx.Commit().Error; err != nil {
		log.Printf("Failed to commit transaction for user %d password reset: %v", user.ID, err)
		return "", "", err
	}

	// 生成认证 JWT Token
	token, err := utils.GenerateAuthToken(user.ID, user.Email, user.Username, user.Plan, NewAuthVersion)
	if err != nil {
		log.Printf("Failed to generate auth token for user %d: %v", user.ID, err)
		return "", "", apperr.PartialSuccess("password changed, please log in again")
	}

	// 生成唯一的 session ID
	sessionID := idgen.UUID()

	session_data := model.SessionUserData{
		UserID:  user.ID,
		Email:    user.Email,
		Username: user.Username,
		Plan:     user.Plan,
		UA:       safeGetStringFromContext(ctx, UserAgentKey),
		IP:       safeGetStringFromContext(ctx, ClientIPKey),
		AuthVersion: NewAuthVersion,
	}

	// redis存储新的 session 数据
	userID_str := strconv.FormatInt(user.ID, 10)
	err = s.userRepo.StoreSessionData(ctx, userID_str, sessionID, &session_data, 7*24*time.Hour)
	if err != nil {
		log.Printf("Failed to store session data for user %d: %v", user.ID, err)
		return "", "", apperr.PartialSuccess("password changed, please log in again")
	}

	return token, sessionID, nil
}

func (s *AuthService) checkLimit(ctx context.Context, key string, limit int, window time.Duration) error {
    ok, err := s.userRepo.CheckRateLimit(ctx, key, limit, window)
    if err != nil {
        return apperr.InternalError("Redis error")
    }
    if !ok {
        return apperr.FrequentRequest()
    }
    return nil
}

func safeGetStringFromContext(ctx context.Context, key ctxKey) string {
    val, ok := ctx.Value(key).(string)
    if !ok {
        return "" // 如果 context 里没有，或者类型不对，返回空串，不会 panic
    }
    return val
}