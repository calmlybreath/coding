package querier

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync/atomic"
	"testing"
	"time"
)

// demoQuery 把每个 string key 映射成 "demo-<key>"。
var demoQuery QuerierFunc[string, string] = func(ctx context.Context, keys []string) (map[string]string, error) {
	res := make(map[string]string, len(keys))
	for _, k := range keys {
		res[k] = "demo-" + k
	}
	return res, nil
}

// faultQuery 永远失败。
var faultQuery QuerierFunc[int, struct{}] = func(ctx context.Context, keys []int) (map[int]struct{}, error) {
	return nil, errors.New("intentional failure")
}

// newDepQuerier 造一个给 Key2Res 链式依赖用的 querier:Val 前面拼上 prefix。
func newDepQuerier(prefix string) QuerierFunc[Key2Res, Key2Res] {
	return func(ctx context.Context, keys []Key2Res) (map[Key2Res]Key2Res, error) {
		res := make(map[Key2Res]Key2Res, len(keys))
		for _, k := range keys {
			res[k] = Key2Res{Id: k.Id, Val: prefix + k.Val}
		}
		return res, nil
	}
}

type Key2Res struct {
	Id  int
	Val string
}

func TestHandle_Dedup(t *testing.T) {
	t.Run("pending keys deduped", func(t *testing.T) {
		var got []string
		store := NewStore(context.Background())
		h := Add[string, string](store, "demo", QuerierFunc[string, string](func(ctx context.Context, keys []string) (map[string]string, error) {
			got = keys
			res := make(map[string]string, len(keys))
			for _, k := range keys {
				res[k] = k
			}
			return res, nil
		}))
		store.Step(func() error { h.AddKeys([]string{"a", "a", "b"}); return nil })
		if err := store.Exec(); err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 {
			t.Errorf("duplicate keys not filtered, got %d keys: %v", len(got), got)
		}
	})

	t.Run("cached keys skipped", func(t *testing.T) {
		var callCount int32
		store := NewStore(context.Background())
		h := Add[string, string](store, "demo", QuerierFunc[string, string](func(ctx context.Context, keys []string) (map[string]string, error) {
			atomic.AddInt32(&callCount, 1)
			res := make(map[string]string, len(keys))
			for _, k := range keys {
				res[k] = k
			}
			return res, nil
		}))
		store.Step(func() error { h.AddKeys([]string{"a", "b"}); return nil })
		if err := store.Exec(); err != nil {
			t.Fatal(err)
		}
		// 第二步再次加入 "a"(已缓存)+ 新 key "c",应只查 "c"。
		store.Step(func() error { h.AddKeys([]string{"a", "c"}); return nil })
		if err := store.Exec(); err != nil {
			t.Fatal(err)
		}
		if got := atomic.LoadInt32(&callCount); got != 2 {
			t.Errorf("expected 2 queries (cached key skipped), got %d", got)
		}
		if _, ok := h.Load("c"); !ok {
			t.Error("new key c not loaded")
		}
	})
}

func TestStore_Reset(t *testing.T) {
	store := NewStore(context.Background())
	h := Add[string, string](store, "demo", demoQuery)
	store.Step(func() error { h.AddKeys([]string{"a"}); return nil })

	if err := store.Exec(); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.Load("a"); !ok {
		t.Fatal("key a not loaded")
	}

	store.Reset()
	if _, ok := h.Load("a"); ok {
		t.Error("Reset did not clear results")
	}

	// 复用同一组 step 重新跑一次。
	if err := store.Exec(); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.Load("a"); !ok {
		t.Error("re-query after Reset failed")
	}
}

func TestStore_BasicFlow(t *testing.T) {
	store := NewStore(context.Background())
	h := Add[string, string](store, "demo", demoQuery)

	store.SetStepFuncs([]StepFunc{
		// 第一步:加载初始数据
		func() error { h.AddKeys([]string{"a", "b"}); return nil },
		// 第二步:加载补充数据
		func() error { h.AddKey("c"); return nil },
	})

	if err := store.Exec(); err != nil {
		t.Fatalf("Exec failed: %v", err)
	}

	expected := map[string]string{
		"a": "demo-a",
		"b": "demo-b",
		"c": "demo-c",
	}
	for k, v := range expected {
		if res, ok := h.Load(k); !ok || res != v {
			t.Errorf("Key %v: expected %q, got %v", k, v, res)
		}
	}
}

func TestStore_ErrorFlows(t *testing.T) {
	t.Run("Propagate errors", func(t *testing.T) {
		store := NewStore(context.Background())
		h := Add[int, struct{}](store, "fault", faultQuery)

		store.SetStepFuncs([]StepFunc{
			func() error { h.AddKey(1); return nil },
		})

		err := store.Exec()
		if err == nil || err.Error() != "exec querier fault err: intentional failure" {
			t.Errorf("Unexpected error: %v", err)
		}
	})

	t.Run("Ignore errors", func(t *testing.T) {
		store := NewStore(context.Background())
		h := Add[int, struct{}](store, "fault", faultQuery, WithIgnoreErr())

		store.SetStepFuncs([]StepFunc{
			func() error { h.AddKey(1); return nil },
		})

		if err := store.Exec(); err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}
	})
}

func TestStore_MultiStep(t *testing.T) {
	store := NewStore(context.Background())
	h := Add[string, string](store, "demo", demoQuery)

	stepTracker := make([]int, 0)
	store.SetStepFuncs([]StepFunc{
		func() error { stepTracker = append(stepTracker, 1); h.AddKey("step1"); return nil },
		func() error { stepTracker = append(stepTracker, 2); h.AddKey("step2"); return nil },
		func() error { stepTracker = append(stepTracker, 3); return nil },
	})

	if err := store.Exec(); err != nil {
		t.Fatal(err)
	}

	expectedSteps := []int{1, 2, 3}
	if fmt.Sprint(stepTracker) != fmt.Sprint(expectedSteps) {
		t.Errorf("Step execution order mismatch: got %v, want %v", stepTracker, expectedSteps)
	}

	for _, key := range []string{"step1", "step2"} {
		if _, ok := h.Load(key); !ok {
			t.Errorf("Key %q not processed", key)
		}
	}
}

func TestStore_Dependency(t *testing.T) {
	for i := 0; i < 100; i++ {
		store := NewStore(context.Background())
		dep1 := Add[Key2Res, Key2Res](store, "dep1", newDepQuerier("dep1-"))
		dep2 := Add[Key2Res, Key2Res](store, "dep2", newDepQuerier("dep2-"))
		dep3 := Add[Key2Res, Key2Res](store, "dep3", newDepQuerier("dep3-"))

		dep1Keys := make([]Key2Res, 0, 10)
		for j := 0; j < 10; j++ {
			id := rand.Intn(10)
			dep1Keys = append(dep1Keys, Key2Res{Id: id, Val: fmt.Sprint(id)})
		}
		var dep2Keys, dep3Keys []Key2Res

		store.SetStepFuncs([]StepFunc{
			func() error { dep1.AddKeys(dep1Keys); return nil },
			func() error {
				// dep1 的结果直接是 dep2 的 key,无需任何类型断言。
				for _, v := range dep1.BatchLoad(dep1Keys) {
					dep2Keys = append(dep2Keys, v)
				}
				dep2.AddKeys(dep2Keys)
				return nil
			},
			func() error {
				for _, v := range dep2.BatchLoad(dep2Keys) {
					dep3Keys = append(dep3Keys, v)
				}
				dep3.AddKeys(dep3Keys)
				return nil
			},
		})
		if err := store.Exec(); err != nil {
			t.Fatalf("Exec failed: %v", err)
		}

		dep3Vals := dep3.BatchLoad(dep3Keys)
		id2Dep3Val := make(map[int]string)
		for _, v := range dep3Vals {
			id2Dep3Val[v.Id] = v.Val
		}
		for _, k := range dep1Keys {
			want := fmt.Sprintf("dep3-dep2-dep1-%v", k.Val)
			if id2Dep3Val[k.Id] != want {
				t.Errorf("Key %v: expected %q, got %v", k, want, id2Dep3Val[k.Id])
			}
		}
	}
}

// TestStore_Concurrency 验证:同一步内多个 querier 并发执行(峰值 in-flight >= 2),
// 且同一 querier 不会被并发调用。
func TestStore_Concurrency(t *testing.T) {
	var inFlight, peak, sameOverlap int32

	mk := func() QuerierFunc[string, string] {
		var localInFlight int32
		return func(ctx context.Context, keys []string) (map[string]string, error) {
			if atomic.AddInt32(&localInFlight, 1) > 1 {
				atomic.AddInt32(&sameOverlap, 1)
			}
			n := atomic.AddInt32(&inFlight, 1)
			for {
				p := atomic.LoadInt32(&peak)
				if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
			atomic.AddInt32(&inFlight, -1)
			atomic.AddInt32(&localInFlight, -1)

			res := make(map[string]string, len(keys))
			for _, k := range keys {
				res[k] = k
			}
			return res, nil
		}
	}

	store := NewStore(context.Background())
	ha := Add[string, string](store, "a", mk())
	hb := Add[string, string](store, "b", mk())
	store.Step(func() error { ha.AddKeys([]string{"a1"}); hb.AddKeys([]string{"b1"}); return nil })

	if err := store.Exec(); err != nil {
		t.Fatal(err)
	}
	if peak < 2 {
		t.Errorf("queriers did not run concurrently, peak=%d", peak)
	}
	if sameOverlap > 0 {
		t.Errorf("same querier called concurrently, overlap=%d", sameOverlap)
	}
}
