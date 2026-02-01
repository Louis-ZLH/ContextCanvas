package model


type Node struct {
	BaseModel
	CanvasID    int64          `gorm:"index" json:"canvas_id,string"`
	NodeType    string         `gorm:"type:varchar(20)" json:"node_type"` // 'chat', 'resource'
	ResourceURL string         `gorm:"type:varchar(2048)" json:"resource_url,omitempty"`
	PosX        float64        `json:"pos_x"`
	PosY        float64        `json:"pos_y"`
}

func (n *Node) TableName() string {
	return "nodes"
}