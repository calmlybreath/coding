package redis

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/go-redis/redis/v7"
)

// GoClient Redis 客户端封装，提供 payload/queue/heartbeat 原子操作
type GoClient struct {
	rc *redis.Client
}

// NewGoClient 创建 go-redis 客户端，入参可为 *redis.Client 或 *l5_redis.L5RedisClient
func NewGoClient(client interface{ WithContext(ctx context.Context) *redis.Client }) *GoClient {
	return &GoClient{rc: client.WithContext(context.Background())}
}

func (c *GoClient) withCtx(ctx context.Context) *redis.Client { return c.rc.WithContext(ctx) }

// --- Payload ---

func (c *GoClient) recordKey(topic, msgID string) string {
	return fmt.Sprintf("txmsg:record:%s:%s", topic, msgID)
}

func (c *GoClient) SaveMessage(ctx context.Context, topic, msgID string, data []byte, ttl time.Duration) error {
	return c.withCtx(ctx).Set(c.recordKey(topic, msgID), data, ttl).Err()
}

func (c *GoClient) LoadMessages(ctx context.Context, topic string, msgIDs []string) ([][]byte, error) {
	keys := make([]string, len(msgIDs))
	for i, id := range msgIDs {
		keys[i] = c.recordKey(topic, id)
	}
	vals, err := c.withCtx(ctx).MGet(keys...).Result()
	if err != nil {
		return nil, err
	}
	result := make([][]byte, len(vals))
	for i, v := range vals {
		if v == nil {
			continue
		}
		if s, ok := v.(string); ok {
			result[i] = []byte(s)
		}
	}
	return result, nil
}

func (c *GoClient) DeleteMessage(ctx context.Context, topic, msgID string) error {
	return c.withCtx(ctx).Del(c.recordKey(topic, msgID)).Err()
}

func (c *GoClient) ExtendMessage(ctx context.Context, topic, msgID string, ttl time.Duration) error {
	return c.withCtx(ctx).Expire(c.recordKey(topic, msgID), ttl).Err()
}

// --- Queue ---

func (c *GoClient) zsetKey(topic string, shard int) string {
	return fmt.Sprintf("txmsg:%s:%d", topic, shard)
}

func (c *GoClient) Enqueue(ctx context.Context, topic string, shard int, msgID string, deliverAfter time.Duration) error {
	return c.withCtx(ctx).ZAdd(c.zsetKey(topic, shard), &redis.Z{
		Score:  scoreAfter(deliverAfter),
		Member: msgID,
	}).Err()
}

func (c *GoClient) Dequeue(ctx context.Context, topic string, shard int, msgID string) error {
	return c.withCtx(ctx).ZRem(c.zsetKey(topic, shard), msgID).Err()
}

var claimScript = redis.NewScript(`
local members = redis.call('ZRANGEBYSCORE', KEYS[1], ARGV[1], ARGV[2], 'LIMIT', 0, ARGV[3])
if #members > 0 then
    for _, m in ipairs(members) do
        redis.call('ZADD', KEYS[1], ARGV[4], m)
    end
end
return members
`)

func (c *GoClient) Claim(ctx context.Context, topic string, shard int, limit int, visibilityTimeout time.Duration) ([]string, error) {
	key := c.zsetKey(topic, shard)
	result, err := claimScript.Run(c.withCtx(ctx), []string{key},
		"0",
		strconv.FormatFloat(scoreNow(), 'f', 0, 64),
		limit,
		strconv.FormatFloat(scoreAfter(visibilityTimeout), 'f', 0, 64),
	).Result()
	if err != nil {
		return nil, err
	}
	ids, ok := result.([]interface{})
	if !ok {
		return nil, nil
	}
	strs := make([]string, 0, len(ids))
	for _, id := range ids {
		if s, ok := id.(string); ok {
			strs = append(strs, s)
		}
	}
	return strs, nil
}

// --- Heartbeat ---

func (c *GoClient) machinesKey(topic string) string {
	return fmt.Sprintf("txmsg:machines:%s", topic)
}

func (c *GoClient) Heartbeat(ctx context.Context, topic, machineID string, ttl time.Duration) error {
	return c.withCtx(ctx).ZAdd(c.machinesKey(topic), &redis.Z{
		Score:  float64(time.Now().Unix()),
		Member: machineID,
	}).Err()
}

func (c *GoClient) ActiveMachines(ctx context.Context, topic string, ttl time.Duration) ([]string, error) {
	rc := c.withCtx(ctx)
	key := c.machinesKey(topic)
	deadline := time.Now().Add(-ttl).Unix()
	rc.ZRemRangeByScore(key, "0", strconv.FormatInt(deadline, 10))
	return rc.ZRange(key, 0, -1).Result()
}

func scoreNow() float64                  { return float64(time.Now().Unix()) }
func scoreAfter(d time.Duration) float64 { return float64(time.Now().Add(d).Unix()) }
