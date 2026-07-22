package memory

import (
	"bytes"
	"context"
	"encoding/gob"
	"sync"
	"time"
	"tools/txmsg"
)

type MemoryStore struct {
	mu      sync.Mutex
	payload map[string][]byte
	topics  map[string][]msgEntry
}

type msgEntry struct {
	msgID string
	score float64
}

func New() *MemoryStore {
	return &MemoryStore{
		payload: make(map[string][]byte),
		topics:  make(map[string][]msgEntry),
	}
}

func (s *MemoryStore) Add(ctx context.Context, topic string, msg *txmsg.Message, deliverAfter time.Duration) error {
	data, err := s.encode(msg)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.payload[msg.ID] = data
	s.topics[topic] = append(s.topics[topic], msgEntry{msgID: msg.ID, score: scoreAfter(deliverAfter)})
	return nil
}

func (s *MemoryStore) Remove(ctx context.Context, topic string, msg *txmsg.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.payload, msg.ID)
	entries := s.topics[topic]
	for i, e := range entries {
		if e.msgID == msg.ID {
			s.topics[topic] = append(entries[:i], entries[i+1:]...)
			break
		}
	}
	return nil
}

func (s *MemoryStore) Poll(ctx context.Context, topic string, shards []int, batchSize int, visibilityTimeout time.Duration) ([]*txmsg.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := scoreNow()
	newScore := scoreAfter(visibilityTimeout)
	entries := s.topics[topic]
	var claimed []msgEntry
	var remaining []msgEntry
	for _, e := range entries {
		if e.score <= now && len(claimed) < batchSize {
			claimed = append(claimed, msgEntry{msgID: e.msgID, score: newScore})
		} else {
			remaining = append(remaining, e)
		}
	}
	s.topics[topic] = append(remaining, claimed...)

	msgs := make([]*txmsg.Message, 0, len(claimed))
	for _, e := range claimed {
		data, ok := s.payload[e.msgID]
		if !ok {
			continue
		}
		msg, err := s.decode(data)
		if err != nil {
			continue
		}
		msgs = append(msgs, msg)
	}
	return msgs, nil
}

func (s *MemoryStore) Reschedule(ctx context.Context, topic string, msg *txmsg.Message, deliverAfter time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries := s.topics[topic]
	filtered := entries[:0]
	for _, e := range entries {
		if e.msgID != msg.ID {
			filtered = append(filtered, e)
		}
	}
	s.topics[topic] = append(filtered, msgEntry{msgID: msg.ID, score: scoreAfter(deliverAfter)})

	data, err := s.encode(msg)
	if err != nil {
		return err
	}
	s.payload[msg.ID] = data
	return nil
}

func (s *MemoryStore) encode(msg *txmsg.Message) ([]byte, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(msg); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (s *MemoryStore) decode(data []byte) (*txmsg.Message, error) {
	var msg txmsg.Message
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

func scoreNow() float64                  { return float64(time.Now().Unix()) }
func scoreAfter(d time.Duration) float64 { return float64(time.Now().Add(d).Unix()) }
