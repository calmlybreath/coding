package agg

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bluele/gcache"
)

// TestSingleflightCacheBasic:首次 Get 走 fn,第二次命中缓存不再走 fn。
func TestSingleflightCacheBasic(t *testing.T) {
	sfc := NewSingleflightCache(gcache.New(100).LFU().Build(), time.Second)

	key := "room_1"
	v, err := sfc.Get(key, func() (interface{}, error) {
		return fmt.Sprintf("res_%v", key), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if v.(string) != "res_room_1" {
		t.Errorf("got %v", v)
	}

	// 第二次:应命中缓存,fn 不应被执行。
	v2, err := sfc.Get(key, func() (interface{}, error) {
		t.Error("fn should not run on cache hit")
		return "wrong", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if v2.(string) != "res_room_1" {
		t.Errorf("got %v", v2)
	}
}

// TestSingleflightCacheSingleflight:并发同 key Get,fn 只应被执行一次。
func TestSingleflightCacheSingleflight(t *testing.T) {
	sfc := NewSingleflightCache(gcache.New(100).LFU().Build(), time.Second)

	var calls int64
	const goroutines = 20
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _ = sfc.Get("hot", func() (interface{}, error) {
				atomic.AddInt64(&calls, 1)
				time.Sleep(50 * time.Millisecond) // 故意慢,放大并发窗口
				return "v", nil
			})
		}()
	}
	close(start)
	wg.Wait()

	if calls != 1 {
		t.Errorf("expected fn called once via singleflight, got %d", calls)
	}
}
