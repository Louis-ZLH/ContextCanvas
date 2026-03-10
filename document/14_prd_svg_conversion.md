# SVG 文件转换：上传时通过 RabbitMQ 异步转 JPEG

## 背景

用户上传 SVG 文件后，当该文件被引用到对话上下文中时，AI（Gemini）返回错误：

```
400 INVALID_ARGUMENT: Unsupported MIME type: image/svg+xml
```

**根因分析**：当前 SVG 上传后原样存入 MinIO，在组装对话上下文时，`isOriginalImage(ct)` 对所有 `image/*` 返回 `true`，导致 SVG 被 base64 编码为 `data:image/svg+xml;base64,...` 直接发给 AI。Gemini / Claude 等模型不支持 SVG MIME type，只支持 JPEG / PNG / GIF / WebP。

## 当前架构

### 上传流程（Go back-end）

```
fileService.UploadFile()
  ├─ 验证 (size, extension, MIME, quota)
  ├─ 类型处理:
  │   ├─ PDF / PPTX / DOCX / XLSX → 额外校验
  │   ├─ isCompressibleImage (排除 SVG) → 压缩为 JPEG
  │   └─ SVG → 不做任何处理，原样上传 ← 问题所在
  ├─ 上传到 MinIO
  ├─ 写入 DB
  └─ needsPreprocessing? → 仅 PDF/DOCX/XLSX/PPTX → 发 RabbitMQ
```

### 上下文组装流程（Go back-end）

```
getResourceNodeContent()
  ├─ isOriginalText(ct) → 读原文本
  ├─ isOriginalImage(ct) → base64 原图发给 AI  ← SVG 走这里
  └─ default → getConvertedContent() (等待转换结果)
```

### 文件转换流程（Python ai-service）

```
RabbitMQ: ai.file.convert
  → file_convert_callback()
    → _CONVERTERS[content_type](file_bytes)
    → 写入 MinIO: {path}_text.txt / {path}_pages/page_N.jpg
    → 释放 Redis 锁 → ACK
```

当前 `_CONVERTERS` 支持：PDF、DOCX、XLSX、PPTX。不支持 SVG。

## 改动方案

### 总览

| 文件 | 改动 | 说明 |
|------|------|------|
| `back-end/.../fileService.go` | +1 行 | `needsPreprocessing()` 增加 SVG |
| `back-end/.../conversationService.go` | +3 行 | SVG 路由到 `getConvertedContent` |
| `ai-service/.../file_convert_consumer.py` | +16 行 | 新增 `_convert_svg`（含白底 + 输出宽度）+ 注册 |
| `ai-service/requirements.txt` | +1 行 | 添加 `cairosvg` |
| `ai-service/Dockerfile` | 修改 1-2 行 | 添加 `libcairo2` 系统依赖（多阶段构建时 runtime 阶段单独加） |

### 1. back-end: `fileService.go`

**文件**：`internal/service/fileService.go`

在 `needsPreprocessing()` 函数中增加 SVG 判断，使 SVG 上传后触发 RabbitMQ 消息：

```go
// 改前
func needsPreprocessing(contentType string) bool {
    ct := strings.ToLower(contentType)
    return ct == "application/pdf" ||
        isDocxContentType(ct) ||
        isXlsxContentType(ct) ||
        isPPTContentType(ct)
}

// 改后
func needsPreprocessing(contentType string) bool {
    ct := strings.ToLower(contentType)
    return ct == "application/pdf" ||
        ct == "image/svg+xml" ||
        isDocxContentType(ct) ||
        isXlsxContentType(ct) ||
        isPPTContentType(ct)
}
```

SVG 上传时仍然原样存入 MinIO（不压缩），然后通过 RabbitMQ 发送 `{file_id, minio_path, "image/svg+xml"}` 给 ai-service 异步转换。

### 2. back-end: `conversationService.go`

**文件**：`internal/service/conversationService.go`

在 `getResourceNodeContent()` 中，将 SVG 路由到 `getConvertedContent()`，而不是 `getOriginalImageContent()`：

```go
// 改前
switch {
case isOriginalText(ct):
    return s.getOriginalTextContent(ctx, file)
case isOriginalImage(ct):
    return s.getOriginalImageContent(ctx, file)
default:
    return s.getConvertedContent(ctx, file, eventCh)
}

// 改后
switch {
case isOriginalText(ct):
    return s.getOriginalTextContent(ctx, file)
case ct == "image/svg+xml":
    return s.getConvertedContent(ctx, file, eventCh)
case isOriginalImage(ct):
    return s.getOriginalImageContent(ctx, file)
default:
    return s.getConvertedContent(ctx, file, eventCh)
}
```

**关键**：SVG case 必须在 `isOriginalImage(ct)` 之前，因为 `isOriginalImage` 对所有 `image/*` 返回 `true`。路由到 `getConvertedContent()` 后，会自动等待转换完成并读取 `{path}_pages/page_1.jpg`。

### 3. ai-service: `file_convert_consumer.py`

**文件**：`services/file_convert_consumer.py`

新增 SVG 转换函数，使用 `cairosvg` 将 SVG 渲染为 PNG，再通过已有的 `_compress_image()` 压缩为 JPEG：

```python
def _convert_svg(file_bytes: bytes) -> tuple[str | None, list[tuple[str, bytes]] | None]:
    """Convert SVG to a single JPEG page image via cairosvg."""
    import cairosvg

    # SVG → PNG
    # - output_width=1568: 确保输出分辨率充足（部分 SVG 无显式 width/height，仅有 viewBox，默认渲染可能很小）
    # - background_color="white": 填充白色背景，避免透明区域在 PNG→JPEG 转换时变黑色
    #   （白色线条 + 透明背景的架构图等场景下，黑底会导致内容几乎不可见）
    png_bytes = cairosvg.svg2png(
        bytestring=file_bytes,
        output_width=1568,
        background_color="white",
    )

    # PNG → JPEG (复用已有的压缩逻辑：max 1568px, quality 80)
    jpeg_bytes = _compress_image(png_bytes)

    return None, [("page_1.jpg", jpeg_bytes)]
```

在 `_CONVERTERS` 字典中注册：

```python
_CONVERTERS = {
    "application/pdf": _convert_pdf,
    "application/vnd.openxmlformats-officedocument.wordprocessingml.document": _convert_docx,
    "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet": _convert_xlsx,
    "application/vnd.openxmlformats-officedocument.presentationml.presentation": _convert_pptx,
    "image/svg+xml": _convert_svg,  # 新增
}
```

无需修改 `file_convert_callback`，现有的消息处理管道（下载 → 转换 → 上传 → 释放锁 → ACK）完全复用。

### 4. ai-service: `requirements.txt`

新增依赖：

```
cairosvg==2.7.1
```

### 5. ai-service: `Dockerfile`

当前 Dockerfile 为单阶段构建，在现有 `apt-get install` 行末追加 Cairo 相关依赖即可：

```dockerfile
# 改前
RUN apt-get update && \
    apt-get install -y --no-install-recommends libreoffice-core libreoffice-impress && \
    apt-get clean && rm -rf /var/lib/apt/lists/*

# 改后
RUN apt-get update && \
    apt-get install -y --no-install-recommends libreoffice-core libreoffice-impress \
    libcairo2 libcairo2-dev libffi-dev && \
    apt-get clean && rm -rf /var/lib/apt/lists/*
```

- `libcairo2`：Cairo 渲染库（cairosvg 运行时依赖）
- `libcairo2-dev`：cairocffi 编译所需头文件（`pip install cairosvg` 时需要）
- `libffi-dev`：CFFI 后端在 slim 镜像上编译所需

## 数据流

```
用户上传 SVG
       │
       ▼
 fileService.UploadFile()
       │
       ├─ 原样上传到 MinIO
       ├─ 写入 DB (content_type: "image/svg+xml")
       ├─ needsPreprocessing("image/svg+xml") → true
       │   ├─ SET Redis: file:wait_to_process:{id}, file:processing:{id}
       │   └─ Publish RabbitMQ: ai.file.convert
       │       {file_id, minio_path, "image/svg+xml"}
       ▼
   RabbitMQ (file_convert_queue)
       │
       ▼
 ai-service: file_convert_callback()
       │
       ├─ 幂等检查 (Redis)
       ├─ _CONVERTERS["image/svg+xml"] → _convert_svg()
       │   ├─ cairosvg.svg2png(bytestring=..., output_width=1568, background_color="white")
       │   └─ _compress_image(png) → JPEG
       ├─ 写入 MinIO: {minio_path}_pages/page_1.jpg
       ├─ 释放 Redis 锁
       └─ ACK

对话引用 SVG 文件
       │
       ▼
 getResourceNodeContent()
       │
       ├─ ct == "image/svg+xml" → getConvertedContent()
       │   ├─ 等待处理完成 (检查 Redis 锁)
       │   └─ 读取 {minio_path}_pages/page_1.jpg
       │       → base64 → data:image/jpeg;base64,...
       └─ 返回给 AI ✓
```

## 边界情况

| 场景 | 处理方式 |
|------|----------|
| SVG 含外部引用 (image href, @import) | cairosvg 不解析外部 URL，忽略缺失资源。可接受：避免 SSRF 风险 |
| 超大 SVG | 已有 5MB 上传限制。cairosvg 内存处理后经 _compress_image 压缩到 ≤1568px JPEG |
| 畸形 SVG | cairosvg 抛异常 → callback 的 try/except 捕获 → 清理 Redis → reject 到 DLQ |
| 透明背景 | `cairosvg.svg2png(background_color="white")` 填充白色背景，避免透明区域变黑色导致内容不可见 |
| SVG 无显式尺寸 (仅有 viewBox) | `cairosvg.svg2png(output_width=1568)` 强制输出宽度，避免默认渲染尺寸过小 |
| 动画 SVG (SMIL) | cairosvg 只渲染首帧静态图。可接受 |
| 幂等性 | 已有 Redis `file:wait_to_process:{id}` 检查，重复消费自动跳过 |
| 已上传的历史 SVG | 不会被追溯转换，需要用户重新上传 |

## 部署顺序

1. **先部署 ai-service**（可独立部署）
   - 更新 Dockerfile + requirements.txt + file_convert_consumer.py
   - 此时 ai-service 能处理 SVG 转换消息，但还没有人发送

2. **再部署 back-end**
   - 更新 fileService.go + conversationService.go
   - 新上传的 SVG 开始触发转换；对话引用 SVG 走转换后的 JPEG

## 改动量

总计约 **21 行有效代码**，横跨 5 个文件。完全复用现有的 RabbitMQ 消息管道、Redis 幂等机制、MinIO 存储模式和前端展示逻辑，无需新增基础设施。
