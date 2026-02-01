package idgen

import (
	"github.com/bwmarrin/snowflake"
	"log"
	"sync"
)

var (
	node *snowflake.Node
	once sync.Once
)

// InitSnowflake 初始化节点
// machineID: 当前服务器的编号 (0-1023)
func InitSnowflake(machineID int64) {
	once.Do(func() {
		var err error
		node, err = snowflake.NewNode(machineID)
		if err != nil {
			log.Fatalf("Snowflake node init failed: %v", err)
		}
	})
}

func GenID() int64 {
	return node.Generate().Int64()
}