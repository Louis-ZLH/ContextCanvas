package model

type AiModel struct {
	ID              int64    `gorm:"primaryKey;autoIncrement" json:"id"`
	Name            string  `gorm:"unique" json:"name"` // e.g. "gpt-4"
	Provider        string  `json:"provider"`
	InputPricePer1k float64 `gorm:"type:decimal(10,6)" json:"input_price_per_1k"`
	OutputPricePer1k float64 `gorm:"type:decimal(10,6)" json:"output_price_per_1k"`
}

func (a *AiModel) TableName() string {
	return "ai_models"
}