package model

// User 直接嵌入 BaseModel
type User struct {
	BaseModel
	Email        string         `gorm:"uniqueIndex;type:varchar(255)" json:"email"`      // 建议只由 Email 做唯一
	Username     string         `gorm:"type:varchar(50)" json:"username"`
	AvatarURL    string         `gorm:"type:varchar(255)" json:"avatar_url"`
	Password     string         `json:"-"` // 永远不返回密码
	TokenQuota   int64          `json:"token_quota"`
	Status       int            `gorm:"default:1" json:"status"` // 1:active, 2:banned, 0:pending
}

// 可以在这里写 User 特有的逻辑
func (u *User) TableName() string {
    return "users"
}