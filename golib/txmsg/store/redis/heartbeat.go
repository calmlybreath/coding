package redis

import (
	"context"
	"sort"
	"sync"
	"time"
)

// HeartbeatAssigner 基于 Redis 心跳的分片动态分配器
type HeartbeatAssigner struct {
	client            *GoClient
	topic             string
	machineID         string
	shardCount        int
	heartbeatInterval time.Duration
	heartbeatTTL      time.Duration
	rebalanceInterval time.Duration

	mu          sync.RWMutex
	localShards []int

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

type HeartbeatOption func(a *HeartbeatAssigner)

func WithHeartbeatInterval(d time.Duration) HeartbeatOption {
	return func(a *HeartbeatAssigner) { a.heartbeatInterval = d }
}
func WithHeartbeatTTL(d time.Duration) HeartbeatOption {
	return func(a *HeartbeatAssigner) { a.heartbeatTTL = d }
}
func WithRebalanceInterval(d time.Duration) HeartbeatOption {
	return func(a *HeartbeatAssigner) { a.rebalanceInterval = d }
}

func NewHeartbeatAssigner(client *GoClient, topic, machineID string, shardCount int, opts ...HeartbeatOption) *HeartbeatAssigner {
	a := &HeartbeatAssigner{
		client:            client,
		topic:             topic,
		machineID:         machineID,
		shardCount:        shardCount,
		heartbeatInterval: 2 * time.Second,
		heartbeatTTL:      10 * time.Second,
		rebalanceInterval: 5 * time.Second,
	}
	for _, o := range opts {
		o(a)
	}
	a.ctx, a.cancel = context.WithCancel(context.Background())
	a.heartbeat()
	a.rebalance()
	a.wg.Add(2)
	go a.heartbeatLoop()
	go a.rebalanceLoop()
	return a
}

func (a *HeartbeatAssigner) Shards(topic string) []int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.localShards
}

func (a *HeartbeatAssigner) Close() {
	a.cancel()
	a.wg.Wait()
}

func (a *HeartbeatAssigner) heartbeatLoop() {
	defer a.wg.Done()
	ticker := time.NewTicker(a.heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			a.heartbeat()
		case <-a.ctx.Done():
			return
		}
	}
}

func (a *HeartbeatAssigner) heartbeat() {
	a.client.Heartbeat(a.ctx, a.topic, a.machineID, a.heartbeatTTL)
}

func (a *HeartbeatAssigner) rebalanceLoop() {
	defer a.wg.Done()
	ticker := time.NewTicker(a.rebalanceInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			a.rebalance()
		case <-a.ctx.Done():
			return
		}
	}
}

func (a *HeartbeatAssigner) rebalance() {
	machines, err := a.client.ActiveMachines(a.ctx, a.topic, a.heartbeatTTL)
	if err != nil || len(machines) == 0 {
		return
	}
	sort.Strings(machines)

	myIdx := -1
	for i, m := range machines {
		if m == a.machineID {
			myIdx = i
			break
		}
	}
	if myIdx < 0 {
		a.mu.Lock()
		a.localShards = nil
		a.mu.Unlock()
		return
	}

	numMachines := len(machines)
	var myShards []int
	for s := 0; s < a.shardCount; s++ {
		if s%numMachines == myIdx {
			myShards = append(myShards, s)
		}
	}
	a.mu.Lock()
	a.localShards = myShards
	a.mu.Unlock()
}
