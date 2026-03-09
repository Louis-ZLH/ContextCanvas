# Search Canvases 全文搜索页面 — 架构设计 & TODO Plan

## 背景

当前 `/canvas/search` 路由只有 `<div>Search Canvases</div>` 占位符。需要实现完整的搜索页面，支持跨 Canvas 标题、对话标题、消息内容的全文搜索，结果按匹配层级排序（标题 > 对话标题 > 消息内容），搜索关键词高亮显示，点击结果跳转至对应 Canvas。

## 需求

- 全文搜索：Canvas 标题 / Conversation 标题 / Message 内容
- 搜索结果按匹配层级排序：
  1. Canvas title 匹配 → 最高优先级
  2. Conversation title 匹配 → 次优先级
  3. Message content 匹配 → 最低优先级
- 列表视图展示结果，每个 Canvas 最多出现一次（取最高优先级匹配）
- 关键词在标题和匹配内容中高亮（accent 颜色）
- 展示匹配上下文 snippet
- 支持分页
- 点击结果跳转到对应 Canvas
- 适配三套主题（saas / dark / paper），使用 CSS 变量自动适配

---

## 一、后端改动

### 1. DTO — `internal/dto/canvas.go`

新增搜索请求参数和响应结构体：

```go
// CanvasSearchItem 搜索结果单项
type CanvasSearchItem struct {
    CanvasID  int64  `json:"canvas_id,string"`
    Title     string `json:"title"`
    UpdatedAt string `json:"updated_at"`
    MatchType string `json:"match_type"` // "title" | "conversation" | "content"
    MatchText string `json:"match_text"` // 匹配到的文本或 snippet
}

// SearchCanvasResponse 搜索响应
type SearchCanvasResponse struct {
    Results []CanvasSearchItem `json:"results"`
    Total   int64              `json:"total"`
    Page    int                `json:"page"`
    Limit   int                `json:"limit"`
}
```

### 2. Repo — `internal/repo/canvasRepo.go`

新增 `SearchCanvases` 方法，分三步查询并合并：

```go
// SearchCanvases 全文搜索，返回合并后的结果
// 搜索优先级：canvas title > conversation title > message content
func (r *canvasRepo) SearchCanvases(ctx context.Context, userID int64, keyword string, page, limit int) ([]CanvasSearchResult, int64, error)
```

**核心实现逻辑：**

```go
type CanvasSearchResult struct {
    CanvasID  int64
    Title     string
    UpdatedAt time.Time
    MatchType string // "title" | "conversation" | "content"
    MatchText string // 匹配到的文本
}

func (r *canvasRepo) SearchCanvases(ctx context.Context, userID int64, keyword string, page, limit int) ([]CanvasSearchResult, int64, error) {
    // 1. 转义 LIKE 通配符（复用 fileRepo 的模式）
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
        Limit(1000). // 安全阈值，防止内存分页时加载过多数据
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
        Limit(1000) // 安全阈值
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
    // 使用 MIN(messages.id) 子查询 + 自连接的方式获取每个 canvas 下第一条匹配
    // message 的 content，避免 correlated subquery 的性能问题，同时兼容
    // ONLY_FULL_GROUP_BY SQL mode。
    //
    // 优化点：
    // 1. 子查询内部也加 LIMIT 1000 安全阈值，防止 message 表全表扫描返回过多中间行
    // 2. 子查询通过 NOT IN 提前排除 Step 1/2 已命中的 canvas_id，减少无效 JOIN 和分组
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
        Limit(1000) // 外层安全阈值
    if err := msgQuery.Find(&msgMatches).Error; err != nil {
        return nil, 0, fmt.Errorf("search message content: %w", err)
    }

    for _, m := range msgMatches {
        // 截取 keyword 附近的 snippet（前后各 40 字符）
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
```

**Snippet 提取辅助函数：**

```go
// extractSnippet 从文本中提取关键词附近的 snippet
// 注意：使用 []rune 进行切片操作，避免中文/emoji 等多字节字符被截断
func extractSnippet(content, keyword string, contextLen int) string {
    runes := []rune(content)
    lower := strings.ToLower(content)
    kw := strings.ToLower(keyword)
    idx := strings.Index(lower, kw)
    if idx == -1 {
        // fallback: 取前 80 个字符
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
```

### 3. Service — `internal/service/canvasService.go`

新增方法签名：

```go
func (s *CanvasService) SearchCanvases(ctx context.Context, userID int64, keyword string, page, limit int) ([]repo.CanvasSearchResult, int64, error)
```

实现直接调用 repo 方法，无额外业务逻辑。

### 4. Handler — `internal/handler/canvasHandler.go`

新增 `SearchCanvases` handler：

```go
func (h *CanvasHandler) SearchCanvases(c *gin.Context) {
    userID, exists := c.Get("user_id")
    if !exists {
        c.JSON(http.StatusUnauthorized, dto.Error(apperr.BizUnauthorized, "Unauthorized"))
        return
    }

    keyword := strings.TrimSpace(c.Query("keyword"))
    if keyword == "" {
        c.JSON(http.StatusOK, dto.Success(dto.SearchCanvasResponse{
            Results: []dto.CanvasSearchItem{},
            Total:   0,
            Page:    1,
            Limit:   20,
        }))
        return
    }

    // 限制 keyword 最大长度，避免超长 LIKE pattern 导致慢查询
    if len([]rune(keyword)) > 100 {
        c.JSON(http.StatusBadRequest, dto.Error(apperr.BizInvalidParams, "Keyword too long (max 100 characters)"))
        return
    }

    page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
    limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
    if page < 1 { page = 1 }
    if limit < 1 { limit = 20 }
    if limit > 50 { limit = 50 }

    results, total, err := h.canvasService.SearchCanvases(c.Request.Context(), userID.(int64), keyword, page, limit)
    if err != nil {
        if appErr, ok := apperr.GetAppError(err); ok {
            c.JSON(appErr.Code, dto.Error(appErr.BizCode, appErr.Message))
            return
        }
        c.JSON(500, dto.Error(apperr.BizUnknown, "Internal Server Error"))
        return
    }

    // 转换为 DTO
    items := make([]dto.CanvasSearchItem, len(results))
    for i, r := range results {
        items[i] = dto.CanvasSearchItem{
            CanvasID:  r.CanvasID,
            Title:     r.Title,
            UpdatedAt: r.UpdatedAt.Format(time.RFC3339),
            MatchType: r.MatchType,
            MatchText: r.MatchText,
        }
    }

    c.JSON(http.StatusOK, dto.Success(dto.SearchCanvasResponse{
        Results: items,
        Total:   total,
        Page:    page,
        Limit:   limit,
    }))
}
```

### 5. Router — `internal/api/canvas.go`

使用独立路由前缀，**不在 canvas group 下注册**，彻底避免与 `/:id` 参数路由的冲突：

```go
// 在 canvas group 外部、同一个 auth middleware group 下注册
api.GET("/canvas-search", canvasHandler.SearchCanvases)
```

> **为什么不在 canvas group 下？**
> 当前 canvas group 已有 `GET /:id`（获取 Canvas 详情）。Gin v1.11.0 在同一路由层级混用静态路由（`/search`）和参数路由（`/:id`）时，会直接 panic（`wildcard route ':id' conflicts with existing children`）。采用独立前缀 `/canvas-search` 是最安全、改动最小的方案，无需修改已有路由结构。

### 6. Interface 更新

`CanvasService` interface（在 handler 文件中定义）和 `CanvasRepo` interface（在 service 文件中定义）都需要新增 `SearchCanvases` 方法签名。

---

## 二、前端改动

### 1. 类型定义 — `src/service/type.ts`

```typescript
// 搜索结果单项
export interface CanvasSearchItem {
  canvasId: string;
  title: string;
  updatedAt: string;
  matchType: "title" | "conversation" | "content";
  matchText: string;
}

// 搜索响应
export interface CanvasSearchResponse {
  results: CanvasSearchItem[];
  total: number;
  page: number;
  limit: number;
}
```

### 2. API 调用 — `src/service/canvas.ts`

```typescript
export async function searchCanvases(params: {
  keyword: string;
  page: number;
  limit: number;
}): Promise<{ success: boolean; message: string; data: CanvasSearchResponse | null }> {
  try {
    const query = new URLSearchParams({
      keyword: params.keyword,
      page: String(params.page),
      limit: String(params.limit),
    });
    const response = await apiRequest<JSONResponse>(`/api/canvas-search?${query}`, {
      method: "GET",
    });
    if (response.code !== 0) {
      return { success: false, message: response.message, data: null };
    }
    return { success: true, message: response.message, data: toCamelCase(response.data) as CanvasSearchResponse };
  } catch (error: unknown) {
    if (error instanceof Error) {
      return { success: false, message: error.message, data: null };
    }
    return { success: false, message: "Failed to search canvases", data: null };
  }
}
```

### 3. React Query — `src/query/canvas.ts`

```typescript
export function searchCanvasQueryOptions(params: { keyword: string; page: number; limit: number }) {
  return queryOptions({
    queryKey: ["canvas", "search", params], // 使用对象形式，与 fileListQueryOptions 保持一致
    queryFn: () => searchCanvases(params),
    staleTime: 2 * 60 * 1000,
    enabled: params.keyword.length > 0,
    retry: false, // 与 canvasListQueryOptions / fileListQueryOptions 保持一致，避免 auth 失败时反复重试
  });
}
```

### 4. 页面组件 — `src/view/canvas/SearchCanvases.tsx`

```
SearchCanvases
├── 页面标题 "Search Canvases"
├── 搜索栏（带 debounce 300ms + 清空按钮）
├── 加载状态（skeleton list）
├── 空状态
│   ├── 无关键词：提示开始搜索
│   └── 有关键词无结果：提示未找到
├── 结果列表
│   └── SearchResultRow（每个结果一行）
│       ├── 左侧
│       │   ├── Canvas 标题（关键词高亮）
│       │   ├── 匹配类型标签（title / conversation / content）
│       │   └── 匹配内容 snippet（关键词高亮）
│       └── 右侧
│           └── 更新时间
└── 分页控件（复用 MyResource 样式）
```

**关键词高亮方案：**

```tsx
function HighlightText({ text, keyword }: { text: string; keyword: string }) {
  if (!keyword) return <>{text}</>;

  const regex = new RegExp(`(${escapeRegex(keyword)})`, "gi");
  const parts = text.split(regex);

  return (
    <>
      {parts.map((part, i) =>
        // 注意：不能直接用 regex.test(part)，因为带 g flag 的正则会维护 lastIndex 状态，
        // 导致在 map 迭代中交替返回 true/false。改用大小写不敏感的字符串比较。
        part.toLowerCase() === keyword.toLowerCase() ? (
          <span key={i} className="text-accent font-semibold">{part}</span>
        ) : (
          <span key={i}>{part}</span>
        )
      )}
    </>
  );
}

function escapeRegex(str: string) {
  return str.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}
```

**匹配类型标签样式：**

| MatchType | 标签文案 | 样式 |
|-----------|---------|------|
| `title` | Canvas Title | accent 主题色 badge |
| `conversation` | Conversation | 次级色 badge |
| `content` | Message | 淡灰色 badge |

**结果行 UI（列表视图）：**

```
┌──────────────────────────────────────────────────────────────────┐
│ [Canvas Title] 小狗123 的故事                     2024-03-08    │
│ Canvas Title   "...关于小狗的讨论记录..."                        │
└──────────────────────────────────────────────────────────────────┘
```

- 第一行：Canvas 标题（关键词用 accent 色高亮）+ 右侧更新时间
- 第二行：匹配类型 badge + 匹配内容 snippet（如果是 title 匹配可省略 snippet，因为标题本身已高亮）
- 点击整行跳转到 `/canvas/:canvas_id`
- Hover 时底色变化

### 5. 路由更新 — `src/router/router.tsx`

```tsx
{
  path: "search",
  element: <SearchCanvases />,
},
```

用 `React.lazy()` 做代码分割（可选，和其他页面保持一致）。

---

## 三、关键设计决策

| 决策项 | 选择 | 理由 |
|--------|------|------|
| 搜索实现 | SQL LIKE（三步查询 + Go 层合并去重） | 实现简单，数据量当前可控；未来可迁移至全文索引 |
| 排序策略 | match_type 优先级 > updated_at DESC | 用户期望标题精确匹配排最前 |
| 去重策略 | 同一 Canvas 仅保留最高优先级匹配 | 避免同一 Canvas 重复出现 |
| 分页位置 | Go 层内存分页（在合并去重后） | 三步查询结果无法在 SQL 层统一分页 |
| Snippet 长度 | keyword 前后各 40 字符 | 兼顾上下文和展示空间 |
| 前端高亮 | 正则 split + accent 色 `<span>` | 简单可靠，无额外依赖 |
| 搜索防抖 | 300ms debounce | 和 MyResource 一致，减少无效请求 |
| 空 keyword | 直接返回空结果（不查询） | 避免无意义的全量查询 |
| 路由方案 | 独立前缀 `GET /canvas-search`（不在 canvas group 下） | Gin v1.11.0 在同层级混用 `/search` 和 `/:id` 会 panic，独立路由彻底避免冲突 |

---

## 四、TODO 清单

### Phase 1: 后端 — DTO & Repo

- [x] `internal/dto/canvas.go` 新增 `CanvasSearchItem` + `SearchCanvasResponse`
- [x] `internal/repo/canvasRepo.go` 新增 `extractSnippet` 辅助函数
- [x] `internal/repo/canvasRepo.go` 新增 `SearchCanvases` 方法（三步查询 + 合并去重 + 内存分页）

### Phase 2: 后端 — Service & Handler & Router

- [x] 更新 `CanvasRepo` interface 新增 `SearchCanvases` 签名
- [x] `internal/service/canvasService.go` 新增 `SearchCanvases` 方法
- [x] 更新 `CanvasService` interface 新增 `SearchCanvases` 签名
- [x] `internal/handler/canvasHandler.go` 新增 `SearchCanvases` handler
- [x] `internal/api/canvas.go` 注册 `GET /canvas-search`（独立路由前缀，不在 canvas group 下，避免与 `/:id` 路由冲突）

### Phase 3: 前端 — Service & Query

- [x] `src/service/type.ts` 新增 `CanvasSearchItem` + `CanvasSearchResponse` 类型
- [x] `src/service/canvas.ts` 新增 `searchCanvases()` 函数
- [x] `src/query/canvas.ts` 新增 `searchCanvasQueryOptions`

### Phase 4: 前端 — 页面组件

- [x] `src/view/canvas/SearchCanvases.tsx` 实现完整页面
  - [x] 搜索栏（debounce + 清空）
  - [x] Loading skeleton
  - [x] 空状态（无关键词 / 无结果）
  - [x] 结果列表 + 关键词高亮
  - [x] 匹配类型 badge
  - [x] 分页控件
  - [x] 点击跳转
- [x] `src/router/router.tsx` 更新 search 路由指向 SearchCanvases 组件

### Phase 5: 测试验证

- [ ] 后端 API 手动测试：空 keyword、超长 keyword（>100 字符）、keyword 含特殊字符（`%`、`_`、`\`）、跨三种匹配类型
- [ ] 前端功能测试：搜索、高亮、分页、空状态、跳转
- [ ] 主题测试：saas / dark / paper 三套主题下高亮和 badge 颜色正确

---

## 五、性能与扩展性说明

**当前方案（SQL LIKE + Go 合并）适用条件：**
- 用户级数据量（非全局搜索），通常 Canvas 数 < 1000
- 已有索引：`user_id` 在 canvases、conversations（via canvas_id）、messages（via conversation_id）上已建索引
- Message content LIKE 是性能瓶颈点，但单用户消息量可控
- Step 3 子查询通过 `NOT IN` 排除 Step 1/2 已命中的 canvas_id，避免对已命中 canvas 下的 message 做冗余 LIKE 扫描
- Step 3 子查询内部和外层均设置了 `LIMIT 1000` 双重安全阈值

**已知限制：**
- **total 可能不精确**：每步查询设置了 `Limit(1000)` 安全阈值，如果某一步实际匹配超过 1000 条，返回的 `total` 会被截断（不含超出部分）。对于当前用户级数据量（Canvas < 1000）此场景极不可能发生，但需在文档中明确此行为。如未来数据量增长超过阈值，需改用 SQL 层分页或提高阈值
- **LIKE 前缀通配符无法利用索引**：`%keyword%` 模式无法利用 B-Tree 索引，只能全表扫描。当前单用户数据量可控，但 Message 表数据增长后可能成为瓶颈

**未来优化方向（如数据量增长）：**
- 为 `canvases.title`、`conversations.title`、`messages.content` 添加 MySQL FULLTEXT INDEX
- 使用 `MATCH ... AGAINST` 替代 `LIKE`
- 或引入 Elasticsearch/Meilisearch 做专用搜索服务

---

## 六、Review 注意事项（已在文档中修正）

以下问题在 code review 中发现，已直接在上方对应代码段中修正：

### 后端

| # | 问题 | 严重程度 | 修正内容 |
|---|------|---------|---------|
| 1 | **路由文件路径错误** | 中 | Canvas 路由实际定义在 `internal/api/canvas.go`，非 `router.go`。已修正第五节标题和 TODO 清单 |
| 2 | **内存分页缺少安全阈值** | 中 | 三步查询会将所有匹配结果加载到内存再分页，如果某用户消息量很大会导致内存压力。已在每步查询添加 `.Limit(1000)` 保护 |
| 3 | **`extractSnippet` 多字节字符截断** | 中 | 原实现按 byte 切片 `content[start:end]`，对中文/emoji 等多字节字符会截断为乱码。已改用 `[]rune` 操作 |
| 4 | **Step 3 message 查询返回冗余行** | 低 | 同一 canvas 下多条 message 命中时，SQL 会返回多行同 `canvas_id`。虽然 Go 层 `seen` map 能去重，但会传输冗余数据。已添加 `GROUP BY` |
| 5 | **Step 3 `GROUP BY` 在 strict SQL mode 下报错** | 高 | 原 `GROUP BY conversations.canvas_id` 仅包含一列，但 SELECT 了 `canvases.title`、`canvases.updated_at`、`messages.content`，在 `ONLY_FULL_GROUP_BY` 模式（MySQL 5.7+ 默认）下会报错。已改为子查询取 content + 完整 GROUP BY 列 |
| 6 | **缺少 keyword 最大长度校验** | 中 | Handler 只检查了空字符串，超长 keyword 会生成超长 LIKE pattern 导致慢查询。已在 Handler 中增加 `len([]rune(keyword)) > 100` 校验 |
| 7 | **Gin 路由冲突风险** | 高→已解决 | Gin v1.11.0 在同层级混用 `/search` 和 `/:id` 会 panic。已改用独立路由前缀 `GET /canvas-search`，彻底避免冲突 |
| 8 | **Repo 层查询缺少 error 处理** | 中 | 三步查询的 `.Find()` 未检查返回的 `error`，数据库异常时会静默忽略。已为每步查询添加 `if err != nil` 检查并返回包装后的错误 |
| 9 | **Step 3 correlated subquery 性能问题** | 中 | 原方案使用 correlated subquery 对每行 GROUP BY 结果执行子查询，数据量大时效率低。已改为 `MIN(id)` 子查询 + 自连接方式，一次查询完成 |
| 10 | **Step 3 子查询缺少安全阈值** | 中 | 外层查询有 `Limit(1000)`，但子查询 `SELECT MIN(m2.id) ... GROUP BY c2.canvas_id` 无限制，message 表数据量大时子查询本身会扫描大量行。已在子查询内部添加 `LIMIT 1000` |
| 11 | **Step 3 子查询扫描已命中的 canvas** | 中 | Step 1/2 已命中的 canvas 在 Go 层通过 `seen` map 去重，但 SQL 层仍会对这些 canvas 下的 message 做 LIKE 扫描，浪费 IO。已将 `excludeIDs` 通过 `NOT IN` 下推到子查询，在 SQL 层提前排除，减少无效扫描和 GROUP BY 开销 |

### 前端

| # | 问题 | 严重程度 | 修正内容 |
|---|------|---------|---------|
| 1 | **`HighlightText` 的 `regex.test` lastIndex bug** | 中 | 带 `g` flag 的正则在 `map` 迭代中反复调用 `regex.test()` 会因 `lastIndex` 状态交替返回 true/false，导致部分高亮丢失。已改为字符串大小写不敏感比较 |
| 2 | **主题数量不准确** | 低 | 项目实际有三套主题（saas / dark / paper），非两套。由于使用 CSS 变量（`text-accent`、`border-main` 等）会自动适配，无需特殊处理，但测试时需覆盖三套主题 |
| 3 | **`queryKey` 格式不一致** | 低 | 原 `queryKey` 将 params 展开为 `[..., params.keyword, params.page, params.limit]`，与 `fileListQueryOptions` 中使用对象形式 `[..., params]` 不一致。已改为对象形式 |
| 4 | **`searchCanvasQueryOptions` 缺少 `retry: false`** | 低 | 与 `canvasListQueryOptions` / `fileListQueryOptions` 不一致，auth 失败时会反复重试。已添加 `retry: false` |
| 5 | **API URL 未同步路由变更** | 中 | 后端路由改为独立前缀 `/canvas-search` 后，前端 API 调用的 URL 需同步修改为 `/api/canvas-search`。已修正 |

### 实现备注

- **路由占位符已存在**：`src/router/router.tsx` 中 `path: "search"` 已注册（当前渲染 `<div>Search Canvases</div>` 占位），实现时只需替换 `element` 即可
- **Sidebar 入口已存在**：`src/ui/layout/Sidebar.tsx` 中 Search Canvases 导航链接已实现，无需额外工作
- **`useDebounce` 复用**：目前 `useDebounce` hook 定义在 `MyResource.tsx` 内部。如果仅 SearchCanvases 一处复用，直接在 `SearchCanvases.tsx` 中重新定义即可，避免额外改动；如果后续更多页面需要，再提取到 `src/hooks/useDebounce.ts` 作为公共 hook
- **`Conversation.ID` 类型差异**：Conversation 的主键是 `string`（varchar(21)，nanoid），而非 Canvas 的 `int64` 雪花 ID。当前 JOIN 查询不受影响，但开发时需注意模型差异
- **Interface 声明位置**：`canvasRepo` interface 定义在 `internal/service/canvasService.go` 中（作为 service 层的依赖），`CanvasService` interface 定义在 `internal/handler/canvasHandler.go` 中（作为 handler 层的依赖），两处都需要新增 `SearchCanvases` 方法签名
