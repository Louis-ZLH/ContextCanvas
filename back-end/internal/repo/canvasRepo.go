package repo

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/luhao/contextGraph/internal/model"
	apperr "github.com/luhao/contextGraph/pkg/errors"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type canvasRepo struct{
	db *gorm.DB
	rdb *redis.Client
}

func NewCanvasRepo(db *gorm.DB, rdb *redis.Client) *canvasRepo {
	return &canvasRepo{db: db, rdb: rdb}
}

func (r *canvasRepo) CreateCanvas(ctx context.Context, userID int64) (model.Canvas, error) {
	// 查询该用户所有以 "Untitled Canvas" 开头的标题
	var titles []string
	r.db.WithContext(ctx).Model(&model.Canvas{}).
		Where("user_id = ? AND title LIKE ?", userID, "Untitled Canvas%").
		Pluck("title", &titles)

	// 生成唯一标题
	title := r.generateUniqueCanvasTitle(titles)

	canvas := model.Canvas{
		UserID: userID,
		Title:  title,
	}

	err := r.db.Create(&canvas).Error
	if err != nil {
		return model.Canvas{}, apperr.InternalError("Internal error while creating canvas")
	}
	return canvas, nil
}

func (r *canvasRepo) ListCanvas(ctx context.Context, userID int64) ([]model.Canvas, error){
	var canvasList []model.Canvas
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("updated_at DESC").Find(&canvasList).Error
	if err != nil {
		return nil, apperr.InternalError("Internal error while listing canvas")
	}
	return canvasList, nil
}

func (r *canvasRepo) DeleteCanvas(ctx context.Context, canvasID int64, userID int64) error {
	result := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", canvasID, userID).Delete(&model.Canvas{})
	if result.Error != nil {
		return apperr.InternalError("Internal error while deleting canvas")
	}
	if result.RowsAffected == 0 {
		return apperr.NotFound("Canvas not found or not owned by user")
	}
	return nil
}

func (r *canvasRepo) RenameCanvas(ctx context.Context, canvasID int64, userID int64, title string) error {
	result := r.db.WithContext(ctx).Model(&model.Canvas{}).
		Where("id = ? AND user_id = ?", canvasID, userID).
		Update("title", title)
	if result.Error != nil {
		return apperr.InternalError("Internal error while renaming canvas")
	}
	if result.RowsAffected == 0 {
		return apperr.NotFound("Canvas not found or not owned by user")
	}
	return nil
}

func (r *canvasRepo) GetCanvasTitleAndVersionByCanvasID(ctx context.Context, canvasID int64, userID int64) (string, int64, error) {
	var canvas model.Canvas
	result := r.db.WithContext(ctx).Select("title, version").Where("id = ? AND user_id = ?", canvasID, userID).First(&canvas)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return "", 0, apperr.NotFound("Canvas not found or not owned by user")
		}
		return "", 0, apperr.InternalError("Internal error while getting canvas title and version")
	}
	return canvas.Title, canvas.Version, nil
}

func (r *canvasRepo) GetCanvasNodesByCanvasID(ctx context.Context, canvasID int64) ([]model.Node, error) {
	var nodes []model.Node
	err := r.db.WithContext(ctx).Where("canvas_id = ?", canvasID).Find(&nodes).Error
	if err != nil {
		return nil, apperr.InternalError("Internal error while getting canvas nodes")
	}
	return nodes, nil
}

func (r *canvasRepo) GetCanvasEdgesByCanvasID(ctx context.Context, canvasID int64) ([]model.NodeEdge, error) {
	var edges []model.NodeEdge
	err := r.db.WithContext(ctx).Where("canvas_id = ?", canvasID).Find(&edges).Error
	if err != nil {
		return nil, apperr.InternalError("Internal error while getting canvas edges")
	}
	return edges, nil
}

func (r *canvasRepo) CheckCanvasOwnership(ctx context.Context, canvasID int64, userID int64) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.Canvas{}).Where("id = ? AND user_id = ?", canvasID, userID).Count(&count).Error
	if err != nil {
		return false, apperr.InternalError("Internal error while checking canvas ownership")
	}
	return count > 0, nil
}

// SyncCanvasInTransaction 在事务中同步画布的所有变更
func (r *canvasRepo) SyncCanvasInTransaction(
	ctx context.Context,
	canvasID int64,
	clientVersion int64,
	updatedNodes, createdNodes []model.Node,
	deletedNodeIDs []string,
	createdEdges []model.NodeEdge,
	deletedEdgeIDs []string,
) (time.Time, int64, error) {
	var updatedAt time.Time
	var newVersion int64

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 0. 乐观锁检查：比对版本号
		var canvas model.Canvas
		if err := tx.Select("version").Where("id = ?", canvasID).First(&canvas).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return apperr.NotFound("Canvas not found")
			}
			return apperr.InternalError("Failed to check canvas version")
		}

		// 版本号不匹配，说明有冲突
		if canvas.Version != clientVersion {
			return apperr.Conflict("Canvas has been modified elsewhere")
		}

		// 1. 更新节点（使用 map 避免 GORM 跳过零值字段）
		if len(updatedNodes) > 0 {
			for _, node := range updatedNodes {
				updates := map[string]interface{}{
					"node_type": node.NodeType,
					"pos_x":     node.PosX,
					"pos_y":     node.PosY,
					"file_id":   node.FileID,
				}
				if err := tx.Model(&model.Node{}).Where("id = ?", node.ID).Updates(updates).Error; err != nil {
					return apperr.InternalError("Failed to update canvas nodes")
				}
			}
		}

		// 2. 创建节点
		if len(createdNodes) > 0 {
		result := tx.Create(&createdNodes)
		if result.Error != nil {
			if errors.Is(result.Error, gorm.ErrDuplicatedKey) {
				return apperr.BadRequest("Node ID already exists, please retry")
			}
			return apperr.InternalError("Failed to create canvas nodes")
		}
		}

		// 3. 删除节点（硬删除，因为 undo/redo 由前端管理）
		if len(deletedNodeIDs) > 0 {
			if err := tx.Unscoped().Where("id IN ?", deletedNodeIDs).Delete(&model.Node{}).Error; err != nil {
				return apperr.InternalError("Failed to delete canvas nodes")
			}
		}

		// 4. 删除边（先删除再创建，避免唯一索引冲突）
		if len(deletedEdgeIDs) > 0 {
			if err := tx.Where("id IN ?", deletedEdgeIDs).Delete(&model.NodeEdge{}).Error; err != nil {
				return apperr.InternalError("Failed to delete canvas edges")
			}
		}

		// 5. 创建边
		if len(createdEdges) > 0 {
			if err := tx.Create(&createdEdges).Error; err != nil {
				if errors.Is(err, gorm.ErrDuplicatedKey) {
					return apperr.BadRequest("Edge ID already exists, please retry")
				}
				return apperr.InternalError("Failed to create canvas edges")
			}
		}

		// 6. 更新画布的 updated_at 和 version，并获取更新后的值
		if err := tx.Model(&model.Canvas{}).Where("id = ?", canvasID).Updates(map[string]interface{}{
			"updated_at": time.Now().UTC(),
			"version":    gorm.Expr("version + 1"),
		}).Error; err != nil {
			return apperr.InternalError("Failed to update canvas timestamp and version")
		}

		// 获取更新后的时间戳和版本号
		var updatedCanvas model.Canvas
		if err := tx.Select("updated_at, version").Where("id = ?", canvasID).First(&updatedCanvas).Error; err != nil {
			return apperr.InternalError("Failed to fetch updated timestamp and version")
		}
		updatedAt = updatedCanvas.UpdatedAt
		newVersion = updatedCanvas.Version

		return nil
	})

	if err != nil {
		return time.Time{}, 0, err
	}

	return updatedAt, newVersion, nil
}


// FullSyncCanvasInTransaction 全量同步画布：删除所有旧节点和边，插入新数据
func (r *canvasRepo) FullSyncCanvasInTransaction(
	ctx context.Context,
	canvasID int64,
	clientVersion int64,
	nodes []model.Node,
	edges []model.NodeEdge,
) (time.Time, int64, error) {
	var updatedAt time.Time
	var newVersion int64

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. 乐观锁检查：比对版本号
		var canvas model.Canvas
		if err := tx.Select("version").Where("id = ?", canvasID).First(&canvas).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return apperr.NotFound("Canvas not found")
			}
			return apperr.InternalError("Failed to check canvas version")
		}

		if canvas.Version != clientVersion {
			return apperr.Conflict("Canvas has been modified elsewhere")
		}

		// 2. 删除该 canvas 下的所有边
		if err := tx.Where("canvas_id = ?", canvasID).Delete(&model.NodeEdge{}).Error; err != nil {
			return apperr.InternalError("Failed to delete canvas edges")
		}

		// 3. 删除该 canvas 下的所有节点
		if err := tx.Unscoped().Where("canvas_id = ?", canvasID).Delete(&model.Node{}).Error; err != nil {
			return apperr.InternalError("Failed to delete canvas nodes")
		}

		// 4. 插入新节点
		if len(nodes) > 0 {
			if err := tx.Create(&nodes).Error; err != nil {
				return apperr.InternalError("Failed to create canvas nodes")
			}
		}

		// 5. 插入新边
		if len(edges) > 0 {
			if err := tx.Create(&edges).Error; err != nil {
				return apperr.InternalError("Failed to create canvas edges")
			}
		}

		// 6. 更新画布的 updated_at 和 version
		if err := tx.Model(&model.Canvas{}).Where("id = ?", canvasID).Updates(map[string]interface{}{
			"updated_at": time.Now().UTC(),
			"version":    gorm.Expr("version + 1"),
		}).Error; err != nil {
			return apperr.InternalError("Failed to update canvas timestamp and version")
		}

		// 获取更新后的时间戳和版本号
		var updatedCanvas model.Canvas
		if err := tx.Select("updated_at, version").Where("id = ?", canvasID).First(&updatedCanvas).Error; err != nil {
			return apperr.InternalError("Failed to fetch updated timestamp and version")
		}
		updatedAt = updatedCanvas.UpdatedAt
		newVersion = updatedCanvas.Version

		return nil
	})

	if err != nil {
		return time.Time{}, 0, err
	}

	return updatedAt, newVersion, nil
}

func (r *canvasRepo) GetCanvasVersion(ctx context.Context, canvasID int64, userID int64) (int64, error) {
	var canvas model.Canvas
	result := r.db.WithContext(ctx).Select("version").Where("id = ? AND user_id = ?", canvasID, userID).First(&canvas)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return 0, apperr.NotFound("Canvas not found or not owned by user")
		}
		return 0, apperr.InternalError("Internal error while getting canvas version")
	}
	return canvas.Version, nil
}

func (r *canvasRepo) ListCanvasConversations(ctx context.Context, canvasID int64) ([]model.Conversation, error) {
    var conversations []model.Conversation
    err := r.db.WithContext(ctx).
        Model(&model.Conversation{}).
        Joins("INNER JOIN nodes n ON n.id = conversations.id AND n.deleted_at IS NULL").
        Where("conversations.canvas_id = ?", canvasID).
        Find(&conversations).Error
    if err != nil {
        return nil, err
    }
	// 没找到返回空数组
    return conversations, nil
}

// GetParentNodesByTargetID 通过 node_edges 查询指定节点的所有父节点
func (r *canvasRepo) GetParentNodesByTargetID(ctx context.Context, targetNodeID string) ([]model.Node, error) {
	var nodes []model.Node
	err := r.db.WithContext(ctx).
		Joins("INNER JOIN node_edges ON node_edges.source_node_id = nodes.id").
		Where("node_edges.target_node_id = ?", targetNodeID).
		Find(&nodes).Error
	if err != nil {
		return nil, apperr.InternalError("Internal error while getting parent nodes")
	}
	return nodes, nil
}

// CanvasSearchResult 搜索结果
type CanvasSearchResult struct {
	CanvasID  int64
	Title     string
	UpdatedAt time.Time
	MatchType string // "title" | "conversation" | "content"
	MatchText string // 匹配到的文本
}

// SearchCanvases 全文搜索，返回合并后的结果
// 搜索优先级：canvas title > conversation title > message content
func (r *canvasRepo) SearchCanvases(ctx context.Context, userID int64, keyword string, page, limit int) ([]CanvasSearchResult, int64, error) {
	// 转义 LIKE 通配符
	escaped := strings.ReplaceAll(keyword, "%", "\\%")
	escaped = strings.ReplaceAll(escaped, "_", "\\_")
	likePattern := "%" + escaped + "%"

	var results []CanvasSearchResult
	seen := make(map[int64]bool) // 按 canvas_id 去重

	// ── Step 1: Canvas title 匹配 ──
	var titleMatches []struct {
		ID        int64
		Title     string
		UpdatedAt time.Time
	}
	if err := r.db.WithContext(ctx).Model(&model.Canvas{}).
		Select("id, title, updated_at").
		Where("user_id = ? AND title LIKE ?", userID, likePattern).
		Order("updated_at DESC").
		Limit(1000).
		Find(&titleMatches).Error; err != nil {
		return nil, 0, fmt.Errorf("search canvas titles: %w", err)
	}

	for _, m := range titleMatches {
		results = append(results, CanvasSearchResult{
			CanvasID:  m.ID,
			Title:     m.Title,
			UpdatedAt: m.UpdatedAt,
			MatchType: "title",
			MatchText: m.Title,
		})
		seen[m.ID] = true
	}

	// ── Step 2: Conversation title 匹配（排除已命中的 canvas）──
	var convMatches []struct {
		CanvasID      int64
		CanvasTitle   string
		CanvasUpdated time.Time
		ConvTitle     string
	}
	convQuery := r.db.WithContext(ctx).
		Table("conversations").
		Select("conversations.canvas_id, canvases.title AS canvas_title, canvases.updated_at AS canvas_updated, conversations.title AS conv_title").
		Joins("JOIN canvases ON canvases.id = conversations.canvas_id").
		Where("canvases.user_id = ? AND conversations.title LIKE ? AND conversations.deleted_at IS NULL AND canvases.deleted_at IS NULL",
			userID, likePattern).
		Order("canvases.updated_at DESC").
		Limit(1000)
	if err := convQuery.Find(&convMatches).Error; err != nil {
		return nil, 0, fmt.Errorf("search conversation titles: %w", err)
	}

	for _, m := range convMatches {
		if seen[m.CanvasID] {
			continue
		}
		results = append(results, CanvasSearchResult{
			CanvasID:  m.CanvasID,
			Title:     m.CanvasTitle,
			UpdatedAt: m.CanvasUpdated,
			MatchType: "conversation",
			MatchText: m.ConvTitle,
		})
		seen[m.CanvasID] = true
	}

	// ── Step 3: Message content 匹配（排除已命中的 canvas）──
	var msgMatches []struct {
		CanvasID      int64
		CanvasTitle   string
		CanvasUpdated time.Time
		Content       string
	}

	// 收集已命中的 canvas_id，用于在 SQL 层提前排除
	excludeIDs := make([]int64, 0, len(seen))
	for id := range seen {
		excludeIDs = append(excludeIDs, id)
	}

	msgQuery := r.db.WithContext(ctx).
		Table("messages").
		Select(`conversations.canvas_id,
				canvases.title AS canvas_title,
				canvases.updated_at AS canvas_updated,
				messages.content`).
		Joins("JOIN conversations ON conversations.id = messages.conversation_id").
		Joins("JOIN canvases ON canvases.id = conversations.canvas_id")

	// 构建子查询：取每个 canvas 下第一条匹配 message 的 id
	if len(excludeIDs) > 0 {
		msgQuery = msgQuery.Joins(`JOIN (
			SELECT MIN(m2.id) AS min_id
			FROM messages m2
			JOIN conversations c2 ON c2.id = m2.conversation_id
			JOIN canvases cv2 ON cv2.id = c2.canvas_id
			WHERE cv2.user_id = ? AND m2.content LIKE ? AND m2.deleted_at IS NULL
			  AND c2.deleted_at IS NULL AND cv2.deleted_at IS NULL
			  AND c2.canvas_id NOT IN (?)
			GROUP BY c2.canvas_id
			LIMIT 1000
		) first_msg ON first_msg.min_id = messages.id`, userID, likePattern, excludeIDs)
	} else {
		msgQuery = msgQuery.Joins(`JOIN (
			SELECT MIN(m2.id) AS min_id
			FROM messages m2
			JOIN conversations c2 ON c2.id = m2.conversation_id
			JOIN canvases cv2 ON cv2.id = c2.canvas_id
			WHERE cv2.user_id = ? AND m2.content LIKE ? AND m2.deleted_at IS NULL
			  AND c2.deleted_at IS NULL AND cv2.deleted_at IS NULL
			GROUP BY c2.canvas_id
			LIMIT 1000
		) first_msg ON first_msg.min_id = messages.id`, userID, likePattern)
	}

	msgQuery = msgQuery.
		Where("canvases.user_id = ? AND messages.deleted_at IS NULL AND conversations.deleted_at IS NULL AND canvases.deleted_at IS NULL",
			userID).
		Order("canvases.updated_at DESC").
		Limit(1000)
	if err := msgQuery.Find(&msgMatches).Error; err != nil {
		return nil, 0, fmt.Errorf("search message content: %w", err)
	}

	for _, m := range msgMatches {
		snippet := extractSnippet(m.Content, keyword, 40)
		results = append(results, CanvasSearchResult{
			CanvasID:  m.CanvasID,
			Title:     m.CanvasTitle,
			UpdatedAt: m.CanvasUpdated,
			MatchType: "content",
			MatchText: snippet,
		})
		seen[m.CanvasID] = true
	}

	// ── 分页 ──
	total := int64(len(results))
	start := (page - 1) * limit
	if start >= int(total) {
		return []CanvasSearchResult{}, total, nil
	}
	end := start + limit
	if end > int(total) {
		end = int(total)
	}

	return results[start:end], total, nil
}

// extractSnippet 从文本中提取关键词附近的 snippet
// 使用 []rune 进行切片操作，避免中文/emoji 等多字节字符被截断
func extractSnippet(content, keyword string, contextLen int) string {
	runes := []rune(content)
	lower := strings.ToLower(content)
	kw := strings.ToLower(keyword)
	idx := strings.Index(lower, kw)
	if idx == -1 {
		if len(runes) > 80 {
			return string(runes[:80]) + "..."
		}
		return content
	}

	// 将 byte 偏移量转换为 rune 偏移量
	runeIdx := len([]rune(content[:idx]))
	kwRuneLen := len([]rune(keyword))

	start := runeIdx - contextLen
	end := runeIdx + kwRuneLen + contextLen
	prefix := ""
	suffix := ""

	if start < 0 {
		start = 0
	} else {
		prefix = "..."
	}
	if end > len(runes) {
		end = len(runes)
	} else {
		suffix = "..."
	}

	return prefix + string(runes[start:end]) + suffix
}

// generateUniqueCanvasTitle 根据已有标题生成唯一的 "Untitled Canvas" 标题
func (r *canvasRepo) generateUniqueCanvasTitle(existingTitles []string) string {
	if len(existingTitles) == 0 {
		return "Untitled Canvas"
	}

	// 正则匹配 "Untitled Canvas" 或 "Untitled Canvas (n)"
	re := regexp.MustCompile(`^Untitled Canvas(?: \((\d+)\))?$`)
	maxNum := 0
	hasBase := false

	for _, title := range existingTitles {
		matches := re.FindStringSubmatch(title)
		if matches == nil {
			continue
		}
		if matches[1] == "" {
			// 匹配到 "Untitled Canvas"
			hasBase = true
		} else {
			// 匹配到 "Untitled Canvas (n)"
			num, _ := strconv.Atoi(matches[1])
			if num > maxNum {
				maxNum = num
			}
		}
	}

	if !hasBase && maxNum == 0 {
		return "Untitled Canvas"
	}

	return fmt.Sprintf("Untitled Canvas (%d)", maxNum+1)
}