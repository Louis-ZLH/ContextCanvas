package model

import "time"

type TokenUsageLog struct {
	ID               int64     `gorm:"primaryKey;autoIncrement" json:"id"` // 正常自增即可
	UserID           int64     `gorm:"index" json:"user_id,string"`
	MessageID        int64     `gorm:"index" json:"message_id,string"`
	ModelName        string    `json:"model_name"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	SnapshotCost     float64   `gorm:"type:decimal(10,6)" json:"snapshot_cost"` // 记录发生时的实际花费金额
	CreatedAt        time.Time `json:"created_at"`
}

func (t *TokenUsageLog) TableName() string {
	return "token_usage_logs"
}