package model

import (
	"time"
)

type Message struct {
	BaseModel
	NodeID           int64          `gorm:"index" json:"node_id,string"`
	Role             string         `gorm:"type:varchar(20)" json:"role"` // 'user', 'assistant'
	Content          string         `gorm:"type:text" json:"content"`
	PromptTokens     int            `json:"prompt_tokens"`
	CompletionTokens int            `json:"completion_tokens"`
	Model            string         `gorm:"type:varchar(50)" json:"model"`
	Status           string         `gorm:"type:varchar(20)" json:"status"` // 'sending', 'success', 'failed'
	ExpiredAt        *time.Time     `json:"expired_at"` // expired && sending -> failed
}

func (m *Message) TableName() string {
	return "messages"
}