package service

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/gif"
	_ "image/png"
	"io"
	"log"
	"mime/multipart"
	"path/filepath"
	"strings"

	"github.com/luhao/contextGraph/internal/model"
	apperr "github.com/luhao/contextGraph/pkg/errors"
	"github.com/minio/minio-go/v7"
	pdfcpuapi "github.com/pdfcpu/pdfcpu/pkg/api"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	maxFileSize        = 5 << 20   // 5 MB
	maxTextFileSize    = 50 << 10  // 50 KB
	maxStoragePerUser  = 200 << 20 // 200 MB
	maxPDFPages        = 3
	maxPPTSlides       = 5
	maxImageWidth      = 1568
	jpegQuality        = 80
)

// 允许的文件扩展名
var allowedExtensions = map[string]bool{
	".pdf":  true,
	".png":  true,
	".jpg":  true,
	".jpeg": true,
	".gif":  true,
	".webp": true,
	".svg":  true,
	".txt":  true,
	".md":   true,
	".docx": true,
	".xlsx": true,
	".pptx": true,
	".csv":  true,
	".json": true,
}

// 旧版 Office 格式扩展名（明确拒绝并提示用户转换）
var legacyOfficeExtensions = map[string]bool{
	".doc": true,
	".xls": true,
	".ppt": true,
}

// 允许的 MIME type 前缀
var allowedMIMEPrefixes = []string{
	"image/",
	"application/pdf",
	"text/",
	"application/vnd.openxmlformats",
	"application/json",
}

// 旧版 Office MIME types（明确拒绝）
var legacyOfficeMIMETypes = map[string]bool{
	"application/msword":             true, // .doc
	"application/vnd.ms-excel":       true, // .xls
	"application/vnd.ms-powerpoint":  true, // .ppt
}

type FileRepo interface {
	UploadToMinio(ctx context.Context, userID int64, file io.Reader, filename string, fileSize int64, contentType string) (string, error)
	CreateFileRecord(ctx context.Context, fileRecord *model.File) error
	GetFileByID(ctx context.Context, fileID int64) (*model.File, error)
	GetFileFromMinio(ctx context.Context, minioPath string) (*minio.Object, error)
	GetNodeWithCanvasUserID(ctx context.Context, nodeID string) (*model.Node, int64, error)
	UpdateNodeFileID(ctx context.Context, nodeID string, fileID *int64) error
	SetFileProcessingKeys(ctx context.Context, fileID int64) error
	PublishFileConvert(ctx context.Context, fileID int64, minioPath string, contentType string) error
	ListFilesByUser(ctx context.Context, userID int64, keyword string, page, limit int) ([]model.File, int64, error)
	DeleteFileByID(ctx context.Context, fileID int64) error
	RemoveMinioObject(ctx context.Context, minioPath string) error
	GetUserStorageUsed(ctx context.Context, userID int64) (int64, error)
}

type FileService struct {
	repo FileRepo
}

func NewFileService(repo FileRepo) *FileService {
	return &FileService{repo: repo}
}

func (s *FileService) UploadFile(ctx context.Context, userID int64, fileHeader *multipart.FileHeader) (int64, error) {
	// 1. 验证文件大小
	if fileHeader.Size > maxFileSize {
		return 0, apperr.BadRequest("File size exceeds limit (max 5MB)")
	}
	if fileHeader.Size == 0 {
		return 0, apperr.BadRequest("File cannot be empty")
	}

	// 2. 拒绝旧版 Office 格式
	ext := strings.ToLower(filepath.Ext(fileHeader.Filename))
	if legacyOfficeExtensions[ext] {
		return 0, apperr.BadRequest("Legacy Office formats (.doc/.xls/.ppt) are not supported. Please convert to .docx/.xlsx/.pptx and re-upload")
	}

	// 3. 验证文件扩展名
	if ext == "" || !allowedExtensions[ext] {
		return 0, apperr.BadRequest("Unsupported file type")
	}

	// 4. 验证 Content-Type（同时拒绝旧版 Office MIME type）
	contentType := fileHeader.Header.Get("Content-Type")
	if legacyOfficeMIMETypes[strings.ToLower(contentType)] {
		return 0, apperr.BadRequest("Legacy Office formats (.doc/.xls/.ppt) are not supported. Please convert to .docx/.xlsx/.pptx and re-upload")
	}
	if !isAllowedMIME(contentType) {
		return 0, apperr.BadRequest("Unsupported file MIME type")
	}

	// 5. 校验用户存储配额
	used, err := s.repo.GetUserStorageUsed(ctx, userID)
	if err != nil {
		return 0, apperr.Wrap(err, 500, apperr.BizUnknown, "Failed to query storage usage")
	}
	if used+fileHeader.Size > maxStoragePerUser {
		return 0, apperr.BadRequest("Insufficient storage. Free users can upload up to 200MB")
	}

	// 6. 打开文件
	file, err := fileHeader.Open()
	if err != nil {
		return 0, apperr.Wrap(err, 500, apperr.BizUnknown, "Failed to read uploaded file")
	}
	defer file.Close()

	var (
		uploadReader      io.Reader = file
		uploadSize        int64     = fileHeader.Size
		uploadContentType string    = contentType
	)

	// 7. 按类型进行额外校验和处理
	switch {
	case contentType == "application/pdf":
		// PDF：校验页数 ≤ 3
		pageCount, err := pdfcpuapi.PageCount(file, nil)
		if err != nil {
			return 0, apperr.BadRequest("Failed to parse PDF file")
		}
		if pageCount > maxPDFPages {
			return 0, apperr.BadRequest(fmt.Sprintf("PDF page count exceeds limit (max %d pages, current %d pages)", maxPDFPages, pageCount))
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return 0, apperr.Wrap(err, 500, apperr.BizUnknown, "File processing failed")
		}

	case isPPTContentType(contentType):
		// PPTX：校验页数 ≤ 5（通过 ZIP 结构计数 slide 数量）
		slideCount, err := countPPTXSlides(file, fileHeader.Size)
		if err == nil && slideCount > maxPPTSlides {
			return 0, apperr.BadRequest(fmt.Sprintf("PPT slide count exceeds limit (max %d slides, current %d slides)", maxPPTSlides, slideCount))
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return 0, apperr.Wrap(err, 500, apperr.BizUnknown, "File processing failed")
		}

	case isDocxContentType(contentType):
		// DOCX：通过 ZIP 读取 word/document.xml 估算文本量
		xmlSize, err := estimateZipEntrySize(file, fileHeader.Size, "word/document.xml")
		if err != nil {
			return 0, apperr.BadRequest("Failed to parse DOCX file")
		}
		if xmlSize > maxTextFileSize {
			return 0, apperr.BadRequest("DOCX text content too large (max 50KB)")
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return 0, apperr.Wrap(err, 500, apperr.BizUnknown, "File processing failed")
		}

	case isXlsxContentType(contentType):
		// XLSX：通过 ZIP 读取 xl/sharedStrings.xml 估算文本量
		xmlSize, err := estimateZipEntrySize(file, fileHeader.Size, "xl/sharedStrings.xml")
		if err != nil {
			return 0, apperr.BadRequest("Failed to parse XLSX file")
		}
		if xmlSize > maxTextFileSize {
			return 0, apperr.BadRequest("XLSX text content too large (max 50KB)")
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return 0, apperr.Wrap(err, 500, apperr.BizUnknown, "File processing failed")
		}

	case isTextContentType(contentType):
		// 文本类文件：校验大小 ≤ 50KB
		if fileHeader.Size > maxTextFileSize {
			return 0, apperr.BadRequest("Text file content too large (max 50KB)")
		}

	case isCompressibleImage(contentType):
		// 图片：压缩后再存 MinIO
		data, err := compressImage(file)
		if err != nil {
			return 0, err
		}
		uploadReader = bytes.NewReader(data)
		uploadSize = int64(len(data))
		uploadContentType = "image/jpeg"
	}

	// 8. 上传到 MinIO
	minioPath, err := s.repo.UploadToMinio(ctx, userID, uploadReader, fileHeader.Filename, uploadSize, uploadContentType)
	if err != nil {
		return 0, err
	}

	// 9. 保存文件记录到数据库
	fileRecord := &model.File{
		UserID:      userID,
		MinioPath:   minioPath,
		Filename:    fileHeader.Filename,
		FileSize:    uploadSize,
		ContentType: uploadContentType,
	}
	if err := s.repo.CreateFileRecord(ctx, fileRecord); err != nil {
		return 0, err
	}

	// 10. 需要异步预处理的文件类型：SET Redis keys + Publish RabbitMQ
	if needsPreprocessing(uploadContentType) {
		if err := s.repo.SetFileProcessingKeys(ctx, fileRecord.ID); err != nil {
			log.Printf("[UploadFile] SetFileProcessingKeys failed for fileID=%d: %v", fileRecord.ID, err)
		}
		if err := s.repo.PublishFileConvert(ctx, fileRecord.ID, minioPath, uploadContentType); err != nil {
			log.Printf("[UploadFile] PublishFileConvert failed for fileID=%d: %v", fileRecord.ID, err)
		}
	}

	return fileRecord.ID, nil
}

// GetFileInfo 获取文件元信息（验证所有权）
func (s *FileService) GetFileInfo(ctx context.Context, userID int64, fileID int64) (*model.File, error) {
	file, err := s.repo.GetFileByID(ctx, fileID)
	if err != nil {
		return nil, err
	}
	if file.UserID != userID {
		return nil, apperr.Forbidden("No permission to access this file")
	}
	return file, nil
}

// DownloadFile 获取文件记录和文件流（验证所有权）
func (s *FileService) DownloadFile(ctx context.Context, userID int64, fileID int64) (*model.File, io.ReadCloser, error) {
	file, err := s.repo.GetFileByID(ctx, fileID)
	if err != nil {
		return nil, nil, err
	}
	if file.UserID != userID {
		return nil, nil, apperr.Forbidden("No permission to access this file")
	}

	obj, err := s.repo.GetFileFromMinio(ctx, file.MinioPath)
	if err != nil {
		return nil, nil, err
	}

	return file, obj, nil
}

// ListFiles 获取用户文件列表
func (s *FileService) ListFiles(ctx context.Context, userID int64, keyword string, page, limit int) ([]model.File, int64, error) {
	return s.repo.ListFilesByUser(ctx, userID, keyword, page, limit)
}

// DeleteFile 删除文件（验证所有权 → 解绑节点 → 软删除 → 删除 MinIO）
func (s *FileService) DeleteFile(ctx context.Context, userID int64, fileID int64) error {
	// 1. 获取文件记录
	file, err := s.repo.GetFileByID(ctx, fileID)
	if err != nil {
		return err
	}
	// 2. 验证所有权
	if file.UserID != userID {
		return apperr.Forbidden("No permission to delete this file")
	}
	// 3. 数据库事务：解绑节点 + 软删除
	if err := s.repo.DeleteFileByID(ctx, fileID); err != nil {
		return err
	}
	// 4. 删除 MinIO 对象（失败仅记日志，不影响主流程）
	if err := s.repo.RemoveMinioObject(ctx, file.MinioPath); err != nil {
		log.Printf("[DeleteFile] RemoveMinioObject failed for fileID=%d path=%s: %v", fileID, file.MinioPath, err)
	}
	return nil
}

// BindFileToNode 将文件绑定到节点
func (s *FileService) BindFileToNode(ctx context.Context, userID int64, fileID int64, nodeID string) error {
	// 1. 验证文件存在且属于当前用户
	file, err := s.repo.GetFileByID(ctx, fileID)
	if err != nil {
		return err
	}
	if file.UserID != userID {
		return apperr.Forbidden("No permission to operate on this file")
	}

	// 2. 验证节点存在且用户拥有其所属画布
	_, canvasUserID, err := s.repo.GetNodeWithCanvasUserID(ctx, nodeID)
	if err != nil {
		return err
	}
	if canvasUserID != userID {
		return apperr.Forbidden("No permission to operate on this node")
	}

	// 3. 更新节点的 file_id
	return s.repo.UpdateNodeFileID(ctx, nodeID, &fileID)
}

// GetStorageUsage 获取用户存储用量
func (s *FileService) GetStorageUsage(ctx context.Context, userID int64) (int64, int64, error) {
	used, err := s.repo.GetUserStorageUsed(ctx, userID)
	if err != nil {
		return 0, 0, apperr.Wrap(err, 500, apperr.BizUnknown, "Failed to query storage usage")
	}
	return used, maxStoragePerUser, nil
}

// compressImage 压缩图片：最大宽度 1568px，JPEG quality=80
func compressImage(r io.Reader) ([]byte, error) {
	img, _, err := image.Decode(r)
	if err != nil {
		return nil, apperr.BadRequest("Failed to parse image file")
	}

	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	// 如果宽度超过上限，等比缩放
	if width > maxImageWidth {
		newHeight := height * maxImageWidth / width
		dst := image.NewRGBA(image.Rect(0, 0, maxImageWidth, newHeight))
		draw.CatmullRom.Scale(dst, dst.Bounds(), img, bounds, draw.Over, nil)
		img = dst
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, apperr.Wrap(err, 500, apperr.BizUnknown, "Image compression failed")
	}

	return buf.Bytes(), nil
}

// isTextContentType 判断是否为文本类 Content-Type（text/* 或 application/json）
func isTextContentType(contentType string) bool {
	ct := strings.ToLower(contentType)
	return strings.HasPrefix(ct, "text/") || ct == "application/json"
}

// isCompressibleImage 判断是否为可压缩的图片类型（排除 SVG）
func isCompressibleImage(contentType string) bool {
	ct := strings.ToLower(contentType)
	return strings.HasPrefix(ct, "image/") && ct != "image/svg+xml"
}

// isDocxContentType 判断是否为 DOCX 类型
func isDocxContentType(contentType string) bool {
	return strings.ToLower(contentType) == "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
}

// isXlsxContentType 判断是否为 XLSX 类型
func isXlsxContentType(contentType string) bool {
	return strings.ToLower(contentType) == "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
}

// estimateZipEntrySize 读取 ZIP 中指定条目的未压缩大小，用于估算文本量
func estimateZipEntrySize(r io.ReaderAt, size int64, entryPath string) (uint64, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return 0, err
	}
	for _, f := range zr.File {
		if f.Name == entryPath {
			return f.UncompressedSize64, nil
		}
	}
	return 0, nil // 条目不存在，无需拒绝
}

// isPPTContentType 判断是否为 PPTX 类型（旧版 .ppt 已在上传入口拒绝）
func isPPTContentType(contentType string) bool {
	return strings.ToLower(contentType) == "application/vnd.openxmlformats-officedocument.presentationml.presentation"
}

// countPPTXSlides 通过 ZIP 结构计算 PPTX 的 slide 数量
func countPPTXSlides(r io.ReaderAt, size int64) (int, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "ppt/slides/slide") && strings.HasSuffix(f.Name, ".xml") {
			count++
		}
	}
	return count, nil
}

// needsPreprocessing 判断文件是否需要异步预处理（PDF、DOCX、XLSX、PPTX）
func needsPreprocessing(contentType string) bool {
	ct := strings.ToLower(contentType)
	return ct == "application/pdf" ||
		isDocxContentType(ct) ||
		isXlsxContentType(ct) ||
		isPPTContentType(ct)
}

func isAllowedMIME(mime string) bool {
	mime = strings.ToLower(mime)
	for _, prefix := range allowedMIMEPrefixes {
		if strings.HasPrefix(mime, prefix) {
			return true
		}
	}
	return false
}
