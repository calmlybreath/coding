package agg

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"
)

const realFetchDelay = time.Millisecond // testMGet 模拟的下游拉取延迟

// GetRes 是测试用的"真实结果":每个 roomId 对应一个 RoomInfo。
func GetRes(roomId string) RoomInfo {
	return RoomInfo{
		Content: fmt.Sprintf("task_%s", roomId),
	}
}

// GenRoomIdN 生成 n 个随机 roomId(可能有重复),模拟一批 key。
func GenRoomIdN(n int) []interface{} {
	roomIds := make([]interface{}, 0, n)
	for i := 0; i < n; i++ {
		roomId := rand.Intn(10000)
		roomIds = append(roomIds, fmt.Sprintf("room_%d", roomId))
	}
	return roomIds
}

// genUniqueKeys 生成 n 个互不相同的 key,用于需要按数量断言结果的测试。
func genUniqueKeys(n int, prefix string) []interface{} {
	keys := make([]interface{}, n)
	for i := 0; i < n; i++ {
		keys[i] = fmt.Sprintf("%s_%d", prefix, i)
	}
	return keys
}

type RoomInfo struct {
	Content string
}

// testMGet 模拟下游批量查询:返回每个 key 对应的 RoomInfo,并人为延迟一下。
func testMGet(ctx context.Context, roomIds []interface{}) (map[interface{}]interface{}, error) {
	roomId2Info := make(map[interface{}]interface{})
	for _, roomId := range roomIds {
		roomId2Info[roomId] = GetRes(roomId.(string))
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(realFetchDelay):
		return roomId2Info, nil
	}
}

// TestAggBasic:并发 SubmitAndWait,验证结果正确(同时给 -race 跑并发场景)。
func TestAggBasic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := NewDefaultConfig()
	cfg.MGetTimeoutMs = 200
	cfg.MaxWaitMs = 2
	agg, err := NewAggregator(ctx, Params{ID: "testAgg", Config: cfg, MGetFn: testMGet})
	if err != nil {
		t.Fatalf("NewAggregator err: %v", err)
	}

	const goroutines = 50
	const keysPerCall = 10
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			keys := GenRoomIdN(keysPerCall)
			res, err := agg.SubmitAndWait(ctx, keys)
			if err != nil {
				t.Errorf("SubmitAndWait err: %v", err)
				return
			}
			for _, k := range keys {
				r, ok := res[k]
				if !ok {
					t.Errorf("key %v missing in result", k)
					return
				}
				if r.(RoomInfo) != GetRes(k.(string)) {
					t.Errorf("key %v res mismatch: got %+v", k, r)
				}
			}
		}()
	}
	close(start)
	wg.Wait()

	stat := agg.Stat()
	t.Logf("stat: %+v", stat)
	if stat.CallApiNum == 0 {
		t.Error("expected some api calls")
	}
}

// TestAggTimeoutMerge:回归测试。每个 key 单独提交,形成 n 个 chunk,
// 让它们全部走超时路径(getTimeoutTasks)。修复前:每次 tick 只删 1 个 chunk → n 次 API 调用,
// 完全不聚合;修复后:所有超时 chunk 合并成 1(或极少数)次 API 调用。
func TestAggTimeoutMerge(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := NewDefaultConfig()
	cfg.MaxWaitMs = 30      // 等待 30ms 才超时
	cfg.MinBatchSize = 1000 // 设大,确保只走超时路径,不走 tryGetTasks
	cfg.MaxBatchSize = 2000
	cfg.MGetTimeoutMs = 200
	agg, err := NewAggregator(ctx, Params{ID: "timeoutMerge", Config: cfg, MGetFn: testMGet})
	if err != nil {
		t.Fatal(err)
	}

	const n = 20
	keys := make([]interface{}, n)
	for i := range keys {
		keys[i] = fmt.Sprintf("merge_%d", i)
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, k := range keys {
		wg.Add(1)
		go func(k interface{}) {
			defer wg.Done()
			<-start
			res, err := agg.SubmitAndWait(ctx, []interface{}{k})
			if err != nil {
				t.Errorf("SubmitAndWait key=%v err: %v", k, err)
				return
			}
			if r, ok := res[k]; !ok || r.(RoomInfo) != GetRes(k.(string)) {
				t.Errorf("key %v res wrong: %+v", k, res[k])
			}
		}(k)
	}
	close(start)
	wg.Wait()

	stat := agg.Stat()
	t.Logf("stat: %+v", stat)
	// 修复前 CallApiNum == n(每个 chunk 单独一次);修复后应远小于 n。
	if stat.CallApiNum >= n {
		t.Errorf("timeout path did not merge: callApiNum=%d, n=%d", stat.CallApiNum, n)
	}
}

// TestAggAutoSplit:keys 数量超过 MaxBatchSize 时,SubmitAndWait 应自动分片,调用方无感。
func TestAggAutoSplit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := NewDefaultConfig()
	cfg.MaxBatchSize = 5
	cfg.MinBatchSize = 5
	cfg.MaxWaitMs = 5
	cfg.MGetTimeoutMs = 200
	agg, err := NewAggregator(ctx, Params{Config: cfg, MGetFn: testMGet})
	if err != nil {
		t.Fatal(err)
	}

	const n = 23 // > MaxBatchSize(5)
	keys := make([]interface{}, n)
	for i := range keys {
		keys[i] = fmt.Sprintf("split_%d", i)
	}
	res, err := agg.SubmitAndWait(ctx, keys)
	if err != nil {
		t.Fatalf("SubmitAndWait err: %v", err)
	}
	if len(res) != n {
		t.Fatalf("expected %d results, got %d", n, len(res))
	}
	for _, k := range keys {
		r, ok := res[k]
		if !ok || r.(RoomInfo) != GetRes(k.(string)) {
			t.Errorf("key %v missing/wrong: %+v", k, r)
		}
	}
}

// TestAggSubmitOverMax:直接 Submit 超过 MaxBatchSize 的 task 应报错(SubmitAndWait 才会自动分片)。
func TestAggSubmitOverMax(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := NewDefaultConfig()
	cfg.MinBatchSize = 1
	cfg.MaxBatchSize = 3
	agg, err := NewAggregator(ctx, Params{Config: cfg, MGetFn: testMGet})
	if err != nil {
		t.Fatal(err)
	}

	tasks := make([]Task, 0, 5)
	for i := 0; i < 5; i++ {
		tasks = append(tasks, Task{
			TaskID: fmt.Sprintf("t_%d", i),
			Key:    i,
			ResCh:  make(chan interface{}, 1),
			ErrCh:  make(chan error, 1),
		})
	}
	err = agg.Submit(ctx, tasks)
	if err == nil {
		t.Fatal("expected error when Submit > MaxBatchSize, got nil")
	}
}

// TestAggStatConcurrent:并发 SubmitAndWait + Stat,验证无数据竞争、不 panic。
func TestAggStatConcurrent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	agg, err := NewAggregator(ctx, Params{Config: NewDefaultConfig(), MGetFn: testMGet})
	if err != nil {
		t.Fatal(err)
	}

	var submitWg sync.WaitGroup
	for i := 0; i < 10; i++ {
		submitWg.Add(1)
		go func(i int) {
			defer submitWg.Done()
			for j := 0; j < 30; j++ {
				_, err := agg.SubmitAndWait(ctx, []interface{}{fmt.Sprintf("k_%d_%d", i, j)})
				if err != nil && !errors.Is(err, context.Canceled) {
					t.Errorf("SubmitAndWait err: %v", err)
					return
				}
			}
		}(i)
	}

	// 一个 goroutine 持续读 Stat,制造与 run() 写入的并发,验证无竞争。
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
				_ = agg.Stat()
			}
		}
	}()

	submitWg.Wait()
	close(stop)
	<-done

	stat := agg.Stat()
	t.Logf("final stat: %+v", stat)
}
