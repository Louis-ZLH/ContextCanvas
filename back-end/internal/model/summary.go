package model

import (
	"time"

	"gorm.io/gorm"
)

type Summary struct {
	ID        int64          `gorm:"primaryKey;autoIncrement" json:"id"` // 正常自增即可
	NodeID    int64          `gorm:"uniqueIndex" json:"node_id,string"`  // 一个Node只有一个Summary
	Content   string         `gorm:"type:text" json:"content"`
	Status    string         `gorm:"type:varchar(20);default:'pending'" json:"status"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (s *Summary) TableName() string {
	return "summaries"
}