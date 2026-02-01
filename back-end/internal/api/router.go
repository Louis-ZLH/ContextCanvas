package api

import (
	"github.com/gin-gonic/gin"
	"github.com/luhao/contextGraph/internal/app"
)

func NewRouter(a *app.App) *gin.Engine {
	_ = a
	r := gin.New()

	r.Use(gin.Logger(), gin.Recovery())

	api := r.Group("/api/")

	api.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "pong"})
	})

	return r
}