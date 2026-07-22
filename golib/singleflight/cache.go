package singleflight

import "time"

// Cacher 是缓存抽象,Cache 基于它。gcache.Cache 等开箱即满足该接口。
type Cacher interface {
	SetWithExpire(key, value interface{}, expiration time.Duration) error
	Get(key interface{}) (interface{}, error)
	HitRate() float64
}

// Cache 在普通缓存之上叠加 singleflight:缓存未命中时,
// 同一个 key 的并发回源只会执行一次 fn,避免缓存击穿。
type Cache struct {
	group      Group
	cache      Cacher
	expiration time.Duration
}

// NewCache 构造。expiration 必须大于 0。
func NewCache(cache Cacher, expiration time.Duration) *Cache {
	if expiration == 0 {
		panic("expiration must > 0")
	}
	return &Cache{
		cache:      cache,
		group:      Group{},
		expiration: expiration,
	}
}

// Get 取 key:命中缓存直接返回;未命中则用 singleflight 包裹 fn 回源并写缓存。
func (c *Cache) Get(key interface{}, fn func() (interface{}, error)) (interface{}, error) {
	if cachedValue, err := c.cache.Get(key); err == nil {
		return cachedValue, nil
	}
	value, err, _ := c.group.Do(key, func() (interface{}, error) {
		v, err := fn()
		if err != nil {
			return nil, err
		}
		c.cache.SetWithExpire(key, v, c.expiration)
		return v, nil
	})
	return value, err
}

// CacheStats 是 Cache 的统计快照。
type CacheStats struct {
	GroupStat
	CacheHitRate float64
}

// StatAndClear 返回统计并清零。
func (c *Cache) StatAndClear() CacheStats {
	return CacheStats{
		GroupStat:    c.group.StatAndClear(),
		CacheHitRate: c.cache.HitRate(),
	}
}
