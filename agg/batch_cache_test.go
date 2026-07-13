package agg

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type SimpleRoomInfoMGet struct{}

func (this *SimpleRoomInfoMGet) MGet(ctx context.Context, keys []interface{}) (map[interface{}]interface{}, error) {
	return testMGet(ctx, keys)
}

// TestBatchCacheBasic:BatchCache(agg + lfu cache)基本正确性。
func TestBatchCacheBasic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	aggCache, err := NewBatchCache(ctx, &SimpleRoomInfoMGet{}, 1000, time.Millisecond*500)
	if err != nil {
		t.Fatal(err)
	}

	keys := genUniqueKeys(100, "v1")
	res, err := aggCache.MGet(ctx, keys)
	if err != nil {
		t.Fatalf("MGet err: %v", err)
	}
	if len(res) != len(keys) {
		t.Fatalf("expected %d results, got %d", len(keys), len(res))
	}
	for _, k := range keys {
		r, ok := res[k]
		if !ok {
			t.Errorf("key %v missing", k)
			continue
		}
		if r.(RoomInfo) != GetRes(k.(string)) {
			t.Errorf("key %v res mismatch", k)
		}
	}
}

// TestBatchCacheNoCacheBasic:无缓存版本(只有 agg + singleflight)基本正确性。
func TestBatchCacheNoCacheBasic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	aggCache, err := NewBatchCacheNoCache(ctx, &SimpleRoomInfoMGet{})
	if err != nil {
		t.Fatal(err)
	}

	keys := genUniqueKeys(50, "v3")
	res, err := aggCache.MGet(ctx, keys)
	if err != nil {
		t.Fatalf("MGet err: %v", err)
	}
	if len(res) != len(keys) {
		t.Fatalf("expected %d results, got %d", len(keys), len(res))
	}
}

// TestBatchCacheHit:同一批 key 第二次 MGet 应命中缓存,不再走下游。
func TestBatchCacheHit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var downstreamCalls int64
	mget := &countingMGet{calls: &downstreamCalls}
	aggCache, err := NewBatchCache(ctx, mget, 1000, time.Second*5)
	if err != nil {
		t.Fatal(err)
	}

	keys := []interface{}{"hit_1", "hit_2", "hit_3"}
	if _, err := aggCache.MGet(ctx, keys); err != nil {
		t.Fatal(err)
	}
	callsAfterFirst := atomic.LoadInt64(&downstreamCalls)
	if callsAfterFirst == 0 {
		t.Fatal("expected downstream calls on first MGet")
	}

	// 第二次:全部命中缓存,下游调用数不应增加。
	if _, err := aggCache.MGet(ctx, keys); err != nil {
		t.Fatal(err)
	}
	callsAfterSecond := atomic.LoadInt64(&downstreamCalls)
	if callsAfterSecond != callsAfterFirst {
		t.Errorf("expected cache hit (no new downstream calls), got %d -> %d", callsAfterFirst, callsAfterSecond)
	}
}

// TestBatchCacheConcurrent:并发 MGet 相同 key,验证 singleflight 去重 + 无竞争。
func TestBatchCacheConcurrent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var downstreamCalls int64
	mget := &countingMGet{calls: &downstreamCalls}
	aggCache, err := NewBatchCache(ctx, mget, 1000, time.Second*5)
	if err != nil {
		t.Fatal(err)
	}

	const goroutines = 30
	key := "hot_key"
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			res, err := aggCache.MGet(ctx, []interface{}{key})
			if err != nil {
				t.Errorf("MGet err: %v", err)
				return
			}
			if r, ok := res[key]; !ok || r.(RoomInfo) != GetRes(key) {
				t.Errorf("key %v res wrong: %+v", key, res[key])
			}
		}()
	}
	close(start)
	wg.Wait()

	// singleflight + cache:并发同 key 只应产生极少数下游调用。
	calls := atomic.LoadInt64(&downstreamCalls)
	t.Logf("downstream calls for %d concurrent same-key MGet: %d", goroutines, calls)
	if calls > 5 {
		t.Errorf("expected singleflight to dedup, got %d downstream calls", calls)
	}
}

// countingMGet 包装 testMGet 并统计下游调用次数(用于验证缓存命中/singleflight)。
type countingMGet struct {
	calls *int64
}

func (m *countingMGet) MGet(ctx context.Context, keys []interface{}) (map[interface{}]interface{}, error) {
	atomic.AddInt64(m.calls, 1)
	return testMGet(ctx, keys)
}
