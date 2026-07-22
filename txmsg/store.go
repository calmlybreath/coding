package txmsg

import (
	"context"
	"time"
)

// Store 消息存储接口
type Store interface {
	Add(ctx context.Context, topic string, msg *Message, deliverAfter time.Duration) error
	Remove(ctx context.Context, topic string, msg *Message) error
	Poll(ctx context.Context, topic string, shards []int, batchSize int, visibilityTimeout time.Duration) ([]*Message, error)
	Reschedule(ctx context.Context, topic string, msg *Message, deliverAfter time.Duration) error
}

// ShardAssigner 分片分配器
type ShardAssigner interface {
	Shards(topic string) []int
}
