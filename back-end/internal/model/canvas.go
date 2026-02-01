package model

type Canvas struct {
	BaseModel
	UserID    int64          `gorm:"index" json:"user_id,string"` // 加索引
	Title     string         `gorm:"type:varchar(100)" json:"title"`
}

func (c *Canvas) TableName() string {
	return "canvases"
}