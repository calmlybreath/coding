package agg

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bluele/gcache"
	"tools/singleflight"
)

type batchCacheStat struct {
	callMGetNum uint64
}

// BatchCache = 批量聚合(Aggregator) + 防击穿(singleflight) + 缓存(cache)。
// 在不增加下游 QPS 的前提下,把短时间内的多个 mget 攒成一批、去重、缓存复用。
type BatchCache struct {
	agg   Agg
	group singleflight.Group
	stat  batchCacheStat

	normalCache       singleflight.Cacher
	importantKey      ImportantKeyChecker
	importantKeyCache singleflight.Cacher

	cacheExpiration      time.Duration
	ignoreKeyResNotExist bool
}

// NewBatchCacheNoCache 只用 Aggregator + singleflight(无缓存),仅防击穿。
func NewBatchCacheNoCache(ctx context.Context, mget MGetter) (*BatchCache, error) {
	a, err := NewAggregator(ctx, Params{
		ID:       "defaultAgg",
		Config:   NewDefaultConfig(),
		MGetImpl: mget,
		ErrorLog: log.Printf,
	})
	if err != nil {
		return nil, err
	}
	return &BatchCache{
		agg:   a,
		group: singleflight.Group{},
	}, nil
}

// NewBatchCache 是最常用的:Aggregator + singleflight + LFU 缓存。
func NewBatchCache(ctx context.Context, mget MGetter, cacheSize int, cacheExpiration time.Duration) (*BatchCache, error) {
	a, err := NewAggregator(ctx, Params{
		ID:       "defaultAgg",
		Config:   NewDefaultConfig(),
		MGetImpl: mget,
		ErrorLog: log.Printf,
	})
	if err != nil {
		return nil, err
	}
	return NewCustomBatchCache(
		a,
		gcache.New(cacheSize).LFU().Build(),
		nil,
		nil,
		cacheExpiration,
		true,
	), nil
}

// NewBatchCacheWithImportantKey 在 NewBatchCache 基础上,为"重要 key"使用独立缓存,
// 避免热 key 被 LFU 淘汰。需要实现 ImportantKeyChecker。
func NewBatchCacheWithImportantKey(
	ctx context.Context,
	mget MGetter,
	useNormalCache bool,
	importantKey ImportantKeyChecker,
	cacheSize int,
	cacheExpiration time.Duration,
) (*BatchCache, error) {
	a, err := NewAggregator(ctx, Params{
		ID:       "defaultAgg",
		Config:   NewDefaultConfig(),
		MGetImpl: mget,
		ErrorLog: log.Printf,
	})
	if err != nil {
		return nil, err
	}

	var normalCache singleflight.Cacher
	if useNormalCache {
		normalCache = gcache.New(cacheSize).LFU().Build()
	}
	return NewCustomBatchCache(
		a,
		normalCache,
		importantKey,
		gcache.New(cacheSize).LFU().Build(),
		cacheExpiration,
		true,
	), nil
}

// NewCustomBatchCache 完全自定义:自带 Aggregator、缓存、ImportantKeyChecker。
func NewCustomBatchCache(
	agg Agg,
	normalCache singleflight.Cacher,
	importantKey ImportantKeyChecker,
	importantKeyCache singleflight.Cacher,
	cacheExpiration time.Duration,
	ignoreKeyResNotExist bool,
) *BatchCache {
	return &BatchCache{
		agg:                  agg,
		normalCache:          normalCache,
		importantKey:         importantKey,
		importantKeyCache:    importantKeyCache,
		cacheExpiration:      cacheExpiration,
		group:                singleflight.Group{},
		ignoreKeyResNotExist: ignoreKeyResNotExist,
	}
}

// ErrItemNotExist 表示某个 key 在下游结果里不存在。
type ErrItemNotExist struct {
	Key interface{}
}

func (e *ErrItemNotExist) Error() string {
	return fmt.Sprintf("key not exist: %s", e.Key)
}

func (c *BatchCache) useImportantCache() bool {
	return c.importantKey != nil && c.importantKeyCache != nil
}

// MGet 批量查询 keys。先查缓存(important key 走独立缓存),未命中的 key 通过
// singleflight 去重后交给 Aggregator 批量回源,回源结果再写回缓存。
func (c *BatchCache) MGet(ctx context.Context, keys []interface{}) (map[interface{}]interface{}, error) {
	atomic.AddUint64(&c.stat.callMGetNum, 1)
	key2Res := make(map[interface{}]interface{})
	for _, key := range keys {
		if c.useImportantCache() && c.importantKey.IsImportant(ctx, key) {
			if val, err := c.importantKeyCache.Get(key); err == nil {
				key2Res[key] = val
			}
		} else if c.normalCache != nil {
			if val, err := c.normalCache.Get(key); err == nil {
				key2Res[key] = val
			}
		}
	}

	if len(key2Res) == len(keys) {
		return key2Res, nil
	}

	missedKeys := make([]interface{}, 0)
	for _, key := range keys {
		if _, ok := key2Res[key]; !ok {
			missedKeys = append(missedKeys, key)
		}
	}

	missedKey2Res := sync.Map{}
	// 不用 errgroup:这里要"所有 key 都跑完、取首个错误"的语义,而不是首个错误就取消其它。
	// errgroup 的派生 ctx 会被丢弃,是坏味道,这里换成 WaitGroup + Once。
	var wg sync.WaitGroup
	var firstErr error
	var errOnce sync.Once
	for _, tempKey := range missedKeys {
		key := tempKey
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err, _ := c.group.Do(key, func() (interface{}, error) {
				key2Res, err := c.agg.SubmitAndWait(ctx, []interface{}{key})
				if err != nil {
					return nil, fmt.Errorf("agg.SubmitAndWait failed: key(%s), err(%w)", key, err)
				}
				res, ok := key2Res[key]
				if !ok {
					return nil, &ErrItemNotExist{Key: key}
				}
				if c.useImportantCache() && c.importantKey.IsImportant(ctx, key) {
					c.importantKeyCache.SetWithExpire(key, res, c.cacheExpiration)
				} else if c.normalCache != nil {
					c.normalCache.SetWithExpire(key, res, c.cacheExpiration)
				}
				return res, nil
			})
			if err != nil {
				if _, ok := err.(*ErrItemNotExist); ok && c.ignoreKeyResNotExist {
					return
				}
				errOnce.Do(func() { firstErr = err })
				return
			}
			missedKey2Res.Store(key, res)
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return nil, fmt.Errorf("agg.SubmitAndWait failed: err(%w)", firstErr)
	}

	missedKey2Res.Range(func(key, value interface{}) bool {
		key2Res[key] = value
		return true
	})
	return key2Res, nil
}
