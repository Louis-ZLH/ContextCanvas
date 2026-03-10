package api

import (
	"github.com/gin-gonic/gin"
	"github.com/luhao/contextGraph/internal/app"
	"github.com/luhao/contextGraph/internal/middleware"
)

func NewUserRouter(api *gin.RouterGroup, a *app.App) {
	userApi := api.Group("/user")

	userApi.Use(middleware.AuthMiddleware(a.RDB, a.DB, a.Cfg.CookieSecure))
	userApi.GET("/profile",a.H.UserHandler.GetProfile)
}