package api

import (
	"github.com/gin-gonic/gin"
	"github.com/luhao/contextGraph/internal/app"
	"github.com/luhao/contextGraph/internal/middleware"
)

func NewAuthRouter(api *gin.RouterGroup, a *app.App) {
	authApi := api.Group("/auth")

	authApi.POST("/code", a.H.AuthHandler.SendCode)
	authApi.POST("/verify", a.H.AuthHandler.VerifyCode)
	authApi.POST("/register", middleware.RegisterMiddleware(), a.H.AuthHandler.Register)
	authApi.POST("/login", a.H.AuthHandler.Login)

	authApi.POST("/logout", middleware.AuthMiddleware(a.RDB, a.DB, a.Cfg.CookieSecure), a.H.AuthHandler.Logout)
	authApi.POST("/reset-password", middleware.ResetPasswordMiddleware(), a.H.AuthHandler.ResetPassword)
}