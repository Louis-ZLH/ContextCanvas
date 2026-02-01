package model

import (
	"time"
)




type NodeEdge struct {
	ParentNodeID int64     `gorm:"primaryKey" json:"parent_node_id,string"`
	ChildNodeID  int64     `gorm:"primaryKey;index" json:"child_node_id,string"` // 联合主键，同时为Child建立索引
	CreatedAt    time.Time `json:"created_at"`
    // Edge一般不软删除，直接物理删除
}

func (n *NodeEdge) TableName() string {
	return "node_edges"
}