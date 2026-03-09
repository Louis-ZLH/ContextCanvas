package handler

import (
	"context"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/luhao/contextGraph/internal/dto"
	"github.com/luhao/contextGraph/internal/model"
	apperr "github.com/luhao/contextGraph/pkg/errors"
)

type FileService interface {
	UploadFile(ctx context.Context, userID int64, fileHeader *multipart.FileHeader) (int64, error)
	GetFileInfo(ctx context.Context, userID int64, fileID int64) (*model.File, error)
	DownloadFile(ctx context.Context, userID int64, fileID int64) (*model.File, io.ReadCloser, error)
	BindFileToNode(ctx context.Context, userID int64, fileID int64, nodeID string) error
	ListFiles(ctx context.Context, userID int64, keyword string, page, limit int) ([]model.File, int64, error)
	DeleteFile(ctx context.Context, userID int64, fileID int64) error
	GetStorageUsage(ctx context.Context, userID int64) (int64, int64, error)
}

type FileHandler struct {
	fileService FileService
}

func NewFileHandler(fileService FileService) *FileHandler {
	return &FileHandler{fileService: fileService}
}

func (h *FileHandler) UploadFile(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, dto.Error(apperr.BizUnauthorized, "Unauthorized"))
		return
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, dto.Error(apperr.BizInvalidParams, "Please upload a file"))
		return
	}

	fileID, err := h.fileService.UploadFile(c.Request.Context(), userID.(int64), fileHeader)
	if err != nil {
		if appErr, ok := apperr.GetAppError(err); ok {
			c.JSON(appErr.Code, dto.Error(appErr.BizCode, appErr.Message))
			return
		}
		c.JSON(http.StatusInternalServerError, dto.Error(apperr.BizUnknown, "Internal Server Error"))
		return
	}

	c.JSON(http.StatusOK, dto.Success(dto.UploadFileResponse{
		FileID: fileID,
	}))
}

// DownloadFile GET /file/:id — 返回文件二进制流，可用于 <img src> 展示或下载
func (h *FileHandler) DownloadFile(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, dto.Error(apperr.BizUnauthorized, "Unauthorized"))
		return
	}

	fileID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, dto.Error(apperr.BizInvalidParams, "Invalid file ID"))
		return
	}

	fileMeta, reader, err := h.fileService.DownloadFile(c.Request.Context(), userID.(int64), fileID)
	if err != nil {
		if appErr, ok := apperr.GetAppError(err); ok {
			c.JSON(appErr.Code, dto.Error(appErr.BizCode, appErr.Message))
			return
		}
		c.JSON(http.StatusInternalServerError, dto.Error(apperr.BizUnknown, "Internal Server Error"))
		return
	}
	defer reader.Close()

	// 如果前端传 ?download=true，则设置 Content-Disposition 为 attachment 触发下载
	// 否则设置为 inline，让浏览器直接展示（如图片）
	disposition := "inline"
	if c.Query("download") == "true" {
		disposition = "attachment"
	}

	c.Header("Content-Disposition", fmt.Sprintf(`%s; filename="%s"`, disposition, fileMeta.Filename))
	c.Header("Content-Type", fileMeta.ContentType)
	c.Header("Content-Length", strconv.FormatInt(fileMeta.FileSize, 10))
	c.Header("Cache-Control", "private, max-age=86400")

	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, reader); err != nil {
		// 响应头已发送，无法返回 JSON 错误；记录日志即可
		log.Printf("[DownloadFile] io.Copy failed for fileID=%d: %v", fileID, err)
	}
}

// GetFileInfo GET /file/:id/info — 返回文件元信息 JSON
func (h *FileHandler) GetFileInfo(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, dto.Error(apperr.BizUnauthorized, "Unauthorized"))
		return
	}

	fileID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, dto.Error(apperr.BizInvalidParams, "Invalid file ID"))
		return
	}

	fileMeta, err := h.fileService.GetFileInfo(c.Request.Context(), userID.(int64), fileID)
	if err != nil {
		if appErr, ok := apperr.GetAppError(err); ok {
			c.JSON(appErr.Code, dto.Error(appErr.BizCode, appErr.Message))
			return
		}
		c.JSON(http.StatusInternalServerError, dto.Error(apperr.BizUnknown, "Internal Server Error"))
		return
	}

	c.JSON(http.StatusOK, dto.Success(dto.FileInfoResponse{
		FileID:      fileMeta.ID,
		Filename:    fileMeta.Filename,
		FileSize:    fileMeta.FileSize,
		ContentType: fileMeta.ContentType,
	}))
}

// ListFiles GET /api/file/list?page=1&limit=20&keyword=xxx
func (h *FileHandler) ListFiles(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, dto.Error(apperr.BizUnauthorized, "Unauthorized"))
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	keyword := c.Query("keyword")

	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}

	files, total, err := h.fileService.ListFiles(c.Request.Context(), userID.(int64), keyword, page, limit)
	if err != nil {
		if appErr, ok := apperr.GetAppError(err); ok {
			c.JSON(appErr.Code, dto.Error(appErr.BizCode, appErr.Message))
			return
		}
		c.JSON(http.StatusInternalServerError, dto.Error(apperr.BizUnknown, "Internal Server Error"))
		return
	}

	items := make([]dto.FileListItem, len(files))
	for i, f := range files {
		items[i] = dto.FileListItem{
			FileID:      f.ID,
			Filename:    f.Filename,
			FileSize:    f.FileSize,
			ContentType: f.ContentType,
			CreatedAt:   f.CreatedAt.Format(time.RFC3339),
		}
	}

	c.JSON(http.StatusOK, dto.Success(dto.FileListResponse{
		Files: items,
		Total: total,
		Page:  page,
		Limit: limit,
	}))
}

// DeleteFile DELETE /api/file/:id
func (h *FileHandler) DeleteFile(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, dto.Error(apperr.BizUnauthorized, "Unauthorized"))
		return
	}

	fileID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, dto.Error(apperr.BizInvalidParams, "Invalid file ID"))
		return
	}

	err = h.fileService.DeleteFile(c.Request.Context(), userID.(int64), fileID)
	if err != nil {
		if appErr, ok := apperr.GetAppError(err); ok {
			c.JSON(appErr.Code, dto.Error(appErr.BizCode, appErr.Message))
			return
		}
		c.JSON(http.StatusInternalServerError, dto.Error(apperr.BizUnknown, "Internal Server Error"))
		return
	}

	c.JSON(http.StatusOK, dto.SuccessMsg("File deleted"))
}

// BindFileToNode POST /file/bind-node — 将文件绑定到节点
func (h *FileHandler) BindFileToNode(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, dto.Error(apperr.BizUnauthorized, "Unauthorized"))
		return
	}

	var req dto.BindFileToNodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.Error(apperr.BizInvalidParams, "Invalid parameters"))
		return
	}

	err := h.fileService.BindFileToNode(c.Request.Context(), userID.(int64), req.FileID, req.NodeID)
	if err != nil {
		if appErr, ok := apperr.GetAppError(err); ok {
			c.JSON(appErr.Code, dto.Error(appErr.BizCode, appErr.Message))
			return
		}
		c.JSON(http.StatusInternalServerError, dto.Error(apperr.BizUnknown, "Internal Server Error"))
		return
	}

	c.JSON(http.StatusOK, dto.SuccessMsg("bind successfully"))
}

// GetStorageUsage GET /api/file/storage — 返回用户存储用量
func (h *FileHandler) GetStorageUsage(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, dto.Error(apperr.BizUnauthorized, "Unauthorized"))
		return
	}

	used, limit, err := h.fileService.GetStorageUsage(c.Request.Context(), userID.(int64))
	if err != nil {
		if appErr, ok := apperr.GetAppError(err); ok {
			c.JSON(appErr.Code, dto.Error(appErr.BizCode, appErr.Message))
			return
		}
		c.JSON(http.StatusInternalServerError, dto.Error(apperr.BizUnknown, "Internal Server Error"))
		return
	}

	c.JSON(http.StatusOK, dto.Success(dto.StorageUsageResponse{
		Used:  used,
		Limit: limit,
	}))
}
