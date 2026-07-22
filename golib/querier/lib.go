package querier

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/sync/errgroup"
)

// Querier 是单个数据源的批量查询抽象:给定一批已去重的 key,返回 key->结果。
// 实现是纯函数,不持有共享状态——去重、缓存、并发编排都交给 Store。
type Querier[K comparable, V any] interface {
	Query(ctx context.Context, keys []K) (map[K]V, error)
}

// QuerierFunc 是 Querier 的函数形式,大多数场景直接传函数即可,无需自定义类型。
type QuerierFunc[K comparable, V any] func(ctx context.Context, keys []K) (map[K]V, error)

func (f QuerierFunc[K, V]) Query(ctx context.Context, keys []K) (map[K]V, error) {
	return f(ctx, keys)
}

// StepFunc 是一个编排步骤:通常在其中用 Handle 收集 key,可用 Handle.BatchLoad 取上一步结果。
// Handle 通过闭包捕获,StepFunc 本身不需要 Store 入参。
type StepFunc func() error

// runner 是 Store 内部用来统一驱动异构(不同 K/V)querier 的非泛型接口。
type runner interface {
	needQuery() bool
	run(ctx context.Context) error
	clearPending()
	reset()
	ignoreErr() bool
	name() string
}

// querierEntry 是 Handle 背后的状态:pending key 收集、results 缓存、错误策略。
//
// 并发安全靠 Store 的执行结构保证,无需加锁:
//   - addKeys / BatchLoad / Load / reset 由主 goroutine 在 step 边界调用;
//   - run / clearPending 由 worker goroutine 调用;
//   - 同一 entry 的字段在主 goroutine 与 worker 之间靠 errgroup.Wait 同步,不会交错访问。
type querierEntry[K comparable, V any] struct {
	q               Querier[K, V]
	qName           string
	shouldIgnoreErr bool
	pending         map[K]struct{}
	results         map[K]V
}

func (e *querierEntry[K, V]) addKeys(keys []K) {
	for _, k := range keys {
		if _, ok := e.results[k]; ok {
			continue // 已有结果,跳过(缓存命中)
		}
		e.pending[k] = struct{}{}
	}
}

func (e *querierEntry[K, V]) needQuery() bool {
	return len(e.pending) > 0
}

func (e *querierEntry[K, V]) run(ctx context.Context) error {
	keys := make([]K, 0, len(e.pending))
	for k := range e.pending {
		keys = append(keys, k)
	}
	res, err := e.q.Query(ctx, keys)
	if err != nil {
		return err
	}
	// 只记入属于本次 pending 的 key,忽略 querier 多返回的 key。
	for k, v := range res {
		if _, ok := e.pending[k]; ok {
			e.results[k] = v
		}
	}
	return nil
}

func (e *querierEntry[K, V]) clearPending() {
	e.pending = make(map[K]struct{})
}

func (e *querierEntry[K, V]) reset() {
	e.pending = make(map[K]struct{})
	e.results = make(map[K]V)
}

func (e *querierEntry[K, V]) ignoreErr() bool { return e.shouldIgnoreErr }
func (e *querierEntry[K, V]) name() string    { return e.qName }

// Handle 是已注册 querier 的类型化句柄。收 key / 取结果都走它,无字符串查表、无类型断言。
type Handle[K comparable, V any] struct {
	entry *querierEntry[K, V]
}

// AddKey 追加单个待查询 key。
func (h Handle[K, V]) AddKey(key K) { h.entry.addKeys([]K{key}) }

// AddKeys 追加一批待查询 key,自动去重(已有结果的 key 会被跳过)。
func (h Handle[K, V]) AddKeys(keys []K) { h.entry.addKeys(keys) }

// Load 取单个 key 的结果。
func (h Handle[K, V]) Load(key K) (V, bool) {
	v, ok := h.entry.results[key]
	return v, ok
}

// BatchLoad 批量取结果;不存在的 key 不会出现在返回的 map 里。
func (h Handle[K, V]) BatchLoad(keys []K) map[K]V {
	res := make(map[K]V, len(keys))
	for _, k := range keys {
		if v, ok := h.entry.results[k]; ok {
			res[k] = v
		}
	}
	return res
}

// Store 管理多个 Querier,按 StepFunc 顺序编排它们的批量查询。
type Store struct {
	ctx     context.Context
	runners []runner
	steps   []StepFunc
}

// NewStore 创建一个空的 Store。
func NewStore(ctx context.Context) *Store {
	return &Store{ctx: ctx}
}

// Config 是注册 Querier 时的配置。
type Config struct {
	IgnoreErr bool // 查询出错时是否忽略(true:清 pending 返回 nil;false:保留 pending 返回错误,支持重试)
}

// Option 调整 Config。
type Option func(*Config)

// WithIgnoreErr 让该 querier 在查询出错时忽略错误。
func WithIgnoreErr() Option {
	return func(c *Config) { c.IgnoreErr = true }
}

// Add 注册一个 Querier 并返回类型化 Handle。
// Go 不允许非泛型类型上定义泛型方法,因此 Add 是顶层泛型函数而非 Store 的方法。
// name 仅用于 Exec 的错误信息,不参与查表。
func Add[K comparable, V any](s *Store, name string, q Querier[K, V], opts ...Option) Handle[K, V] {
	cfg := Config{}
	for _, o := range opts {
		o(&cfg)
	}
	e := &querierEntry[K, V]{
		q:               q,
		qName:           name,
		shouldIgnoreErr: cfg.IgnoreErr,
		pending:         make(map[K]struct{}),
		results:         make(map[K]V),
	}
	s.runners = append(s.runners, e)
	return Handle[K, V]{entry: e}
}

// Step 追加一个编排步骤。
func (s *Store) Step(f StepFunc) {
	s.steps = append(s.steps, f)
}

// SetStepFuncs 替换全部步骤。
func (s *Store) SetStepFuncs(fs []StepFunc) {
	s.steps = fs
}

// Reset 清空所有 querier 的 pending key 和已缓存结果,使 Store 可被重新使用。
func (s *Store) Reset() {
	for _, r := range s.runners {
		r.reset()
	}
}

// Exec 按 steps 顺序执行:每个 step 先串行跑 StepFunc(用于往各 Querier 收集 key),
// 再并发执行所有 needQuery 的 Querier。steps 为空时返回错误。
//
// 错误语义:任意一个 Querier 查询出错且未设置 IgnoreErr,即返回
// "exec querier <name> err: <err>" 并停止后续 step;出错 querier 的 pending 保留以便重试。
// 设置了 IgnoreErr 的 Querier 出错时,清空其 pending 并继续。
func (s *Store) Exec() error {
	if len(s.steps) == 0 {
		return errors.New("steps is empty")
	}
	for _, step := range s.steps {
		if err := step(); err != nil {
			return err
		}
		var eg errgroup.Group
		for _, r := range s.runners {
			if !r.needQuery() {
				continue
			}
			r := r // go 1.20:循环变量共享,需捕获
			eg.Go(func() error {
				if err := r.run(s.ctx); err != nil && !r.ignoreErr() {
					return fmt.Errorf("exec querier %s err: %w", r.name(), err)
				}
				r.clearPending()
				return nil
			})
		}
		if err := eg.Wait(); err != nil {
			return err
		}
	}
	return nil
}
