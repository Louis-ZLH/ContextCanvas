package middleware

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/luhao/contextGraph/internal/dto"
	"github.com/luhao/contextGraph/internal/model"
	apperr "github.com/luhao/contextGraph/pkg/errors"
	"github.com/luhao/contextGraph/pkg/utils"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

func RegisterMiddleware() gin.HandlerFunc{
	return func(c *gin.Context) {
		token, err := c.Cookie("verification_token_register")
		
		if err != nil || token == "" {
			c.JSON(http.StatusUnauthorized, dto.Error(apperr.BizUnauthorized, "Unauthorized"))
			c.Abort()
			return
		}

		payload, err := utils.ParseVerificationToken(token)
		if err != nil {
            c.JSON(http.StatusUnauthorized, dto.Error(apperr.BizUnauthorized, "Invalid token"))
            c.Abort()
            return
        }
		c.Set("email", payload.Email)

		c.Next()
	}
}


func ResetPasswordMiddleware() gin.HandlerFunc{
	return func(c *gin.Context) {
		token, err := c.Cookie("verification_token_reset_password")
		
		if err != nil || token == "" {
			c.JSON(http.StatusUnauthorized, dto.Error(apperr.BizUnauthorized, "Unauthorized"))
			c.Abort()
			return
		}

		payload, err := utils.ParseVerificationToken(token)
		if err != nil {
            c.JSON(http.StatusUnauthorized, dto.Error(apperr.BizUnauthorized, "Invalid token"))
            c.Abort()
            return
        }
		c.Set("email", payload.Email)

		c.Next()
	}
}

func AuthMiddleware(rdb *redis.Client, db *gorm.DB, cookieSecure bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err_token := c.Cookie("auth_token")
		sessionID, err_sessionID := c.Cookie("session_id")

		// ===== 判断 token 和 sessionID 是否存在， 或者伪造=====
		if err_token != nil || token == "" || err_sessionID != nil || sessionID == "" {
			c.JSON(http.StatusUnauthorized, dto.Error(apperr.BizUnauthorized, "Unauthorized"))
			c.Abort()
			return
		}

		payload, JWTerr := utils.ParseAuthToken(token)
		if JWTerr == utils.ErrTokenInvalid{
			c.JSON(http.StatusUnauthorized, dto.Error(apperr.BizUnauthorized, "Invalid token"))
			c.Abort()
			return
		}

		// --------- 检查 auth_version 是否匹配 ---------
		authVersion := payload.AuthVersion
		userVersionKey := "user:" + strconv.FormatInt(payload.UserID, 10) + ":version"
		storedVersionStr, RDBerr := rdb.Get(c.Request.Context(), userVersionKey).Result()
		if RDBerr != nil && RDBerr != redis.Nil {
			c.JSON(http.StatusInternalServerError, dto.Error(apperr.BizUnknown, "Internal server error"))
			c.Abort()
			return
		}
		if RDBerr == redis.Nil {
			// 从数据库加载
			var user model.User
			result := db.First(&user, payload.UserID)
			if result.Error != nil {
				c.JSON(http.StatusUnauthorized, dto.Error(apperr.BizUnauthorized, "User not found"))
				c.Abort()
				return
			}
			storedVersionStr = strconv.FormatInt(user.Auth_version, 10)
			err := rdb.Set(c.Request.Context(), userVersionKey, storedVersionStr, 24 * time.Hour).Err()
			if err != nil {
				log.Printf("Could not set auth_version in redis: %v", err)
			}
		}

		storedVersion, convErr := strconv.ParseInt(storedVersionStr, 10, 64)
		if convErr != nil {
			c.JSON(http.StatusInternalServerError, dto.Error(apperr.BizUnknown, "Internal server error"))
			c.Abort()
			return
		}
		// 比较版本号，用<而不是 !=， 可能存在删除版本号失败情况。
		if authVersion < storedVersion {
			c.JSON(http.StatusUnauthorized, dto.Error(apperr.BizUnauthorized, "Token has been revoked, please login again"))
			c.Abort()
			return
		}


		// --------- 处理 token 过期逻辑，refresh token和session ---------
		if JWTerr == utils.ErrTokenExpired{
			if payload.UserID == 0 {
				c.JSON(http.StatusUnauthorized, dto.Error(apperr.BizUnauthorized, "Invalid token payload"))
				c.Abort()
				return
			}
			rdbKey := "user:" + strconv.FormatInt(payload.UserID, 10) + ":refresh:" + sessionID;
			var sessionData model.SessionUserData
			err := rdb.Get(c.Request.Context(), rdbKey).Scan(&sessionData)
			if err == redis.Nil {
				c.JSON(http.StatusUnauthorized, dto.Error(apperr.BizUnauthorized, "Session expired, please login again"))
				c.Abort()
				return
			} else if err != nil {
				c.JSON(http.StatusInternalServerError, dto.Error(apperr.BizUnknown, "Internal server error"))
				c.Abort()
				return
			}
			currentUA := c.Request.UserAgent()
			if sessionData.UA != currentUA {
				c.JSON(http.StatusUnauthorized, dto.Error(apperr.BizUnauthorized, "Unauthorized device"))
				c.Abort()
				return
			}
			currentIP := c.ClientIP()
			sessionData.IP = currentIP // 更新为当前 IP 地址
			// Token expired but session is valid, issue a new token
			newToken, err := utils.GenerateAuthToken(sessionData.UserID, sessionData.Email, sessionData.Username, sessionData.Plan, sessionData.AuthVersion)
			if err != nil {
				c.JSON(http.StatusInternalServerError, dto.Error(apperr.BizUnknown, "Failed to generate token"))
				c.Abort()
				return
			}

			c.SetCookie("auth_token", newToken, 3600*24*7,"/","",cookieSecure,true)
			c.SetCookie("session_id", sessionID, 3600*24*7,"/","",cookieSecure,true)
			go func ()  {
				rdb.Set(context.Background(), rdbKey, &sessionData, 7*24*time.Hour)
				log.Println("Session data updated in Redis for user:", sessionData.UserID)
			}()
			payload.UserID = sessionData.UserID
			payload.Email = sessionData.Email
			payload.Username = sessionData.Username
			payload.Plan = sessionData.Plan
		}
		c.Set("user_id", payload.UserID)
		c.Set("email", payload.Email)
		c.Set("username", payload.Username)
		c.Set("plan", payload.Plan)
		c.Set("session_id", sessionID)

		c.Next()
	}
}