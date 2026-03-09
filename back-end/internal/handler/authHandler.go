package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/luhao/contextGraph/internal/dto"
	"github.com/luhao/contextGraph/internal/model"
	"github.com/luhao/contextGraph/internal/service"
	apperr "github.com/luhao/contextGraph/pkg/errors"
	"github.com/luhao/contextGraph/pkg/verify"
)

type AuthService interface {
	SendCode(ctx context.Context, email string, reqType string) error
	VerifyCode(ctx context.Context, email, code, reqType string) (string, error)
	Register(ctx context.Context, user *model.User) (string, string, error)
	Login(ctx context.Context, email, password string) (string, string, error)
	Logout(ctx context.Context, userID int64, sessionID string) error
	ResetPassword(ctx context.Context, email, newPassword string) (string, string, error)
}

type AuthHandler struct {
	authService AuthService
}

func NewAuthHandler(authService AuthService) *AuthHandler {
	return &AuthHandler{authService: authService}
}

func (h *AuthHandler) SendCode(c *gin.Context) {
	var req dto.SendCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.Error(apperr.BizInvalidParams, "invalid parameters"))
		return
	}

	err := h.authService.SendCode(c.Request.Context(), req.Email, req.Type)
	if err != nil {
		// 区分错误类型
		if appErr, ok := apperr.GetAppError(err); ok {
			c.JSON(appErr.Code, dto.Error(appErr.BizCode, appErr.Message))
			return
		}
		c.JSON(500, dto.Error(apperr.BizUnknown, "Internal Server Error"))
		return
	}

	// 生产环境不应返回验证码，仅返回成功消息
	c.JSON(http.StatusOK, dto.SuccessMsg(
		"Verification code sent successfully",
	))
}

func (h *AuthHandler) VerifyCode(c *gin.Context) {
	var req dto.VerifyCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.Error(apperr.BizInvalidParams, "invalid parameters"))
		return
	}

	token, err := h.authService.VerifyCode(c.Request.Context(), req.Email, req.Code, req.Type)
	if err != nil {
		// 区分错误类型
		if appErr, ok := apperr.GetAppError(err); ok {
			c.JSON(appErr.Code, dto.Error(appErr.BizCode, appErr.Message))
			return
		}
		c.JSON(500, dto.Error(apperr.BizUnknown, "Internal Server Error"))
		return
	}

	// 设置 HttpOnly 和 Secure Cookie
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("verification_token_"+req.Type, token, 60 * 5, "/", "", true, true)

	c.JSON(http.StatusOK, dto.SuccessMsg("Code verified successfully"))
}

func (h *AuthHandler) Register(c *gin.Context) {
	var req dto.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.Error(apperr.BizInvalidParams, "invalid parameters"))
		return
	}
	email, ok := c.Get("email")
	if !ok {
		c.JSON(http.StatusUnauthorized, dto.Error(apperr.BizUnauthorized, "Unauthorized"))
		return
	}

	// 大小写、数字、特殊字符的组合
	_, err := verify.IsValidPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusBadRequest, dto.Error(apperr.BizInvalidParams, err.Error()))
		return
	}

	user := &model.User{
		Email:    email.(string),
		Password: req.Password,
		Username: req.Username,
	}

	ctx := c.Request.Context()
	ctx = context.WithValue(ctx, service.UserAgentKey, c.Request.UserAgent())
	ctx = context.WithValue(ctx, service.ClientIPKey, c.ClientIP())
	token, sessionID, err := h.authService.Register(ctx, user)

	if err != nil {
		// 区分错误类型
		if appErr, ok := apperr.GetAppError(err); ok {
			if appErr.BizCode == apperr.BizPartialSuccess {
				c.JSON(appErr.Code, dto.SuccessMsg(appErr.Message))
				return
			}
			c.JSON(appErr.Code, dto.Error(appErr.BizCode, appErr.Message))
			return
		}
		c.JSON(500, dto.Error(apperr.BizUnknown, "internal server error"))
		return
	}

	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("auth_token", token, 60 * 60 * 24 * 7, "/", "", true, true)
	c.SetCookie("session_id", sessionID, 60 * 60 * 24 * 7, "/", "", true, true)
	c.SetCookie("verification_token_register", "", -1, "/", "", true, true)

	c.JSON(http.StatusOK, dto.Success(dto.RegisterResponse{
		UserID:   user.ID,
		Username: user.Username,
	}))
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req dto.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.Error(apperr.BizInvalidParams, "invalid parameters"))
		return
	}

	ctx := c.Request.Context()
	ctx = context.WithValue(ctx, service.UserAgentKey, c.Request.UserAgent())
	ctx = context.WithValue(ctx, service.ClientIPKey, c.ClientIP())
	token, sessionID, err := h.authService.Login(ctx, req.Email, req.Password)
	if err != nil {
		// 区分错误类型
		if appErr, ok := apperr.GetAppError(err); ok {
			c.JSON(appErr.Code, dto.Error(appErr.BizCode, appErr.Message))
			return
		}
		c.JSON(http.StatusInternalServerError, dto.Error(apperr.BizUnknown, "Internal server error"))
		return
	}

	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("auth_token", token, 60 * 60 * 24 * 7, "/", "", true, true)
	c.SetCookie("session_id", sessionID, 60 * 60 * 24 * 7, "/", "", true, true)

	c.JSON(http.StatusOK, dto.SuccessMsg("Login successfully"))
}

func (h *AuthHandler) Logout(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, dto.Error(apperr.BizUnauthorized, "Unauthorized"))
		return
	}
	sessionID, exists := c.Get("session_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, dto.Error(apperr.BizUnauthorized, "Unauthorized"))
		return
	}

	err := h.authService.Logout(c.Request.Context(), userID.(int64), sessionID.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.Error(apperr.BizUnknown, "Internal server error"))
		return
	}

	// 清除 Cookie
	c.SetCookie("auth_token", "", -1, "/", "", true, true)
	c.SetCookie("session_id", "", -1, "/", "", true, true)

	c.JSON(http.StatusOK, dto.SuccessMsg("Logout successful"))
}

func (h *AuthHandler) ResetPassword(c *gin.Context) {
	var req dto.ResetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.Error(apperr.BizInvalidParams, "invalid parameters"))
		return
	}
	email, ok := c.Get("email")
	if !ok {
		c.JSON(http.StatusUnauthorized, dto.Error(apperr.BizUnauthorized, "Unauthorized"))
		return
	}

	// 大小写、数字、特殊字符的组合
	_, err := verify.IsValidPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusBadRequest, dto.Error(apperr.BizInvalidParams, err.Error()))
		return
	}

	ctx := c.Request.Context()
	ctx = context.WithValue(ctx, service.UserAgentKey, c.Request.UserAgent())
	ctx = context.WithValue(ctx, service.ClientIPKey, c.ClientIP())
	token, session_id, err := h.authService.ResetPassword(ctx, email.(string), req.Password)
	if err != nil {
		// 区分错误类型
		if appErr, ok := apperr.GetAppError(err); ok {
			if appErr.BizCode == apperr.BizPartialSuccess {
				c.JSON(appErr.Code, dto.SuccessMsg(appErr.Message))
				return
			}
			c.JSON(appErr.Code, dto.Error(appErr.BizCode, appErr.Message))
			return
		}
		c.JSON(500, dto.Error(apperr.BizUnknown, "internal server error"))
		return
	}

	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("auth_token", token, 60 * 60 * 24 * 7, "/", "", true, true)
	c.SetCookie("session_id", session_id, 60 * 60 * 24 * 7, "/", "", true, true)
	c.SetCookie("verification_token_reset_password", "", -1, "/", "", true, true)


	c.JSON(http.StatusOK, dto.SuccessMsg("Password reset successfully"))
}