package agg

import "time"

// SingleflightCache 在普通缓存之上叠加 singleflight:缓存未命中时,
// 同一个 key 的并发回源只会执行一次 fn,避免缓存击穿。
type SingleflightCache struct {
	singleflight Singleflight
	cache        Cacher
	expiration   time.Duration
}

// NewSingleflightCache 构造。expiration 必须大于 0。
func NewSingleflightCache(cache Cacher, expiration time.Duration) *SingleflightCache {
	if expiration == 0 {
		panic("expiration must > 0")
	}
	return &SingleflightCache{
		cache:        cache,
		singleflight: Singleflight{},
		expiration:   expiration,
	}
}

// Get 取 key:命中缓存直接返回;未命中则用 singleflight 包裹 fn 回源并写缓存。
func (c *SingleflightCache) Get(key interface{}, fn func() (interface{}, error)) (interface{}, error) {
	if cachedValue, err := c.cache.Get(key); err == nil {
		return cachedValue, nil
	}
	value, err, _ := c.singleflight.Do(key, func() (interface{}, error) {
		v, err := fn()
		if err != nil {
			return nil, err
		}
		c.cache.SetWithExpire(key, v, c.expiration)
		return v, nil
	})
	return value, err
}

// SingleflightCacheStats 是 SingleflightCache 的统计快照。
type SingleflightCacheStats struct {
	SingleflightStat
	CacheHitRate float64
}

// StatAndClear 返回统计并清零。
func (c *SingleflightCache) StatAndClear() SingleflightCacheStats {
	return SingleflightCacheStats{
		SingleflightStat: c.singleflight.StatAndClear(),
		CacheHitRate:     c.cache.HitRate(),
	}
}
