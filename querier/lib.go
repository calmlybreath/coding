package querier

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/sync/errgroup"
)

// Querier 是单个数据源的批量查询抽象。嵌入 BaseQuerier 后只需实现 Query。
type Querier interface {
	AddKeys(keys []interface{}, ignoreErr bool)
	Load(key interface{}) (interface{}, bool)
	BatchLoad(keys []interface{}) map[interface{}]interface{}
	NeedQuery() bool
	ClearPendingKeys()
	IgnoreErr() bool

	Query(ctx context.Context) error // 执行加载,继承 BaseQuerier 的只需实现这个方法
}

// BaseQuerier 提供 key 收集/去重/结果存取的通用实现,具体 Querier 嵌入它后只需实现 Query。
type BaseQuerier struct {
	pendingKeys map[interface{}]struct{}
	key2Res     map[interface{}]interface{}
	ignoreErr   bool
}

func (b *BaseQuerier) AddKeys(keys []interface{}, ignoreErr bool) {
	if b.pendingKeys == nil {
		b.pendingKeys = make(map[interface{}]struct{})
	}
	if b.key2Res == nil {
		b.key2Res = make(map[interface{}]interface{})
	}
	for _, key := range keys {
		if _, ok := b.key2Res[key]; ok {
			continue
		}
		b.pendingKeys[key] = struct{}{}
	}
	b.ignoreErr = ignoreErr
}

func (b *BaseQuerier) Load(key interface{}) (interface{}, bool) {
	if b.key2Res == nil {
		return nil, false
	}
	res, ok := b.key2Res[key]
	return res, ok
}

func (b *BaseQuerier) BatchLoad(keys []interface{}) map[interface{}]interface{} {
	res := make(map[interface{}]interface{})
	for _, key := range keys {
		if r, ok := b.key2Res[key]; ok {
			res[key] = r
		}
	}
	return res
}

func (b *BaseQuerier) NeedQuery() bool {
	return len(b.pendingKeys) > 0
}

func (b *BaseQuerier) IgnoreErr() bool {
	return b.ignoreErr
}

func (b *BaseQuerier) ClearPendingKeys() {
	b.pendingKeys = nil
}

// StepFunc 是一个编排步骤:通常用来向各 Querier 收集 key,可在其中用 BatchLoad 取上一步结果。
type StepFunc func(s *Store) error

// Store 管理多个 Querier,按 StepFunc 顺序编排它们的批量查询。
type Store struct {
	ctx       context.Context
	queriers  map[string]Querier
	stepFuncs []StepFunc
}

// NewStore 创建一个空的 Store。
func NewStore(ctx context.Context) *Store {
	return &Store{
		ctx:      ctx,
		queriers: make(map[string]Querier),
	}
}

func (s *Store) AddQuerier(name string, q Querier) {
	s.queriers[name] = q
}

func (s *Store) SetStepFuncs(stepFuncs []StepFunc) {
	s.stepFuncs = stepFuncs
}

func (s *Store) Load(name string, key interface{}) (interface{}, bool) {
	q, ok := s.queriers[name]
	if !ok {
		return nil, false
	}
	return q.Load(key)
}

func (s *Store) BatchLoad(name string, keys []interface{}) map[interface{}]interface{} {
	q, ok := s.queriers[name]
	if !ok {
		return nil
	}
	return q.BatchLoad(keys)
}

func (s *Store) AddKey(name string, key interface{}, ignoreErr bool) {
	if q, ok := s.queriers[name]; ok {
		q.AddKeys([]interface{}{key}, ignoreErr)
	}
}

func (s *Store) AddKeys(name string, keys []interface{}, ignoreErr bool) {
	if q, ok := s.queriers[name]; ok {
		q.AddKeys(keys, ignoreErr)
	}
}

// Exec 按 stepFuncs 顺序执行:每个 step 先串行跑 StepFunc(用于往各 Querier 收集 key),
// 再并发执行所有 NeedQuery 的 Querier。stepFuncs 为空时返回错误,而不是 panic。
func (s *Store) Exec() error {
	if len(s.stepFuncs) == 0 {
		return errors.New("stepFuncs is empty")
	}
	for idx := 0; idx < len(s.stepFuncs); idx++ {
		if err := s.stepFuncs[idx](s); err != nil {
			return err
		}
		// 并发执行所有需要查询的 querier
		wg := errgroup.Group{}
		for tmpName, tmpQuerier := range s.queriers {
			if !tmpQuerier.NeedQuery() {
				continue
			}
			name := tmpName
			q := tmpQuerier
			wg.Go(func() error {
				execErr := q.Query(s.ctx)
				if execErr != nil && !q.IgnoreErr() {
					return fmt.Errorf("exec querier %s err: %w", name, execErr)
				}
				q.ClearPendingKeys()
				return nil
			})
		}
		if waitErr := wg.Wait(); waitErr != nil {
			return waitErr
		}
	}
	return nil
}
