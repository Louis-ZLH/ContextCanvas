package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/luhao/contextGraph/internal/dto"
	"github.com/luhao/contextGraph/internal/infra"
	"github.com/luhao/contextGraph/internal/model"
	apperr "github.com/luhao/contextGraph/pkg/errors"
	"github.com/luhao/contextGraph/pkg/idgen"
	"github.com/luhao/contextGraph/pkg/tokenutil"
	"gorm.io/gorm"
)

const perMessageOverhead = 4 // 每条消息的 role / 结构开销 token 数

const nodeSummaryFailSentinel = "__fail__" // node summary 生成失败哨兵值

type conversationRepo interface {
	CreateConversation(ctx context.Context, conversationID string, canvasID int64, title string) (model.Conversation, model.Message, error)
	GetConversationByID(ctx context.Context, conversationID string) (model.Conversation, error)
	GetMessagesByConversationID(ctx context.Context, conversationID string) ([]model.Message, error)
	CreateMessage(ctx context.Context, msg *model.Message) error
	GetMessageByID(ctx context.Context, id int64) (model.Message, error)
	UpdateCurrentLeafID(ctx context.Context, conversationID string, leafID int64) error
	UpdateTitle(ctx context.Context, conversationID string, title string) error
	UpdateMessageSummary(ctx context.Context, messageID int64, summary string) error

	// Message-level summary 锁
	AcquireMsgSummaryLock(ctx context.Context, conversationID string) (bool, error)
	ReleaseMsgSummaryLock(ctx context.Context, conversationID string) error
	IsMsgSummaryLocked(ctx context.Context, conversationID string) (bool, error)

	// Node-level summary 缓存
	GetNodeSummaryCache(ctx context.Context, conversationID string, leafID int64) (string, error)
	SetNodeSummaryCache(ctx context.Context, conversationID string, leafID int64, summary string) error
	DeleteNodeSummaryCache(ctx context.Context, conversationID string, leafID int64) error

	// Node-level summary 生成锁
	AcquireNodeSummaryLock(ctx context.Context, conversationID string, leafID int64) (bool, error)
	ReleaseNodeSummaryLock(ctx context.Context, conversationID string, leafID int64) error
	IsNodeSummaryLocked(ctx context.Context, conversationID string, leafID int64) (bool, error)

	// Title 生成锁
	AcquireGenTitleLock(ctx context.Context, conversationID string) (bool, error)
}

type ai interface {
	GenerateTitle(ctx context.Context, messages []infra.ChatMessage) (string, error)
	StreamChat(ctx context.Context, messages []infra.ChatMessage, model int) (<-chan infra.AIStreamEvent, error)
	GenerateSummary(ctx context.Context, messages []infra.ChatMessage, previousSummary *string, summaryType string) (string, error)
}

// fileContentRepo 定义 ConversationService 获取文件内容所需的方法
type fileContentRepo interface {
	GetFileByID(ctx context.Context, fileID int64) (*model.File, error)
	GetFileTextCache(ctx context.Context, fileID int64) (string, error)
	SetFileTextCache(ctx context.Context, fileID int64, text string) error
	GetFileSummaryCache(ctx context.Context, fileID int64) (string, error)
	SetFileSummaryCache(ctx context.Context, fileID int64, summary string) error
	IsFileProcessing(ctx context.Context, fileID int64) (bool, error)
	GetMinioObjectBytes(ctx context.Context, minioPath string) ([]byte, error)
	MinioObjectExists(ctx context.Context, minioPath string) (bool, error)
	ListMinioObjects(ctx context.Context, prefix string) ([]string, error)
}

type ConversationService struct {
	conversationRepo conversationRepo
	canvasRepo       canvasRepo
	fileContentRepo  fileContentRepo
	ai               ai
}

func NewConversationService(conversationRepo conversationRepo, canvasRepo canvasRepo, fileContentRepo fileContentRepo, ai ai) *ConversationService {
	return &ConversationService{
		conversationRepo: conversationRepo,
		canvasRepo:       canvasRepo,
		fileContentRepo:  fileContentRepo,
		ai:               ai,
	}
}

func (s *ConversationService) CreateConversation(
	ctx context.Context,
	userID int64,
	conversationID string,
	canvasID int64,
	content string,
) (conversation model.Conversation, rootMessage model.Message, err error) {
	// 1. 验证用户对画布的访问权限
	if owned, err := s.canvasRepo.CheckCanvasOwnership(ctx, canvasID, userID); err != nil {
		return model.Conversation{}, model.Message{}, err
	} else if !owned {
		return model.Conversation{}, model.Message{}, apperr.Unauthorized("User does not have access to this canvas")
	}

	// 2. 创建会话和根消息（title 留空，由 SendMessage 并行生成）
	conversation, rootMessage, err = s.conversationRepo.CreateConversation(ctx, conversationID, canvasID, "")
	if err != nil {
		return model.Conversation{}, model.Message{}, err
	}

	return conversation, rootMessage, nil
}

func (s *ConversationService) GetConversationHistory(ctx context.Context, userID int64, conversationID string) ([]model.Message, error) {
	// 1. 查询会话信息（不存在则返回 nil, nil，handler 返回 data: null）
	conversation, err := s.conversationRepo.GetConversationByID(ctx, conversationID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	// 2. 验证用户对画布的访问权限
	if owned, err := s.canvasRepo.CheckCanvasOwnership(ctx, conversation.CanvasID, userID); err != nil {
		return nil, err
	} else if !owned {
		return nil, apperr.Unauthorized("User does not have access to this canvas")
	}

	// 3. 查询消息历史
	messages, err := s.conversationRepo.GetMessagesByConversationID(ctx, conversationID)
	if err != nil {
		return nil, err
	}

	return messages, nil
}

// SendMessage 处理发送消息的完整流程，通过 eventCh 向 handler 推送 SSE 事件。
// 调用方负责 close eventCh 前的 SSE headers 设置和 [DONE] 发送。
func (s *ConversationService) SendMessage(
	ctx context.Context,
	userID int64,
	req dto.SendMessageRequest,
	eventCh chan<- dto.SSEEvent,
) {
	defer close(eventCh)

	// 1. 权限校验
	conv, err := s.conversationRepo.GetConversationByID(ctx, req.ConversationID)
	if err != nil {
		eventCh <- dto.SSEEvent{Type: "error", Data: dto.ErrorData{Message: "Conversation not found"}}
		return
	}
	if owned, err := s.canvasRepo.CheckCanvasOwnership(ctx, conv.CanvasID, userID); err != nil || !owned {
		eventCh <- dto.SSEEvent{Type: "error", Data: dto.ErrorData{Message: "Unauthorized"}}
		return
	}

	// 2. 保存 user message
	userMsg := model.Message{
		ConversationID: req.ConversationID,
		ParentID:       &req.ParentID,
		Role:           "user",
		Content:        req.Content,
		Model:          &req.Model,
		Status:         "completed",
	}
	if err := s.conversationRepo.CreateMessage(ctx, &userMsg); err != nil {
		eventCh <- dto.SSEEvent{Type: "error", Data: dto.ErrorData{Message: "Failed to save user message"}}
		return
	}

	// 3. 预生成 assistant message ID（提前，用于 defer 兜底 + 通过 user_message 传给前端）
	assistantMsgID := idgen.GenID()
	var assistantWritten bool
	var deferErrMsg string

	// 兜底闭包：写入 error assistant + 更新 leaf
	writeErrAssistant := func(content string, status string) string {
		if assistantWritten {
			return ""
		}
		errMsg := model.Message{
			ConversationID: req.ConversationID,
			ParentID:       &userMsg.ID,
			Role:           "assistant",
			Content:        content,
			Model:          &req.Model,
			Status:         status,
		}
		errMsg.ID = assistantMsgID
		_ = s.conversationRepo.CreateMessage(context.Background(), &errMsg)
		_ = s.conversationRepo.UpdateCurrentLeafID(context.Background(), req.ConversationID, errMsg.ID)
		assistantWritten = true
		return strconv.FormatInt(assistantMsgID, 10)
	}

	// 安全网：函数退出时，如果 assistant 未被写入，自动补写 + 统一发送 SSE error
	defer func() {
		msgIDStr := writeErrAssistant("", "error")
		if msgIDStr == "" {
			return // assistantWritten 已为 true，无需发送
		}
		if deferErrMsg == "" {
			deferErrMsg = "Internal error"
		}
		eventCh <- dto.SSEEvent{
			Type: "error",
			Data: dto.ErrorData{
				Message:   deferErrMsg,
				MessageID: msgIDStr,
			},
		}
	}()

	// 4. 发送 user_message 事件（携带 assistantMsgID，让前端 abort2 可直接 confirm）
	eventCh <- dto.SSEEvent{
		Type: "user_message",
		Data: dto.UserMessageEvent{
			FullMessage:    modelToFullMessage(userMsg),
			AssistantMsgID: assistantMsgID,
		},
	}

	if err := s.conversationRepo.UpdateCurrentLeafID(ctx, req.ConversationID, userMsg.ID); err != nil {
		deferErrMsg = "Failed to update conversation"
		return
	}

	// 5. 并发组装上下文（assemblyTimer 30s）
	chatMessages, rawMessages, fullRoundCount, assemblyErr := s.assembleContext(ctx, req.ConversationID, userMsg.ID, req.ParentDelta, eventCh)
	if assemblyErr != nil {
		deferErrMsg = assemblyErr.Error()
		return
	}

	// 6. 后置触发 + 硬截断保护
	parentContextTokens := s.countParentContextTokens(chatMessages, len(rawMessages))
	s.maybeTriggerSummary(ctx, req.ConversationID, userMsg.ID, chatMessages, rawMessages, parentContextTokens, fullRoundCount)
	chatMessages, err = s.hardTruncate(chatMessages, len(rawMessages))
	if err != nil {
		deferErrMsg = err.Error()
		return
	}

	// 7. title WaitGroup（complete 后异步生成，defer Wait 保证 eventCh 关闭前完成）
	var titleWg sync.WaitGroup
	defer titleWg.Wait()

	// 8. 发送 thinking 事件，前端从 summarizing 切换到 thinking
	eventCh <- dto.SSEEvent{Type: "thinking"}

	// 9. 调用 AI 获取 stream（支持首 token 超时取消）
	aiCtx, aiCancel := context.WithCancel(ctx)
	defer aiCancel()

	aiCh, err := s.ai.StreamChat(aiCtx, chatMessages, req.Model)
	if err != nil {
		deferErrMsg = "Failed to call AI service"
		return
	}

	// 10. 首 token 超时保护：30s 内未收到任何事件则取消 AI 请求
	var firstTokenReceived bool
	var timedOut atomic.Bool
	firstTokenTimer := time.AfterFunc(30*time.Second, func() {
		timedOut.Store(true)
		aiCancel()
	})
	defer firstTokenTimer.Stop()

	// 11. 消费 AI 事件，转发给 handler
	var fullContent strings.Builder
	for evt := range aiCh {
		switch evt.Type {
		case "token":
			if !firstTokenReceived {
				firstTokenReceived = true
				firstTokenTimer.Stop()
			}
			fullContent.WriteString(evt.Content)
			eventCh <- dto.SSEEvent{
				Type: "token",
				Data: dto.TokenData{
					Content:   evt.Content,
					MessageID: strconv.FormatInt(assistantMsgID, 10),
				},
			}
		case "tool_call":
			if !firstTokenReceived {
				firstTokenReceived = true
				firstTokenTimer.Stop()
			}
			eventCh <- dto.SSEEvent{
				Type: "tool_call",
				Data: dto.ToolCallData{Content: evt.Content},
			}
		case "complete":
			firstTokenTimer.Stop()
			assistantMsg := model.Message{
				ConversationID:   req.ConversationID,
				ParentID:         &userMsg.ID,
				Role:             "assistant",
				Content:          fullContent.String(),
				Model:            &req.Model,
				Status:           "completed",
				PromptTokens:     evt.PromptTokens,
				CompletionTokens: evt.CompletionTokens,
			}
			assistantMsg.ID = assistantMsgID
			if err := s.conversationRepo.CreateMessage(ctx, &assistantMsg); err != nil {
				deferErrMsg = "Failed to save assistant message"
				return
			}
			assistantWritten = true
			_ = s.conversationRepo.UpdateCurrentLeafID(ctx, req.ConversationID, assistantMsg.ID)

			eventCh <- dto.SSEEvent{
				Type: "complete",
				Data: modelToFullMessage(assistantMsg),
			}

			// 异步生成 title（complete 后触发，用 userMsg + AI 完整回复）
			if req.GenerateTitle {
				titleWg.Add(1)
				go func() {
					defer titleWg.Done()
					titleCtx, titleCancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer titleCancel()

					// Redis SETNX 锁：防止并发重复生成 title
					acquired, err := s.conversationRepo.AcquireGenTitleLock(titleCtx, req.ConversationID)
					if err != nil {
						log.Printf("acquire gen_title lock error: %v", err)
						return
					}
					if !acquired {
						return
					}

					titleMessages := []infra.ChatMessage{
						{Role: "user", Content: userMsg.Content},
						{Role: "assistant", Content: fullContent.String()},
					}
					title, err := s.ai.GenerateTitle(titleCtx, titleMessages)
					if err != nil {
						log.Printf("generate title error: %v", err)
						return
					}
					if err := s.conversationRepo.UpdateTitle(
						context.Background(), req.ConversationID, title,
					); err != nil {
						log.Printf("update title error: %v", err)
						return
					}
					eventCh <- dto.SSEEvent{
						Type: "title",
						Data: dto.TitleData{Title: title},
					}
				}()
			}
			return
		case "error":
			firstTokenTimer.Stop()
			msgIDStr := writeErrAssistant(fullContent.String(), "error")
			eventCh <- dto.SSEEvent{
				Type: "error",
				Data: dto.ErrorData{
					Message:   evt.Content,
					MessageID: msgIDStr,
				},
			}
			return
		}
	}

	// AI channel 关闭但没收到 complete
	if timedOut.Load() && !firstTokenReceived {
		msgIDStr := writeErrAssistant("", "error")
		eventCh <- dto.SSEEvent{
			Type: "error",
			Data: dto.ErrorData{
				Message:   "AI service response timed out, please try again later",
				MessageID: msgIDStr,
			},
		}
		return
	}
	// 客户端 abort 或连接断开
	writeErrAssistant(fullContent.String(), "aborted")
}

// RetryMessage 对已有的 user message 重新生成 assistant 回复（创建新分支）。
// 不发送 user_message 事件，事件流为：token × N → complete → [DONE]
func (s *ConversationService) RetryMessage(
	ctx context.Context,
	userID int64,
	req dto.RetryMessageRequest,
	eventCh chan<- dto.SSEEvent,
) {
	defer close(eventCh)

	// 1. 权限校验
	conv, err := s.conversationRepo.GetConversationByID(ctx, req.ConversationID)
	if err != nil {
		eventCh <- dto.SSEEvent{Type: "error", Data: dto.ErrorData{Message: "Conversation not found"}}
		return
	}
	if owned, err := s.canvasRepo.CheckCanvasOwnership(ctx, conv.CanvasID, userID); err != nil || !owned {
		eventCh <- dto.SSEEvent{Type: "error", Data: dto.ErrorData{Message: "Unauthorized"}}
		return
	}

	// 2. 查找 user message，验证它属于该 conversation
	userMsg, err := s.conversationRepo.GetMessageByID(ctx, req.UserMsgID)
	if err != nil {
		eventCh <- dto.SSEEvent{Type: "error", Data: dto.ErrorData{Message: "User message not found"}}
		return
	}
	if userMsg.ConversationID != req.ConversationID || userMsg.Role != "user" {
		eventCh <- dto.SSEEvent{Type: "error", Data: dto.ErrorData{Message: "Invalid user message"}}
		return
	}

	// 3. 预生成 assistant message ID + defer 兜底机制
	assistantMsgID := idgen.GenID()
	var assistantWritten bool
	var deferErrMsg string

	// 兜底闭包：写入 error/aborted assistant + 更新 leaf
	writeErrAssistant := func(content string, status string) string {
		if assistantWritten {
			return ""
		}
		errMsg := model.Message{
			ConversationID: req.ConversationID,
			ParentID:       &req.UserMsgID,
			Role:           "assistant",
			Content:        content,
			Model:          &req.Model,
			Status:         status,
		}
		errMsg.ID = assistantMsgID
		_ = s.conversationRepo.CreateMessage(context.Background(), &errMsg)
		_ = s.conversationRepo.UpdateCurrentLeafID(context.Background(), req.ConversationID, errMsg.ID)
		assistantWritten = true
		return strconv.FormatInt(assistantMsgID, 10)
	}

	// 安全网：函数退出时，如果 assistant 未被写入，自动补写 + 统一发送 SSE error
	defer func() {
		msgIDStr := writeErrAssistant("", "error")
		if msgIDStr == "" {
			return // assistantWritten 已为 true，无需发送
		}
		if deferErrMsg == "" {
			deferErrMsg = "Internal error"
		}
		eventCh <- dto.SSEEvent{
			Type: "error",
			Data: dto.ErrorData{
				Message:   deferErrMsg,
				MessageID: msgIDStr,
			},
		}
	}()

	// 4. 发送 retry_ack 事件（携带 assistantMsgID，让前端可立即感知）
	eventCh <- dto.SSEEvent{
		Type: "retry_ack",
		Data: dto.RetryAckEvent{AssistantMsgID: assistantMsgID},
	}

	// 5. 并发组装上下文（assemblyTimer 30s）
	chatMessages, rawMessages, fullRoundCount, assemblyErr := s.assembleContext(ctx, req.ConversationID, userMsg.ID, req.ParentDelta, eventCh)
	if assemblyErr != nil {
		deferErrMsg = assemblyErr.Error()
		return
	}

	// 6. 后置触发 + 硬截断保护
	parentContextTokens := s.countParentContextTokens(chatMessages, len(rawMessages))
	s.maybeTriggerSummary(ctx, req.ConversationID, userMsg.ID, chatMessages, rawMessages, parentContextTokens, fullRoundCount)
	chatMessages, err = s.hardTruncate(chatMessages, len(rawMessages))
	if err != nil {
		deferErrMsg = err.Error()
		return
	}

	// 7. 发送 thinking 事件，前端从 summarizing 切换到 thinking
	eventCh <- dto.SSEEvent{Type: "thinking"}

	// 8. 调用 AI 获取 stream（支持首 token 超时取消）
	aiCtx, aiCancel := context.WithCancel(ctx)
	defer aiCancel()

	aiCh, err := s.ai.StreamChat(aiCtx, chatMessages, req.Model)
	if err != nil {
		deferErrMsg = "Failed to call AI service"
		return
	}

	// 9. 首 token 超时保护：30s 内未收到任何事件则取消 AI 请求
	var firstTokenReceived bool
	var timedOut atomic.Bool
	firstTokenTimer := time.AfterFunc(30*time.Second, func() {
		timedOut.Store(true)
		aiCancel()
	})
	defer firstTokenTimer.Stop()

	// 10. 消费 AI 事件，转发给 handler
	var fullContent strings.Builder
	for evt := range aiCh {
		switch evt.Type {
		case "token":
			if !firstTokenReceived {
				firstTokenReceived = true
				firstTokenTimer.Stop()
			}
			fullContent.WriteString(evt.Content)
			eventCh <- dto.SSEEvent{
				Type: "token",
				Data: dto.TokenData{
					Content:   evt.Content,
					MessageID: strconv.FormatInt(assistantMsgID, 10),
				},
			}
		case "tool_call":
			if !firstTokenReceived {
				firstTokenReceived = true
				firstTokenTimer.Stop()
			}
			eventCh <- dto.SSEEvent{
				Type: "tool_call",
				Data: dto.ToolCallData{Content: evt.Content},
			}
		case "complete":
			firstTokenTimer.Stop()
			assistantMsg := model.Message{
				ConversationID:   req.ConversationID,
				ParentID:         &req.UserMsgID,
				Role:             "assistant",
				Content:          fullContent.String(),
				Model:            &req.Model,
				Status:           "completed",
				PromptTokens:     evt.PromptTokens,
				CompletionTokens: evt.CompletionTokens,
			}
			assistantMsg.ID = assistantMsgID
			if err := s.conversationRepo.CreateMessage(ctx, &assistantMsg); err != nil {
				deferErrMsg = "Failed to save assistant message"
				return
			}
			assistantWritten = true
			_ = s.conversationRepo.UpdateCurrentLeafID(ctx, req.ConversationID, assistantMsg.ID)

			eventCh <- dto.SSEEvent{
				Type: "complete",
				Data: modelToFullMessage(assistantMsg),
			}
			return
		case "error":
			firstTokenTimer.Stop()
			msgIDStr := writeErrAssistant(fullContent.String(), "error")
			eventCh <- dto.SSEEvent{
				Type: "error",
				Data: dto.ErrorData{
					Message:   evt.Content,
					MessageID: msgIDStr,
				},
			}
			return
		}
	}

	// AI channel 关闭但没收到 complete
	if timedOut.Load() && !firstTokenReceived {
		msgIDStr := writeErrAssistant("", "error")
		eventCh <- dto.SSEEvent{
			Type: "error",
			Data: dto.ErrorData{
				Message:   "AI service response timed out, please try again later",
				MessageID: msgIDStr,
			},
		}
		return
	}
	// 客户端 abort 或连接断开
	writeErrAssistant(fullContent.String(), "aborted")
}

func (s *ConversationService) UpdateCurrentLeaf(
	ctx context.Context,
	userID int64,
	conversationID string,
	leafID int64,
) error {
	conv, err := s.conversationRepo.GetConversationByID(ctx, conversationID)
	if err != nil {
		return apperr.NotFound("Conversation not found")
	}

	if owned, err := s.canvasRepo.CheckCanvasOwnership(ctx, conv.CanvasID, userID); err != nil {
		return err
	} else if !owned {
		return apperr.Unauthorized("User does not have access to this canvas")
	}

	return s.conversationRepo.UpdateCurrentLeafID(ctx, conversationID, leafID)
}

// ========== 上下文组装（Phase 3: 并发 + assemblyTimer） ==========

// assembleContext 并发执行 Step 2（父节点上下文）和 Step 3（message chain），
// 整个组装阶段受 assemblyTimer（30s）限制。
// 返回注入了 fake turns 的完整 chatMessages、rawMessages 和完整轮次数。
func (s *ConversationService) assembleContext(
	ctx context.Context,
	conversationID string,
	leafID int64,
	delta dto.ParentDelta,
	eventCh chan<- dto.SSEEvent,
) (chatMessages []infra.ChatMessage, rawMessages []infra.ChatMessage, fullRoundCount int, err error) {
	// assemblyTimer：上下文组装阶段 30s 超时
	assemblyCtx, assemblyCancel := context.WithTimeout(ctx, 30*time.Second)
	defer assemblyCancel()

	// Step 2（父节点上下文）和 Step 3（message chain）并发执行
	var wg sync.WaitGroup
	var parentBlocks []infra.ContentBlock
	var parentErr error

	wg.Add(2)
	go func() {
		defer wg.Done()
		parentBlocks, parentErr = s.resolveParentContext(assemblyCtx, conversationID, delta, eventCh)
	}()
	go func() {
		defer wg.Done()
		chatMessages, rawMessages, fullRoundCount = s.buildMessageChain(assemblyCtx, conversationID, leafID, eventCh)
	}()
	wg.Wait()

	// 检查组装超时
	if assemblyCtx.Err() != nil {
		return nil, nil, 0, fmt.Errorf("Context assembly timed out, please try again later")
	}
	if parentErr != nil {
		return nil, nil, 0, parentErr
	}

	// 注入父节点上下文为 fake first turn
	if len(parentBlocks) > 0 {
		parentBlocks = append(parentBlocks, infra.ContentBlock{
			Type: "text",
			Text: "The above is the prerequisite knowledge for this chat",
		})
		fakeFirstTurn := []infra.ChatMessage{
			{Role: "user", Content: parentBlocks},
			{Role: "assistant", Content: "OK, I have understood the above prerequisite knowledge."},
		}
		chatMessages = append(fakeFirstTurn, chatMessages...)
	}

	return chatMessages, rawMessages, fullRoundCount, nil
}

// resolveParentContext 根据 DB 中的 node_edges 和前端传来的 ParentDelta，
// 解析出所有父节点的上下文内容，返回 ContentBlock 列表。
func (s *ConversationService) resolveParentContext(
	ctx context.Context,
	conversationID string,
	delta dto.ParentDelta,
	eventCh chan<- dto.SSEEvent,
) ([]infra.ContentBlock, error) {
	// 1. 从 node_edges 查询当前 ChatNode 的所有父节点（conversationID == nodeID）
	dbNodes, err := s.canvasRepo.GetParentNodesByTargetID(ctx, conversationID)
	if err != nil {
		return nil, fmt.Errorf("failed to get parent nodes: %w", err)
	}

	// 2. 构建父节点集合（map by ID）
	parentMap := make(map[string]model.Node, len(dbNodes)+len(delta.NewParentNodes))
	for _, n := range dbNodes {
		parentMap[n.ID] = n
	}

	// 3. 合并前端传来的新父节点（优先 DB 数据，DB 查不到则使用前端数据）
	for _, newNode := range delta.NewParentNodes {
		if _, exists := parentMap[newNode.ID]; !exists {
			parentMap[newNode.ID] = model.Node{
				ID:        newNode.ID,
				NodeType:  newNode.Type,
				FileID:    newNode.FileID,
				CreatedAt: time.Now(), // 新关联节点视为最新，截断时最后裁
			}
		}
	}

	// 4. 排除已删除的父节点
	for _, deletedID := range delta.DeletedParentNodeIDs {
		delete(parentMap, deletedID)
	}

	// 5. 将 parentMap 转为 slice 并按 CreatedAt 升序排序（最早创建的排前面，截断时优先移除）
	sortedNodes := make([]model.Node, 0, len(parentMap))
	for _, node := range parentMap {
		sortedNodes = append(sortedNodes, node)
	}
	sort.Slice(sortedNodes, func(i, j int) bool {
		return sortedNodes[i].CreatedAt.Before(sortedNodes[j].CreatedAt)
	})

	if len(sortedNodes) == 0 {
		return nil, nil
	}

	// 6. 并发处理所有父节点，按类型获取上下文
	type parentResult struct {
		blocks []infra.ContentBlock
		err    error
		fatal  bool // resourceNode 错误为 fatal，chatNode 错误仅 log
	}
	results := make([]parentResult, len(sortedNodes))

	var wg sync.WaitGroup
	for i, node := range sortedNodes {
		wg.Add(1)
		go func(idx int, n model.Node) {
			defer wg.Done()
			switch n.NodeType {
			case "chatNode":
				summary, err := s.getChatNodeSummary(ctx, n.ID, eventCh)
				if err != nil {
					log.Printf("[resolveParentContext] chatNode %s summary error: %v", n.ID, err)
					return
				}
				if summary == "" {
					return
				}
				results[idx].blocks = []infra.ContentBlock{{
					Type: "text",
					Text: fmt.Sprintf("[Related conversation node summary]\n%s", summary),
				}}

			case "resourceNode":
				if n.FileID == nil || *n.FileID == -1 {
					return
				}
				fileBlocks, err := s.getResourceNodeContent(ctx, *n.FileID, eventCh)
				if err != nil {
					results[idx].err = err
					results[idx].fatal = true
					return
				}
				results[idx].blocks = fileBlocks
			}
		}(i, node)
	}
	wg.Wait()

	// 按原顺序合并结果
	var blocks []infra.ContentBlock
	for _, r := range results {
		if r.fatal {
			return nil, r.err
		}
		blocks = append(blocks, r.blocks...)
	}

	return blocks, nil
}

// getResourceNodeContent 根据文件的 ContentType 获取文件内容，返回 ContentBlock 列表。
func (s *ConversationService) getResourceNodeContent(ctx context.Context, fileID int64, eventCh chan<- dto.SSEEvent) ([]infra.ContentBlock, error) {
	file, err := s.fileContentRepo.GetFileByID(ctx, fileID)
	if err != nil {
		log.Printf("[getResourceNodeContent] file %d not found: %v", fileID, err)
		return nil, nil // 文件不存在则跳过，不中断请求
	}

	ct := strings.ToLower(file.ContentType)

	switch {
	case isOriginalText(ct):
		return s.getOriginalTextContent(ctx, file)
	case isOriginalImage(ct):
		return s.getOriginalImageContent(ctx, file)
	default:
		// 转换类型（PDF、DOCX、XLSX、PPTX）
		return s.getConvertedContent(ctx, file, eventCh)
	}
}

// getOriginalTextContent 获取原始文本文件内容（text/*、application/json）。
// 先查 Redis 缓存，miss 时从 MinIO 读取并回填缓存。
func (s *ConversationService) getOriginalTextContent(ctx context.Context, file *model.File) ([]infra.ContentBlock, error) {
	// 先查 Redis 缓存
	cached, err := s.fileContentRepo.GetFileTextCache(ctx, file.ID)
	if err == nil && cached != "" {
		return []infra.ContentBlock{{
			Type: "text",
			Text: formatFileTextBlock(file.Filename, cached),
		}}, nil
	}

	// Cache miss：从 MinIO 读原文件
	data, err := s.fileContentRepo.GetMinioObjectBytes(ctx, file.MinioPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read text file from MinIO: %w", err)
	}

	text := string(data)

	// 回填缓存（忽略错误）
	_ = s.fileContentRepo.SetFileTextCache(ctx, file.ID, text)

	return []infra.ContentBlock{{
		Type: "text",
		Text: formatFileTextBlock(file.Filename, text),
	}}, nil
}

// getOriginalImageContent 获取原始图片文件内容（image/*）。
// 直接从 MinIO 读取并转 base64。
func (s *ConversationService) getOriginalImageContent(ctx context.Context, file *model.File) ([]infra.ContentBlock, error) {
	data, err := s.fileContentRepo.GetMinioObjectBytes(ctx, file.MinioPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read image from MinIO: %w", err)
	}

	b64 := base64.StdEncoding.EncodeToString(data)
	mediaType := file.ContentType
	if mediaType == "" {
		mediaType = "image/jpeg"
	}

	return []infra.ContentBlock{{
		Type:     "image_url",
		ImageURL: &infra.ImageURL{URL: fmt.Sprintf("data:%s;base64,%s", mediaType, b64)},
	}}, nil
}

// getConvertedContent 获取需要预处理的文件内容（PDF、DOCX、XLSX、PPTX）。
// 先检查处理状态，然后按优先级取用：summary → text → pages。
func (s *ConversationService) getConvertedContent(ctx context.Context, file *model.File, eventCh chan<- dto.SSEEvent) ([]infra.ContentBlock, error) {
	// 1. 检查是否正在处理中
	processing, err := s.fileContentRepo.IsFileProcessing(ctx, file.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to check file processing status: %w", err)
	}
	if processing {
		eventCh <- dto.SSEEvent{Type: "summarizing", Data: dto.SummarizingData{Reason: "File being processed..."}}
		if err := s.waitForProcessing(ctx, file.ID); err != nil {
			return nil, err
		}
	}

	// 2. 按优先级取用：summary → text → pages

	// 2a. 尝试 summary（token 最省）
	blocks, err := s.tryGetSummaryContent(ctx, file)
	if err == nil && blocks != nil {
		return blocks, nil
	}

	// 2b. 尝试 text（summary 尚未生成或生成失败时退化）
	blocks, err = s.tryGetTextContent(ctx, file)
	if err == nil && blocks != nil {
		return blocks, nil
	}

	// 2c. 尝试 pages（仅 PPTX 和扫描 PDF，无文本产物时）
	blocks, err = s.tryGetPagesContent(ctx, file)
	if err == nil && blocks != nil {
		return blocks, nil
	}

	// 3. 全无 → 返回错误
	return nil, apperr.BadRequest("File parsing error, please re-upload the file")
}

// tryGetSummaryContent 尝试获取文件摘要（Redis 缓存 → MinIO _summary.txt）
func (s *ConversationService) tryGetSummaryContent(ctx context.Context, file *model.File) ([]infra.ContentBlock, error) {
	// 先查 Redis 缓存
	cached, err := s.fileContentRepo.GetFileSummaryCache(ctx, file.ID)
	if err == nil && cached != "" {
		return []infra.ContentBlock{{
			Type: "text",
			Text: formatFileTextBlock(file.Filename, cached),
		}}, nil
	}

	// Cache miss：尝试从 MinIO 读取
	summaryPath := file.MinioPath + "_summary.txt"
	exists, err := s.fileContentRepo.MinioObjectExists(ctx, summaryPath)
	if err != nil || !exists {
		return nil, fmt.Errorf("summary not available")
	}

	data, err := s.fileContentRepo.GetMinioObjectBytes(ctx, summaryPath)
	if err != nil {
		return nil, err
	}

	text := string(data)
	_ = s.fileContentRepo.SetFileSummaryCache(ctx, file.ID, text)

	return []infra.ContentBlock{{
		Type: "text",
		Text: formatFileTextBlock(file.Filename, text),
	}}, nil
}

// tryGetTextContent 尝试获取提取的文本（Redis 缓存 → MinIO _text.txt）
func (s *ConversationService) tryGetTextContent(ctx context.Context, file *model.File) ([]infra.ContentBlock, error) {
	// 先查 Redis 缓存
	cached, err := s.fileContentRepo.GetFileTextCache(ctx, file.ID)
	if err == nil && cached != "" {
		return []infra.ContentBlock{{
			Type: "text",
			Text: formatFileTextBlock(file.Filename, cached),
		}}, nil
	}

	// Cache miss：尝试从 MinIO 读取
	textPath := file.MinioPath + "_text.txt"
	exists, err := s.fileContentRepo.MinioObjectExists(ctx, textPath)
	if err != nil || !exists {
		return nil, fmt.Errorf("text not available")
	}

	data, err := s.fileContentRepo.GetMinioObjectBytes(ctx, textPath)
	if err != nil {
		return nil, err
	}

	text := string(data)
	_ = s.fileContentRepo.SetFileTextCache(ctx, file.ID, text)

	return []infra.ContentBlock{{
		Type: "text",
		Text: formatFileTextBlock(file.Filename, text),
	}}, nil
}

// tryGetPagesContent 尝试获取转换出的图片页面（MinIO _pages/）
func (s *ConversationService) tryGetPagesContent(ctx context.Context, file *model.File) ([]infra.ContentBlock, error) {
	pagesPrefix := file.MinioPath + "_pages/"
	pageObjects, err := s.fileContentRepo.ListMinioObjects(ctx, pagesPrefix)
	if err != nil || len(pageObjects) == 0 {
		return nil, fmt.Errorf("pages not available")
	}

	var blocks []infra.ContentBlock
	for _, pagePath := range pageObjects {
		data, err := s.fileContentRepo.GetMinioObjectBytes(ctx, pagePath)
		if err != nil {
			log.Printf("[tryGetPagesContent] failed to read page %s: %v", pagePath, err)
			continue
		}
		b64 := base64.StdEncoding.EncodeToString(data)
		blocks = append(blocks, infra.ContentBlock{
			Type:     "image_url",
			ImageURL: &infra.ImageURL{URL: fmt.Sprintf("data:image/jpeg;base64,%s", b64)},
		})
	}

	if len(blocks) == 0 {
		return nil, fmt.Errorf("no readable pages")
	}
	return blocks, nil
}

// waitForProcessing 轮询等待文件处理完成（间隔 500ms，最大等待 5s）。
func (s *ConversationService) waitForProcessing(ctx context.Context, fileID int64) error {
	const maxWait = 5 * time.Second
	const interval = 500 * time.Millisecond

	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		processing, err := s.fileContentRepo.IsFileProcessing(ctx, fileID)
		if err != nil {
			return fmt.Errorf("failed to check processing status: %w", err)
		}
		if !processing {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}

	return apperr.BadRequest("File processing timed out, please try again later")
}

// ========== 辅助函数 ==========

// buildMessageChain 从 leafID 沿 parent_id 回溯，支持 message-level summary 截断。
//
// 遍历时检查每个 message 的 summary 字段：
//   - 遇到非空 summary 且到当前消息 ≥ 3 轮 raw → 使用 summary + raw messages，停止遍历
//   - < 3 轮 raw → 跳过该 summary 继续向上
//   - 有可用 summary 时，构造 fake second turn 注入
//
// 返回值：
//   - chatMessages: 有序的聊天消息列表（可能包含 fake second turn）
//   - rawMessages: 不含 fake turn 的原始消息列表（用于后置 token 计数和 summary 触发）
//   - fullRoundCount: parent 链上完整的 (user,assistant) 轮次数
func (s *ConversationService) buildMessageChain(ctx context.Context, conversationID string, leafID int64, eventCh chan<- dto.SSEEvent) (chatMessages []infra.ChatMessage, rawMessages []infra.ChatMessage, fullRoundCount int) {
	// 1. 锁前置检查：有锁就等
	locked, err := s.conversationRepo.IsMsgSummaryLocked(ctx, conversationID)
	if err != nil {
		log.Printf("[buildMessageChain] check lock error: %v", err)
	}
	if locked {
		eventCh <- dto.SSEEvent{Type: "summarizing", Data: dto.SummarizingData{Reason: "messages are being summarized..."}}
		if err := s.waitForSummaryReady(ctx, conversationID); err != nil {
			log.Printf("[buildMessageChain] wait timeout for conversation %s, fallback to recent messages", conversationID)
			return s.buildFallbackChain(ctx, leafID)
		}
	}

	// 2. 沿 parent 链回溯，收集消息并检查 summary
	type rawMsg struct {
		msg     model.Message
		chatMsg infra.ChatMessage
	}
	var collected []rawMsg
	var usableSummary string

	msgID := &leafID
	for msgID != nil {
		msg, err := s.conversationRepo.GetMessageByID(ctx, *msgID)
		if err != nil {
			break
		}
		if msg.Role == "root" {
			break
		}

		collected = append([]rawMsg{{
			msg:     msg,
			chatMsg: infra.ChatMessage{Role: msg.Role, Content: msg.Content},
		}}, collected...)

		// 检查 summary
		if msg.Summary != nil && *msg.Summary != "" {
			// 计算该 summary 到 leaf 之间的完整轮数（不含 summary 所在消息自身）
			// collected[0] 是 summary 所在消息，collected[1:] 是它之后的消息
			afterSummary := make([]infra.ChatMessage, 0, len(collected)-1)
			for _, c := range collected[1:] {
				afterSummary = append(afterSummary, c.chatMsg)
			}
			rounds := countChatMsgRounds(afterSummary)
			if rounds >= 3 {
				usableSummary = *msg.Summary
				collected = collected[1:]
				break
			}
			// < 3 轮，跳过该 summary 继续向上
		}

		msgID = msg.ParentID
	}

	// 3. 构建 rawMessages（保持不变，用于步数对齐）
	rawMessages = make([]infra.ChatMessage, len(collected))
	for i, c := range collected {
		rawMessages[i] = c.chatMsg
	}

	// 3.5 过滤/修饰 error/aborted assistant（仅影响 chatMessages，不影响 rawMessages）
	var filtered []rawMsg
	for i := 0; i < len(collected); i++ {
		msg := collected[i]
		if msg.msg.Role == "assistant" &&
			(msg.msg.Status == "error" || msg.msg.Status == "aborted") {

			if msg.msg.Content == "" {
				// 空内容 → 跳过 assistant，同时移除前面配对的 user
				if len(filtered) > 0 && filtered[len(filtered)-1].msg.Role == "user" {
					filtered = filtered[:len(filtered)-1]
				}
				continue
			}

			// 有内容但未完成 → 追加中断说明，让 AI 知道上次回复被截断
			var suffix string
			if msg.msg.Status == "aborted" {
				suffix = "\n\n[System: Generation interrupted by user]"
			} else {
				suffix = "\n\n[System: Generation interrupted by error]"
			}
			msg.msg.Content += suffix
			msg.chatMsg.Content = msg.msg.Content
			filtered = append(filtered, msg)
			continue
		}
		filtered = append(filtered, msg)
	}

	// 从过滤后的消息构建 chatMessages 用的列表
	filteredChatMsgs := make([]infra.ChatMessage, len(filtered))
	for i, c := range filtered {
		filteredChatMsgs[i] = c.chatMsg
	}

	// 4. 计算完整轮次数
	fullRoundCount = countChatMsgRounds(rawMessages)

	// 5. 如果有可用 summary，构造 fake second turn
	if usableSummary != "" {
		fakeSecondTurn := []infra.ChatMessage{
			{Role: "user", Content: "Previous conversation summary: " + usableSummary},
			{Role: "assistant", Content: "OK, I have understood the previous conversation."},
		}
		chatMessages = append(fakeSecondTurn, filteredChatMsgs...)
	} else {
		chatMessages = filteredChatMsgs
	}

	return chatMessages, rawMessages, fullRoundCount
}

// waitForSummaryReady 轮询 Redis 锁是否消失（500ms 间隔，最多 15s）。
func (s *ConversationService) waitForSummaryReady(ctx context.Context, conversationID string) error {
	const maxWait = 15 * time.Second
	const interval = 500 * time.Millisecond

	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		locked, err := s.conversationRepo.IsMsgSummaryLocked(ctx, conversationID)
		if err != nil {
			return err
		}
		if !locked {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
	return fmt.Errorf("wait for summary timeout")
}

// buildFallbackChain 超时 fallback：只保留最近 3 轮 raw messages。
func (s *ConversationService) buildFallbackChain(ctx context.Context, leafID int64) ([]infra.ChatMessage, []infra.ChatMessage, int) {
	type collected struct {
		msg     model.Message
		chatMsg infra.ChatMessage
	}
	var chain []collected
	msgID := &leafID
	for msgID != nil {
		msg, err := s.conversationRepo.GetMessageByID(ctx, *msgID)
		if err != nil {
			break
		}
		if msg.Role == "root" {
			break
		}
		chain = append([]collected{{
			msg:     msg,
			chatMsg: infra.ChatMessage{Role: msg.Role, Content: msg.Content},
		}}, chain...)
		msgID = msg.ParentID
	}

	// 过滤/修饰 error/aborted assistant（与 buildMessageChain 步骤 3.5 对齐）
	var filtered []infra.ChatMessage
	for i := 0; i < len(chain); i++ {
		m := chain[i]
		if m.msg.Role == "assistant" &&
			(m.msg.Status == "error" || m.msg.Status == "aborted") {

			if m.msg.Content == "" {
				// 空内容 → 跳过 assistant，同时移除前面配对的 user
				if len(filtered) > 0 && filtered[len(filtered)-1].Role == "user" {
					filtered = filtered[:len(filtered)-1]
				}
				continue
			}

			// 有内容但未完成 → 追加中断说明
			var suffix string
			if m.msg.Status == "aborted" {
				suffix = "\n\n[System: Generation interrupted by user]"
			} else {
				suffix = "\n\n[System: Generation interrupted by error]"
			}
			m.chatMsg.Content = m.msg.Content + suffix
			filtered = append(filtered, m.chatMsg)
			continue
		}
		filtered = append(filtered, m.chatMsg)
	}

	kept := keepRecentRounds(filtered, 3)
	return kept, kept, countChatMsgRounds(kept)
}

// countChatMsgRounds 计算 ChatMessage 列表中完整的 (user, assistant) 轮次数。
func countChatMsgRounds(msgs []infra.ChatMessage) int {
	rounds := 0
	for i := 0; i+1 < len(msgs); i += 2 {
		if msgs[i].Role == "user" && msgs[i+1].Role == "assistant" {
			// 跳过空 content assistant（error/aborted 场景），不计入有效轮次
			if content, ok := msgs[i+1].Content.(string); ok && content == "" {
				continue
			}
			rounds++
		}
	}
	return rounds
}

// keepRecentRounds 保留最近 n 轮完整的 (user, assistant) 对，从末尾往前取。
func keepRecentRounds(msgs []infra.ChatMessage, n int) []infra.ChatMessage {
	// 从末尾往前数 n 轮（每轮 2 条消息）
	keep := n * 2
	if keep >= len(msgs) {
		return msgs
	}
	return msgs[len(msgs)-keep:]
}

// ========== Node-level Summary ==========

// getChatNodeSummary 获取 ChatNode 的 node-level summary。
// 先查 Redis 缓存，miss 时同步生成。
func (s *ConversationService) getChatNodeSummary(ctx context.Context, nodeID string, eventCh chan<- dto.SSEEvent) (string, error) {
	// nodeID == conversationID
	conv, err := s.conversationRepo.GetConversationByID(ctx, nodeID)
	if err != nil {
		return "", nil // conversation 不存在则跳过
	}

	// 如果 leaf == root，说明没有实际对话，跳过
	if conv.CurrentLeafID == conv.RootMessageID {
		return "", nil
	}

	// 1. 尝试 Redis 缓存
	cached, err := s.conversationRepo.GetNodeSummaryCache(ctx, nodeID, conv.CurrentLeafID)
	if err == nil && cached != "" {
		if cached == nodeSummaryFailSentinel {
			return "", nil
		}
		return cached, nil
	}

	// 2. Cache miss → 通知前端正在整理关联节点上下文
	eventCh <- dto.SSEEvent{Type: "summarizing", Data: dto.SummarizingData{Reason: "Organizing the context of associated nodes..."}}

	// 3. 加锁生成
	acquired, err := s.conversationRepo.AcquireNodeSummaryLock(ctx, nodeID, conv.CurrentLeafID)
	if err != nil {
		return "", fmt.Errorf("acquire node summary lock: %w", err)
	}

	if acquired {
		// double-check: 另一个请求可能已在我们等待锁之前生成完毕
		cached, err = s.conversationRepo.GetNodeSummaryCache(ctx, nodeID, conv.CurrentLeafID)
		if err == nil && cached != "" {
			_ = s.conversationRepo.ReleaseNodeSummaryLock(ctx, nodeID, conv.CurrentLeafID)
			if cached == nodeSummaryFailSentinel {
				return "", nil
			}
			return cached, nil
		}

		// 加锁成功 → 同步生成
		summary, genErr := s.generateNodeSummary(ctx, nodeID, conv.CurrentLeafID)
		if genErr != nil {
			_ = s.conversationRepo.SetNodeSummaryCache(ctx, nodeID, conv.CurrentLeafID, nodeSummaryFailSentinel)
			_ = s.conversationRepo.ReleaseNodeSummaryLock(ctx, nodeID, conv.CurrentLeafID)
			return "", genErr
		}
		// 先写入缓存，再释放锁，确保等待方能立即读到缓存
		_ = s.conversationRepo.SetNodeSummaryCache(ctx, nodeID, conv.CurrentLeafID, summary)
		_ = s.conversationRepo.ReleaseNodeSummaryLock(ctx, nodeID, conv.CurrentLeafID)
		return summary, nil
	}

	// 4. 加锁失败 → 轮询等待缓存出现，超时由 assemblyTimer 统一控制
	return s.waitForNodeSummaryCache(ctx, nodeID, conv.CurrentLeafID)
}

// waitForNodeSummaryCache 轮询等待 node summary 缓存出现。
// 超时由外层 assemblyTimer（context.WithTimeout）统一控制。
func (s *ConversationService) waitForNodeSummaryCache(ctx context.Context, conversationID string, leafID int64) (string, error) {
	const interval = 500 * time.Millisecond

	for {
		cached, err := s.conversationRepo.GetNodeSummaryCache(ctx, conversationID, leafID)
		if err == nil && cached != "" {
			if cached == nodeSummaryFailSentinel {
				return "", nil
			}
			return cached, nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(interval):
		}
	}
}

// generateNodeSummary 生成 node-level summary。
// 输入：父节点 conversation 内最深的 message-level summary + 其后的 raw messages。
func (s *ConversationService) generateNodeSummary(ctx context.Context, conversationID string, leafID int64) (string, error) {
	// 沿 parent 链回溯，找到最深的 message-level summary + 尾部 raw messages
	var rawAfterSummary []infra.ChatMessage
	var deepestSummary *string

	msgID := &leafID
	// 先收集所有消息
	var allMsgs []model.Message
	for msgID != nil {
		msg, err := s.conversationRepo.GetMessageByID(ctx, *msgID)
		if err != nil {
			break
		}
		if msg.Role == "root" {
			break
		}
		allMsgs = append([]model.Message{msg}, allMsgs...)
		msgID = msg.ParentID
	}

	// 找最深的 summary（从后往前找第一个有 summary 的）
	summaryIdx := -1
	for i := len(allMsgs) - 1; i >= 0; i-- {
		if allMsgs[i].Summary != nil && *allMsgs[i].Summary != "" {
			summaryIdx = i
			deepestSummary = allMsgs[i].Summary
			break
		}
	}

	// 构建输入消息：summary 之后的 raw messages
	startIdx := 0
	if summaryIdx >= 0 {
		startIdx = summaryIdx + 1
	}
	for i := startIdx; i < len(allMsgs); i++ {
		rawAfterSummary = append(rawAfterSummary, infra.ChatMessage{
			Role:    allMsgs[i].Role,
			Content: allMsgs[i].Content,
		})
	}

	// 如果没有任何消息可供总结，跳过
	if len(rawAfterSummary) == 0 && deepestSummary == nil {
		return "", nil
	}

	callCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	summary, err := s.ai.GenerateSummary(callCtx, rawAfterSummary, deepestSummary, "node")
	if err != nil {
		return "", fmt.Errorf("generate node summary: %w", err)
	}
	return summary, nil
}

// ========== 后置触发 + 硬截断 ==========

const (
	minContextWindow    = 128000 // 128K tokens
	summaryOffsetRounds = 2     // summary 挂载回退轮数
)

// countParentContextTokens 计算 chatMessages 中父节点上下文（fake first turn）的 token 数。
// rawCount 是 rawMessages 的长度，用于定位 parent context 在 chatMessages 中的位置。
func (s *ConversationService) countParentContextTokens(chatMessages []infra.ChatMessage, rawCount int) int {
	// chatMessages 结构：[fake first turn (2)] + [fake second turn (2, optional)] + [raw messages (rawCount)]
	// parent context = chatMessages 前面的部分，减去 raw messages 和可能的 fake second turn
	parentEnd := len(chatMessages) - rawCount
	if parentEnd <= 0 {
		return 0
	}

	// 只计算 fake first turn（前 2 条，如果存在）
	// fake second turn 属于 message-level summary，不算 parent context
	tokens := 0
	for i := 0; i < parentEnd; i++ {
		msg := chatMessages[i]
		// fake first turn 的 user 消息是 []ContentBlock（ok=false），
		// fake second turn 的 user 消息是 string（ok=true）。
		// 遇到 string 类型的 user 消息即表示到了 fake second turn，停止计算。
		if _, ok := msg.Content.(string); ok && msg.Role == "user" {
			break
		}
		tokens += perMessageOverhead + estimateChatMessage(msg)
	}
	return tokens
}

// maybeTriggerSummary 后置触发判断：token 超过阈值时异步启动 summary 生成。
func (s *ConversationService) maybeTriggerSummary(
	ctx context.Context,
	conversationID string,
	leafID int64,
	chatMessages []infra.ChatMessage,
	rawMessages []infra.ChatMessage,
	parentContextTokens int,
	fullRoundCount int,
) {
	// 轮次不够，无法挂载 summary
	if fullRoundCount < summaryOffsetRounds+1 {
		return
	}

	// 计算 ②+③+④ 的 token 数（总量减去父节点上下文）
	convTokens := estimateChatMessages(chatMessages) - parentContextTokens

	// 动态阈值：(128K - parentContextTokens) * 0.3
	threshold := (minContextWindow - parentContextTokens) * 3 / 10
	if convTokens <= threshold {
		return
	}

	// 尝试加锁
	acquired, err := s.conversationRepo.AcquireMsgSummaryLock(ctx, conversationID)
	if err != nil || !acquired {
		return // 另一个请求已在生成
	}

	// 找到挂载目标：从 rawMessages 末尾往前退 summaryOffsetRounds 轮的 assistant message
	// rawMessages 格式：[u1, a1, u2, a2, ..., uN(当前 user prompt)]
	// targetIdx 是 summaryInput 的切片上界，rawMessages[:targetIdx] 包含到挂载目标 assistant 为止。
	// 不计当前 user prompt（末尾），从倒数第 2 条开始往前退 summaryOffsetRounds 轮，
	// 即 targetIdx = len - (2*summaryOffsetRounds + 1)，挂载目标 assistant = rawMessages[targetIdx-1]。
	targetIdx := len(rawMessages) - (summaryOffsetRounds*2 + 1)

	// 确保 target 不落在空 error/aborted assistant 上
	// （空内容 assistant 在 chatMessages 过滤时会被跳过，summary 挂载其上等于丢失）
	for targetIdx >= 2 {
		if content, ok := rawMessages[targetIdx-1].Content.(string); ok && content == "" {
			targetIdx -= 2 // 跳过这一空轮，继续向前
			continue
		}
		break
	}
	if targetIdx < 2 {
		_ = s.conversationRepo.ReleaseMsgSummaryLock(context.Background(), conversationID)
		return
	}

	go s.asyncGenerateMsgSummary(conversationID, leafID, rawMessages, targetIdx)
}

// asyncGenerateMsgSummary 异步生成 message-level summary 并写入 DB。
func (s *ConversationService) asyncGenerateMsgSummary(
	conversationID string,
	leafID int64,
	rawMessages []infra.ChatMessage,
	targetIdx int,
) {
	bgCtx := context.Background()
	defer s.conversationRepo.ReleaseMsgSummaryLock(bgCtx, conversationID)

	// 从 leaf 回溯，一次遍历同时找到：
	//   1. target message（stepsToTarget 步）—— summary 的挂载目标
	//   2. rawMessages[0] 的 parent（len(rawMessages) 步）—— 查 previousSummary
	stepsToTarget := len(rawMessages) - targetIdx
	totalSteps := len(rawMessages)

	msgID := &leafID
	var targetMsgID int64
	for i := 0; i < totalSteps && msgID != nil; i++ {
		if i == stepsToTarget {
			targetMsgID = *msgID
		}
		msg, err := s.conversationRepo.GetMessageByID(bgCtx, *msgID)
		if err != nil {
			log.Printf("[asyncGenerateMsgSummary] get message failed: %v", err)
			return
		}
		msgID = msg.ParentID
	}
	// 此时 targetMsgID = 挂载目标, msgID = rawMessages[0] 的 parent

	if targetMsgID == 0 {
		log.Printf("[asyncGenerateMsgSummary] target message not found")
		return
	}

	// 查 previousSummary：rawMessages[0] 的 parent 如果不是 root 且有 summary，则使用
	var previousSummary *string
	if msgID != nil {
		parentMsg, err := s.conversationRepo.GetMessageByID(bgCtx, *msgID)
		if err == nil && parentMsg.Role != "root" && parentMsg.Summary != nil && *parentMsg.Summary != "" {
			previousSummary = parentMsg.Summary
		}
	}

	// 构建 summary 输入：rawMessages[:targetIdx] 包含到目标 assistant
	summaryInput := rawMessages[:targetIdx]

	// 过滤空 error/aborted 对：空 assistant + 配对 user 不参与 summary 生成
	var cleanInput []infra.ChatMessage
	for i := 0; i+1 < len(summaryInput); i += 2 {
		if content, ok := summaryInput[i+1].Content.(string); ok && content == "" {
			continue
		}
		cleanInput = append(cleanInput, summaryInput[i], summaryInput[i+1])
	}
	if len(summaryInput)%2 == 1 {
		cleanInput = append(cleanInput, summaryInput[len(summaryInput)-1])
	}
	summaryInput = cleanInput

	callCtx, cancel := context.WithTimeout(bgCtx, 60*time.Second)
	defer cancel()

	summary, err := s.ai.GenerateSummary(callCtx, summaryInput, previousSummary, "message")
	if err != nil {
		log.Printf("[asyncGenerateMsgSummary] generation failed for conversation %s: %v", conversationID, err)
		return
	}

	if err := s.conversationRepo.UpdateMessageSummary(bgCtx, targetMsgID, summary); err != nil {
		log.Printf("[asyncGenerateMsgSummary] db write failed for message %d: %v", targetMsgID, err)
	}
}

// hardTruncate 硬截断保护：合并 prompt 后 token 总量超过 128K*0.80 时按优先级裁剪。
// rawCount 是 raw messages 的数量（不含 fake turns）。
// 如果裁剪后仍然超标，返回 error。
func (s *ConversationService) hardTruncate(chatMessages []infra.ChatMessage, rawCount int) ([]infra.ChatMessage, error) {
	maxAllowed := minContextWindow * 4 / 5 // 128K * 0.80

	totalTokens := estimateChatMessages(chatMessages)
	if totalTokens <= maxAllowed {
		return chatMessages, nil
	}

	// 定位 raw messages 在 chatMessages 中的位置
	rawStart := len(chatMessages) - rawCount
	if rawStart < 0 {
		rawStart = 0
	}

	// a. 丢弃最早的 raw messages，一轮（user+assistant）一起移除，至少保留最近 1 轮
	minKeep := 3 // 至少保留最近 1 轮 = 2 条， 加1条user prompt
	for rawCount > minKeep && estimateChatMessages(chatMessages) > maxAllowed {
		chatMessages = append(chatMessages[:rawStart], chatMessages[rawStart+2:]...)
		rawCount -= 2
	}

	if estimateChatMessages(chatMessages) <= maxAllowed {
		return chatMessages, nil
	}

	// b. 截断 parent context：逐个移除 fake first turn user 消息中的 content blocks
	if rawStart >= 2 {
		if blocks, ok := chatMessages[0].Content.([]infra.ContentBlock); ok {
			for len(blocks) > 0 && estimateChatMessages(chatMessages) > maxAllowed {
				blocks = blocks[1:] // 从头部移除（最早关联的先裁）
				if len(blocks) == 0 {
					// 所有 block 都移除了，删掉整个 fake first turn（user + assistant）
					chatMessages = chatMessages[2:]
					break
				}
				chatMessages[0].Content = blocks
			}
		}
	}

	// c. 仍超标 → 返回 400 错误
	if estimateChatMessages(chatMessages) > maxAllowed {
		return nil, fmt.Errorf("prompt too long: %d tokens exceeds limit %d after truncation", estimateChatMessages(chatMessages), maxAllowed)
	}

	return chatMessages, nil
}

func modelToFullMessage(m model.Message) dto.FullMessage {
	return dto.FullMessage{
		ID:               m.ID,
		ConversationID:   m.ConversationID,
		ParentID:         m.ParentID,
		Role:             m.Role,
		Content:          m.Content,
		Model:            m.Model,
		Status:           m.Status,
		PromptTokens:     m.PromptTokens,
		CompletionTokens: m.CompletionTokens,
		CreatedAt:        m.CreatedAt,
		UpdatedAt:        m.UpdatedAt,
	}
}

// isOriginalText 判断是否为原始文本类型（text/* 或 application/json）
func isOriginalText(contentType string) bool {
	return strings.HasPrefix(contentType, "text/") || contentType == "application/json"
}

// isOriginalImage 判断是否为原始图片类型（image/*）
func isOriginalImage(contentType string) bool {
	return strings.HasPrefix(contentType, "image/")
}

// formatFileTextBlock 格式化文件文本内容块
func formatFileTextBlock(filename, text string) string {
	return fmt.Sprintf("[File: %s]\n---File content start---\n%s\n---File content end---", filename, text)
}

// estimateChatMessage 估算单条 ChatMessage 的 token 数。
func estimateChatMessage(msg infra.ChatMessage) int {
	switch v := msg.Content.(type) {
	case string:
		return tokenutil.EstimateText(v)
	case []infra.ContentBlock:
		var tokens int
		for _, block := range v {
			switch block.Type {
			case "text":
				tokens += tokenutil.EstimateText(block.Text)
			case "image_url":
				tokens += tokenutil.EstimateImages(1)
			}
		}
		return tokens
	default:
		return 0
	}
}

// estimateChatMessages 估算整条消息链的 token 总数（含每条消息的结构开销）。
func estimateChatMessages(messages []infra.ChatMessage) int {
	total := 0
	for _, msg := range messages {
		total += perMessageOverhead
		total += estimateChatMessage(msg)
	}
	return total
}
