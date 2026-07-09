package agg

import (
	"context"
	"time"

	"github.com/bluele/gcache"
)

// Cacher 是缓存抽象,BatchCache / SingleflightCache 都基于它。
type Cacher interface {
	SetWithExpire(key, value interface{}, expiration time.Duration) error
	Get(key interface{}) (interface{}, error)
	HitRate() float64
}

// ImportantKeyChecker 判断一个 key 是否"重要":重要 key 走独立缓存,不会被普通 LFU/LRU 淘汰。
type ImportantKeyChecker interface {
	IsImportant(ctx context.Context, key interface{}) bool
}

// ImportantStringKey 是 ImportantKeyChecker 的 string-key 实现。
type ImportantStringKey struct {
	keys map[string]struct{}
}

// NewImportantStringKey 用一组重要 key 构造。
func NewImportantStringKey(keys []string) *ImportantStringKey {
	km := make(map[string]struct{})
	for _, k := range keys {
		km[k] = struct{}{}
	}
	return &ImportantStringKey{keys: km}
}

func (k *ImportantStringKey) IsImportant(ctx context.Context, key interface{}) bool {
	_, ok := k.keys[key.(string)]
	return ok
}

// Cache 包装 gcache.Cache,提供 Cacher 语义。
type Cache struct {
	gcache.Cache
}

// NewLFUCache 构造一个 LFU 缓存。
func NewLFUCache(size int) *Cache {
	return &Cache{Cache: gcache.New(size).LFU().Build()}
}

// NewLRUCache 构造一个 LRU 缓存。
func NewLRUCache(size int) *Cache {
	return &Cache{Cache: gcache.New(size).LRU().Build()}
}
