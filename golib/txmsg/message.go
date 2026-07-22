package txmsg

import (
	"encoding/gob"
	"fmt"
	"math/rand"
	"time"
)

// Message 事务消息。Body 为 interface{}，gob 编码自动保留具体类型。
// 业务方需在自己的 init() 中调用 gob.Register 注册 Body 类型。
type Message struct {
	ID         string
	Body       interface{}
	RetryCount int
	CreateTime time.Time
}

// NewMessage 创建消息，自动生成唯一 ID
func NewMessage(body interface{}) *Message {
	return &Message{
		ID:         newMsgID(),
		Body:       body,
		RetryCount: 0,
		CreateTime: time.Now(),
	}
}

// RegisterTypes 注册 Body 类型，gob 编解码使用。显式调用，避免遗忘。
func RegisterTypes(samples ...interface{}) {
	for _, s := range samples {
		gob.Register(s)
	}
}

func newMsgID() string {
	return fmt.Sprintf("%d_%d", time.Now().UnixNano(), rand.Int63())
}
