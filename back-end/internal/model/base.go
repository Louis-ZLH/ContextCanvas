package model

import (
	"time"

	"github.com/luhao/contextGraph/pkg/idgen"
	"gorm.io/gorm"
)

type BaseModel struct {
    ID        int64          `gorm:"primaryKey;autoIncrement:false" json:"id,string"` // 雪花算法生成
    CreatedAt time.Time      `json:"created_at"`
    UpdatedAt time.Time      `json:"updated_at"`
    DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (b *BaseModel) BeforeCreate(tx *gorm.DB) (err error) {
	// 如果 ID 还没被赋值（为0），就生成一个新的
	if b.ID == 0 {
		b.ID = idgen.GenID()
	}
	return
}