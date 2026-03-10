package api

import (
	"github.com/gin-gonic/gin"
	"github.com/luhao/contextGraph/internal/app"
	"github.com/luhao/contextGraph/internal/middleware"
)

func NewCanvasRouter(api *gin.RouterGroup, a *app.App) {
	canvasApi := api.Group("/canvas")

	canvasApi.Use(middleware.AuthMiddleware(a.RDB, a.DB, a.Cfg.CookieSecure))
	// Add canvas-related routes here
	canvasApi.POST("/create", a.H.CanvasHandler.CreateCanvas)
	canvasApi.GET("/list", a.H.CanvasHandler.ListCanvas)
	canvasApi.DELETE("/:id", a.H.CanvasHandler.DeleteCanvas)
	canvasApi.PATCH("/rename/:id", a.H.CanvasHandler.RenameCanvas)

	canvasApi.GET("/:id", a.H.CanvasHandler.GetCanvasDetail)
	canvasApi.POST("/:id/sync", a.H.CanvasHandler.SyncCanvas)
	canvasApi.POST("/:id/full-sync", a.H.CanvasHandler.FullSyncCanvas) //Sync失败三次后，前端会调用全量同步接口保底
	canvasApi.GET("/:id/version", a.H.CanvasHandler.GetCanvasVersion) // 获取画布版本号接口，供前端重新聚焦使用

	canvasApi.GET("/:id/conversation/list", a.H.CanvasHandler.ListCanvasConversations)

	// 独立路由前缀，不在 canvas group 下，避免与 /:id 路由冲突
	canvasSearchApi := api.Group("")
	canvasSearchApi.Use(middleware.AuthMiddleware(a.RDB, a.DB, a.Cfg.CookieSecure))
	canvasSearchApi.GET("/canvas-search", a.H.CanvasHandler.SearchCanvases)
}