package api

import (
	"github.com/gin-gonic/gin"
	"github.com/luhao/contextGraph/internal/app"
	"github.com/luhao/contextGraph/internal/middleware"
)

func NewChatRouter(api *gin.RouterGroup, a *app.App) {
	chatApi := api.Group("/chat")

	chatApi.Use(middleware.AuthMiddleware(a.RDB, a.DB, a.Cfg.CookieSecure))
	chatApi.POST("/create", a.H.ConversationHandler.CreateConversation)
	chatApi.GET("/history/:conversationId", a.H.ConversationHandler.GetConversationHistory)
	chatApi.PUT("/conversations/:conversationId/leaf", a.H.ConversationHandler.UpdateCurrentLeaf)

	streamLimited := chatApi.Group("", middleware.StreamLimitMiddleware(a.RDB))
	streamLimited.POST("/messages", a.H.ConversationHandler.SendMessage)
	streamLimited.POST("/retry/message", a.H.ConversationHandler.RetryMessage)
}