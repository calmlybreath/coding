package redis

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"hash/fnv"
	"time"
	"tools/txmsg"
)

const defaultPayloadTTL = 7 * 24 * time.Hour

type Store struct {
	client     *GoClient
	shardCount int
	msgTTL     time.Duration
}

func NewStore(client *GoClient, shardCount int) *Store {
	if shardCount <= 0 {
		shardCount = 1
	}
	return &Store{client: client, shardCount: shardCount, msgTTL: defaultPayloadTTL}
}

func (s *Store) Add(ctx context.Context, topic string, msg *txmsg.Message, deliverAfter time.Duration) error {
	data, err := s.encode(msg)
	if err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	if err := s.client.SaveMessage(ctx, topic, msg.ID, data, s.msgTTL); err != nil {
		return fmt.Errorf("put payload: %w", err)
	}
	if err := s.client.Enqueue(ctx, topic, s.shard(msg.ID), msg.ID, deliverAfter); err != nil {
		s.client.DeleteMessage(ctx, topic, msg.ID)
		return fmt.Errorf("enqueue: %w", err)
	}
	return nil
}

func (s *Store) Remove(ctx context.Context, topic string, msg *txmsg.Message) error {
	if err := s.client.Dequeue(ctx, topic, s.shard(msg.ID), msg.ID); err != nil {
		return fmt.Errorf("dequeue: %w", err)
	}
	if err := s.client.DeleteMessage(ctx, topic, msg.ID); err != nil {
		return fmt.Errorf("del payload: %w", err)
	}
	return nil
}

func (s *Store) Poll(ctx context.Context, topic string, shards []int, batchSize int, visibilityTimeout time.Duration) ([]*txmsg.Message, error) {
	scanShards := shards
	if scanShards == nil {
		scanShards = make([]int, s.shardCount)
		for i := 0; i < s.shardCount; i++ {
			scanShards[i] = i
		}
	}
	var allIDs []string
	for _, shard := range scanShards {
		ids, err := s.client.Claim(ctx, topic, shard, batchSize, visibilityTimeout)
		if err != nil {
			return nil, fmt.Errorf("claim shard %d: %w", shard, err)
		}
		allIDs = append(allIDs, ids...)
	}
	if len(allIDs) == 0 {
		return nil, nil
	}

	msgDatas, err := s.client.LoadMessages(ctx, topic, allIDs)
	if err != nil {
		return nil, fmt.Errorf("get payloads: %w", err)
	}
	msgs := make([]*txmsg.Message, 0, len(msgDatas))
	for i, data := range msgDatas {
		if data == nil {
			continue
		}
		msg, err := s.decode(data)
		if err != nil {
			continue
		}
		if msg.ID == "" {
			msg.ID = allIDs[i]
		}
		msgs = append(msgs, msg)
	}
	return msgs, nil
}

func (s *Store) Reschedule(ctx context.Context, topic string, msg *txmsg.Message, deliverAfter time.Duration) error {
	if data, err := s.encode(msg); err == nil {
		s.client.SaveMessage(ctx, topic, msg.ID, data, s.msgTTL)
	}
	s.client.ExtendMessage(ctx, topic, msg.ID, s.msgTTL)
	if err := s.client.Enqueue(ctx, topic, s.shard(msg.ID), msg.ID, deliverAfter); err != nil {
		return fmt.Errorf("enqueue reschedule: %w", err)
	}
	return nil
}

func (s *Store) ShardCount() int { return s.shardCount }

func (s *Store) shard(msgID string) int {
	h := fnv.New32a()
	h.Write([]byte(msgID))
	return int(h.Sum32()) % s.shardCount
}

func (s *Store) encode(msg *txmsg.Message) ([]byte, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(msg); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (s *Store) decode(data []byte) (*txmsg.Message, error) {
	var msg txmsg.Message
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&msg); err != nil {
		return nil, err
	}
	return &msg, nil
}
