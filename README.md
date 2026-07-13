# tools

个人后端工具集,解决批量拉取场景下的几个常见痛点:下游 QPS 放大、缓存击穿、多级依赖查询的 N+1。

- `agg` —— 时间窗口批量聚合(`Aggregator` + `BatchCache`)
- `singleflight` —— 去重原语(`Group`)及在其上的防击穿缓存(`Cache`)
- `querier` —— 分阶段 DAG 批量查询编排
- `task` —— 极简的 future/promise

依赖方向:`agg` 依赖 `singleflight`;`querier`、`task` 独立。

## agg

把短时间内的多个 mget 请求**按时间窗口攒成一批**打下游,降低下游 QPS;叠加 singleflight 防击穿、cache 命中复用。与 `x/sync/singleflight` 只去重 in-flight 请求不同,`Aggregator` 会跨调用方按窗口批量。

### 最常用:Aggregator + lfu cache + singleflight

```go
import "tools/agg"

// 实现 MGetter:真正的下游批量查询
type RoomMGet struct{}
func (r *RoomMGet) MGet(ctx context.Context, keys []interface{}) (map[interface{}]interface{}, error) {
    // 调下游 rpc / db ...
}

bc, err := agg.NewBatchCache(ctx, &RoomMGet{}, 10000 /*cacheSize*/, 10*time.Second /*ttl*/)
res, err := bc.MGet(ctx, []interface{}{"room_1", "room_2"})
// res: map[interface{}]interface{}{"room_1": ..., "room_2": ...}
```

### 构造函数怎么选

| 构造 | 能力 |
| --- | --- |
| `NewBatchCacheNoCache` | Aggregator + singleflight(防击穿,无 cache) |
| `NewBatchCache` | Aggregator + singleflight + lfu cache(最常用) |
| `NewBatchCacheWithImportantKey` | `NewBatchCache` + 重要 key 独立缓存(热 key 不被 LFU 淘汰) |
| `NewCustomBatchCache` | 全自定义(自带 Aggregator / cache / ImportantKeyChecker) |

缓存参数类型是 `singleflight.Cacher`(`gcache.Cache` 开箱即满足)。

### 行为要点

- `SubmitAndWait` 自动按 `MaxBatchSize` 分片,调用方无需自行拆分;直接用 `Submit` 则单批不能超过 `MaxBatchSize`。
- **失败语义**:任意一个 key 出错或 ctx 取消,`SubmitAndWait`/`MGet` 立即返回该错误,不返回部分结果(整批成功或整批失败)。
- `IgnoreKeyResNotExist=true` 时,下游结果里不存在的 key 当 nil 返回而非报错。
- `Stat()` 返回的统计快照全部原子读取,可安全在其它 goroutine 调用。

## singleflight

`Group` 是 `golang.org/x/sync/singleflight.Group` 的派生版,额外加了 `Stat`/`StatAndClear` 统计。并发同 key 的调用只执行一次,其它等结果。

```go
import "tools/singleflight"

var g singleflight.Group
v, err, shared := g.Do(key, func() (interface{}, error) { /* 回源 */ })
```

如果只想要 singleflight + cache(不跨调用方批量),用更轻的 `Cache`:

```go
import "tools/singleflight"

c := singleflight.NewCache(cache, 10*time.Second) // cache 需满足 singleflight.Cacher
v, err := c.Get(key, func() (interface{}, error) { /* 未命中时回源,并发只执行一次 */ })
```

## querier

按 `StepFunc` 顺序编排多级依赖的批量查询:每个 step 先串行收集 key,再**并发**执行所有 `NeedQuery` 的 Querier,下一 step 可通过 `BatchLoad` 用上一 step 的结果。解决多级依赖下的 N+1。

```go
import "tools/querier"

type UserQuerier struct{ querier.BaseQuerier }
func (q *UserQuerier) Query(ctx context.Context) error {
    // 用 q.pendingKeys 批量查 user,写回 q.key2Res
    return nil
}

store := querier.NewStore(ctx)
store.AddQuerier("user", &UserQuerier{})
store.AddQuerier("profile", &ProfileQuerier{})

store.SetStepFuncs([]querier.StepFunc{
    func(s *querier.Store) error {
        s.AddKeys("user", []interface{}{uid1, uid2}, false)
        return nil
    },
    func(s *querier.Store) error {
        // 第二步依赖第一步结果
        users := s.BatchLoad("user", []interface{}{uid1, uid2})
        keys := toProfileKeys(users)
        s.AddKeys("profile", keys, false)
        return nil
    },
})
if err := store.Exec(); err != nil { /* ... */ }
u, _ := store.Load("user", uid1)
```

- 嵌入 `BaseQuerier` 只需实现 `Query(ctx)`。
- `AddKeys` 第二个参数 `ignoreErr` 控制该 Querier 出错时是否中断整个 `Exec`。
- `AddKeys` 会自动跳过已加载过的 key,不会重复查询。

## task

```go
import "tools/task"

t := task.Start(ctx, func(ctx context.Context) (interface{}, error) {
    return doWork(ctx)
})
data, err := t.Await() // 阻塞拿结果;ctx 先结束则返回 ctx.Err()
```

- `Start` 把 ctx 透传给 `TaskFunc`,`TaskFunc` 应尊重 ctx,否则取消后 goroutine 仍会跑到自然结束。
- `Await` 只能调用一次,第二次返回零值。
