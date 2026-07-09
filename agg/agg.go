package agg

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"math/rand"
	"runtime"
	"sync/atomic"
	"time"
)

// ErrClosed 在 Aggregator 关闭后返回(目前仅作语义占位)。
var ErrClosed = errors.New("aggregator closed")

// MGetter 是下游批量查询的抽象:给定一批 key,返回 key->结果。
type MGetter interface {
	MGet(ctx context.Context, keys []interface{}) (map[interface{}]interface{}, error)
}

// MGetFn 是 MGetter 的函数形式,构建 Aggregator 时与 MGetImpl 二选一。
type MGetFn func(ctx context.Context, keys []interface{}) (map[interface{}]interface{}, error)

// LogFn 是日志回调,签名同 fmt.Printf。
type LogFn func(format string, v ...interface{})

// Task 是一次查询请求:提交后通过 ResCh 拿结果、ErrCh 拿错误。
type Task struct {
	TaskID string           // 任务 id
	Key    interface{}      // 查询 key
	ResCh  chan interface{} // 结果 channel
	ErrCh  chan error       // 错误 channel
}

// Config 是 Aggregator 的配置。默认值由 NewDefaultConfig 给出。
// 不写 default struct tag:没有反射读取它,写了反而会和真实默认值不一致、产生误导。
type Config struct {
	ChanSize             int   // 分发器 channel 大小
	MaxWaitMs            int64 // 任务在队列里的最大等待时长,time.Millisecond
	MinBatchSize         int   // 最小批量大小:攒到这么多 unique key 就立即触发一次批量查询
	MaxBatchSize         int   // 最大批量大小:单次 Submit 的 key 数上限,SubmitAndWait 会自动按它分片
	IgnoreKeyResNotExist bool  // 查询结果里不存在的 key,是否当 nil 返回而非报错
	MGetTimeoutMs        int64 // 单次批量查询的超时时间,time.Millisecond
}

func (c *Config) Validate() error {
	if c.ChanSize <= 0 {
		return fmt.Errorf("ChanSize must > 0")
	}
	if c.MaxWaitMs <= 0 {
		return fmt.Errorf("MaxWaitMs must > 0")
	}
	if c.MinBatchSize <= 0 {
		return fmt.Errorf("MinBatchSize must > 0")
	}
	if c.MaxBatchSize <= 0 {
		return fmt.Errorf("MaxBatchSize must > 0")
	}
	if c.MaxBatchSize < c.MinBatchSize {
		return fmt.Errorf("MaxBatchSize must >= MinBatchSize")
	}
	if c.MGetTimeoutMs <= 0 {
		return fmt.Errorf("MGetTimeoutMs must > 0")
	}
	return nil
}

// NewDefaultConfig 返回一份常用默认配置。
func NewDefaultConfig() Config {
	return Config{
		ChanSize:             500,
		MaxWaitMs:            2,
		MinBatchSize:         20,
		MaxBatchSize:         30,
		MGetTimeoutMs:        300,
		IgnoreKeyResNotExist: true,
	}
}

// TaskChunk 是一次 Submit 投递进来的任务块。
type TaskChunk struct {
	SubmitTime time.Time // 提交时间
	Tasks      []Task
}

// aggStat 是 Aggregator 的内部计数器:只在 run() goroutine 写(atomic)、Stat() 原子读。
type aggStat struct {
	SubmitTaskChunkNum    int64 // 提交的 task chunk 数量
	RecvTaskChunkNum      int64 // 接收的 task chunk 数量
	ProcessTaskChunkNum   int64 // 处理的 task chunk 数量
	TotalDelay            int64 // 总延迟(从提交到执行),microsecond
	TotalRecvDelay        int64 // 总接收延迟(从提交 channel 到接收),microsecond
	MergeNum              int64 // 合并 key 的数量
	TotalCallApiNum       int64 // 调用下游 api 的数量
	TotalReduceCallApiNum int64 // 相比不聚合减少的 api 调用数量
	TimeoutTaskChunkNum   int64 // 超时触发的 task chunk 数量
	TimeoutTickerNum      int64 // 超时 ticker 触发次数
}

// Stats 是 Stat() 返回的统计快照。
type Stats struct {
	GoNum               int64
	SubmitTaskChunkNum  int64
	RecvTaskChunkNum    int64
	ProcessTaskChunkNum int64
	AvgDelayMs          float64 // 平均总延迟,毫秒
	AvgRecvDelayMs      float64 // 平均接收延迟,毫秒
	MergeNum            int64
	CallApiNum          int64
	ReduceCallApiNum    int64
	TimeoutTaskChunkNum int64
	TimeoutTickerNum    int64
}

// Agg 是 Aggregator 对外暴露的最小接口(仅 SubmitAndWait),便于解耦和 mock。
type Agg interface {
	SubmitAndWait(ctx context.Context, keys []interface{}) (map[interface{}]interface{}, error)
}

// Aggregator 把短时间内的多个 mget 请求按时间窗口攒成一批打下游,降低下游 QPS。
type Aggregator struct {
	ID       string // id
	ctx      context.Context
	config   Config
	errorLog LogFn
	debugLog LogFn

	// mgetFn / mgetImpl 二选一
	mgetFn   MGetFn  // 批量查询函数
	mgetImpl MGetter // 批量查询接口

	taskChunkCh         chan TaskChunk // 任务 channel
	clearStatCh         chan struct{}
	taskChunkQueue      *list.List // 等待中的任务队列,elem 是 TaskChunk
	taskNum             int        // 队列里的任务总数
	lastRecvTaskChunkTs int64      // 最近一次接收 task chunk 的时间戳(unix 秒)
	isClosed            bool

	stat aggStat
}

// Params 是构建 Aggregator 的参数。
type Params struct {
	Config Config
	// MGetFn / MGetImpl 二选一
	MGetFn   MGetFn  // 批量查询函数
	MGetImpl MGetter // 批量查询接口

	// 可选
	ID       string
	ErrorLog LogFn
	DebugLog LogFn
}

func (p *Params) Validate() error {
	if p.MGetFn == nil && p.MGetImpl == nil {
		return errors.New("MGetFn or MGetImpl must be set")
	}
	if p.MGetFn != nil && p.MGetImpl != nil {
		return errors.New("MGetFn and MGetImpl can not be set at the same time")
	}
	if err := p.Config.Validate(); err != nil {
		return fmt.Errorf("config validate error: %w", err)
	}
	return nil
}

// NewAggregator 创建并启动一个 Aggregator。
func NewAggregator(ctx context.Context, params Params) (*Aggregator, error) {
	if err := params.Validate(); err != nil {
		return nil, fmt.Errorf("params validate error: %w", err)
	}
	a := &Aggregator{
		ctx:            ctx,
		ID:             params.ID,
		config:         params.Config,
		errorLog:       params.ErrorLog,
		debugLog:       params.DebugLog,
		mgetFn:         params.MGetFn,
		mgetImpl:       params.MGetImpl,
		taskChunkCh:    make(chan TaskChunk, params.Config.ChanSize),
		taskChunkQueue: list.New(),
		clearStatCh:    make(chan struct{}, 1),
	}

	go a.run()

	return a, nil
}

// SubmitAndWait 提交 keys 并阻塞等待全部结果。
// 若 keys 数量超过 MaxBatchSize,会自动按 MaxBatchSize 分片多次提交,调用方无需自行拆分。
//
// 失败语义:任意一个 key 的查询出错(或 ctx 取消)即立即返回该错误,
// 其余尚未拿到结果的 key 会被丢弃 —— 即"整批成功或整批失败",不会返回部分结果。
func (a *Aggregator) SubmitAndWait(ctx context.Context, keys []interface{}) (map[interface{}]interface{}, error) {
	if len(keys) == 0 {
		return map[interface{}]interface{}{}, nil
	}
	maxBatch := a.config.MaxBatchSize
	key2Res := make(map[interface{}]interface{}, len(keys))
	for start := 0; start < len(keys); start += maxBatch {
		end := start + maxBatch
		if end > len(keys) {
			end = len(keys)
		}
		batch := keys[start:end]
		taskList := make([]Task, 0, len(batch))
		for _, key := range batch {
			taskList = append(taskList, Task{
				TaskID: genTaskID(),
				Key:    key,
				ResCh:  make(chan interface{}, 1),
				ErrCh:  make(chan error, 1),
			})
		}
		if err := a.Submit(ctx, taskList); err != nil {
			return nil, err
		}
		for _, task := range taskList {
			select {
			case res := <-task.ResCh:
				if res != nil {
					key2Res[task.Key] = res
				}
			case err := <-task.ErrCh:
				return nil, err
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	return key2Res, nil
}

// Stat 返回统计快照。所有字段均通过原子读获取,可安全地在其它 goroutine 调用,
// 不会与 run() 的写入产生数据竞争。
func (a *Aggregator) Stat() Stats {
	recvTaskChunkNum := atomic.LoadInt64(&a.stat.RecvTaskChunkNum)
	processTaskChunkNum := atomic.LoadInt64(&a.stat.ProcessTaskChunkNum)
	totalDelay := atomic.LoadInt64(&a.stat.TotalDelay)
	totalRecvDelay := atomic.LoadInt64(&a.stat.TotalRecvDelay)

	var avgDelayMs float64
	if processTaskChunkNum != 0 {
		avgDelayMs = (float64(totalDelay) / float64(1000)) / float64(processTaskChunkNum)
	}
	var avgRecvDelayMs float64
	if recvTaskChunkNum != 0 {
		avgRecvDelayMs = (float64(totalRecvDelay) / float64(1000)) / float64(recvTaskChunkNum)
	}

	return Stats{
		GoNum:               int64(runtime.NumGoroutine()),
		SubmitTaskChunkNum:  atomic.LoadInt64(&a.stat.SubmitTaskChunkNum),
		RecvTaskChunkNum:    recvTaskChunkNum,
		ProcessTaskChunkNum: processTaskChunkNum,
		MergeNum:            atomic.LoadInt64(&a.stat.MergeNum),
		CallApiNum:          atomic.LoadInt64(&a.stat.TotalCallApiNum),
		ReduceCallApiNum:    atomic.LoadInt64(&a.stat.TotalReduceCallApiNum),
		AvgDelayMs:          avgDelayMs,
		AvgRecvDelayMs:      avgRecvDelayMs,
		TimeoutTaskChunkNum: atomic.LoadInt64(&a.stat.TimeoutTaskChunkNum),
		TimeoutTickerNum:    atomic.LoadInt64(&a.stat.TimeoutTickerNum),
	}
}

func (a *Aggregator) logError(format string, v ...interface{}) {
	if a.errorLog != nil {
		a.errorLog(format, v...)
	}
}

func (a *Aggregator) logDebug(format string, v ...interface{}) {
	if a.debugLog != nil {
		a.debugLog(format, v...)
	}
}

// Submit 把一批 Task 投递到聚合队列。单次提交的 task 数不能超过 MaxBatchSize,
// 否则报错(需要更大批量请用 SubmitAndWait,它会自动分片)。
func (a *Aggregator) Submit(ctx context.Context, tasks []Task) error {
	submitTime := time.Now()
	taskLen := len(tasks)
	if taskLen == 0 {
		return nil
	}
	if taskLen > a.config.MaxBatchSize {
		a.logError("taskLen > MaxBatchSize, taskLen=%d, MaxBatchSize=%d", taskLen, a.config.MaxBatchSize)
		return fmt.Errorf("taskLen > MaxBatchSize, taskLen=%d, MaxBatchSize=%d", taskLen, a.config.MaxBatchSize)
	}
	chunk := TaskChunk{
		SubmitTime: submitTime,
		Tasks:      tasks,
	}
	atomic.AddInt64(&a.stat.SubmitTaskChunkNum, 1)
	select {
	case <-ctx.Done():
		return fmt.Errorf("submit err=%w", ctx.Err())
	case a.taskChunkCh <- chunk:
		return nil
	}
}

func (a *Aggregator) getTimeoutTasks() (map[interface{}][]Task, bool) {
	if a.taskChunkQueue.Len() == 0 {
		return nil, false
	}
	key2Tasks := make(map[interface{}][]Task)
	now := time.Now()
	elemNum := 0
	// 注意:container/list 的 Remove 会把 el.next 置 nil,所以必须先保存 next 再 Remove,
	// 否则 for 的 el = el.Next() 会拿到 nil,一次循环只删一个 chunk(曾经的 bug:
	// 超时路径完全不聚合,每个超时 chunk 单独发一次下游调用)。
	var next *list.Element
	for el := a.taskChunkQueue.Front(); el != nil; el = next {
		next = el.Next()
		taskChunk := el.Value.(TaskChunk)
		// 是否 timeout
		if now.Sub(taskChunk.SubmitTime) < time.Duration(a.config.MaxWaitMs)*time.Millisecond {
			break
		}
		for _, t := range taskChunk.Tasks {
			if _, ok := key2Tasks[t.Key]; !ok {
				key2Tasks[t.Key] = []Task{t}
			} else {
				atomic.AddInt64(&a.stat.MergeNum, 1)
				key2Tasks[t.Key] = append(key2Tasks[t.Key], t)
			}
		}
		a.taskNum -= len(taskChunk.Tasks)
		atomic.AddInt64(&a.stat.ProcessTaskChunkNum, 1)
		atomic.AddInt64(&a.stat.TimeoutTaskChunkNum, 1)
		atomic.AddInt64(&a.stat.TotalDelay, now.Sub(taskChunk.SubmitTime).Microseconds())
		elemNum++
		a.taskChunkQueue.Remove(el)
	}
	if len(key2Tasks) == 0 {
		return nil, false
	}

	atomic.AddInt64(&a.stat.TotalReduceCallApiNum, int64(elemNum)-1)
	atomic.AddInt64(&a.stat.TotalCallApiNum, 1)
	return key2Tasks, true
}

func (a *Aggregator) tryGetTasks() (map[interface{}][]Task, bool) {
	// 如果 taskNum 没达到最小批量,也没必要往下遍历
	if a.taskNum < a.config.MinBatchSize {
		return nil, false
	}
	reachedMinSize := false // 是否达到最小批量大小
	elems := make([]*list.Element, 0, a.taskChunkQueue.Len())

	visitedKeys := make(map[interface{}]struct{})
	for el := a.taskChunkQueue.Front(); el != nil; el = el.Next() {
		taskChunk := el.Value.(TaskChunk)
		for _, t := range taskChunk.Tasks {
			visitedKeys[t.Key] = struct{}{}
		}
		curNum := len(visitedKeys)
		if curNum >= a.config.MinBatchSize {
			reachedMinSize = true
			if curNum > a.config.MaxBatchSize {
				// 本次遍历的 elem 不能加进去,否则超过最大批量
				break
			}
			elems = append(elems, el)
			break
		}
		elems = append(elems, el)
	}
	if !reachedMinSize {
		return nil, false
	}

	key2Tasks := make(map[interface{}][]Task)
	now := time.Now()
	for _, el := range elems {
		taskChunk := el.Value.(TaskChunk)
		for _, t := range taskChunk.Tasks {
			if _, ok := key2Tasks[t.Key]; !ok {
				key2Tasks[t.Key] = []Task{t}
			} else {
				atomic.AddInt64(&a.stat.MergeNum, 1)
				key2Tasks[t.Key] = append(key2Tasks[t.Key], t)
			}
		}
		a.taskNum -= len(taskChunk.Tasks)
		atomic.AddInt64(&a.stat.ProcessTaskChunkNum, 1)
		atomic.AddInt64(&a.stat.TotalDelay, now.Sub(taskChunk.SubmitTime).Microseconds())
		a.taskChunkQueue.Remove(el)
	}

	atomic.AddInt64(&a.stat.TotalReduceCallApiNum, int64(len(elems)-1))
	atomic.AddInt64(&a.stat.TotalCallApiNum, 1)
	return key2Tasks, true
}

// run 是 Aggregator 的主循环:接收任务、攒批、超时兜底。同一批 task 不会被拆分。
func (a *Aggregator) run() {
	timeoutTicker := time.NewTicker(time.Millisecond)
	exitTicker := time.NewTicker(10 * time.Second)
	for {
		select {
		case taskChunk := <-a.taskChunkCh:
			now := time.Now()
			a.lastRecvTaskChunkTs = now.Unix()
			atomic.AddInt64(&a.stat.RecvTaskChunkNum, 1)
			atomic.AddInt64(&a.stat.TotalRecvDelay, now.Sub(taskChunk.SubmitTime).Microseconds())
			a.taskChunkQueue.PushBack(taskChunk)
			a.taskNum += len(taskChunk.Tasks)
			key2Tasks, get := a.tryGetTasks()
			if get {
				go a.process(a.ctx, key2Tasks)
				a.logDebug("id=%v, fetch taskChunk by tasksCh", a.ID)
			}
		case <-timeoutTicker.C:
			atomic.AddInt64(&a.stat.TimeoutTickerNum, 1)
			key2Tasks, exist := a.getTimeoutTasks()
			if exist {
				go a.process(a.ctx, key2Tasks)
				a.logDebug("id=%v, fetch taskChunk by ticker", a.ID)
			}
		case <-a.ctx.Done():
			if !a.isClosed {
				a.logError("id=%v, ctx done", a.ID)
				a.isClosed = true
			}
		case <-exitTicker.C:
			// 保证所有任务处理完后再退出:ctx 关闭 + 10s 内没有新任务进来
			if a.isClosed && a.lastRecvTaskChunkTs+10 < time.Now().Unix() {
				a.logError("id=%v, exitTicker done", a.ID)
				return
			}
		case <-a.clearStatCh:
			atomic.StoreInt64(&a.stat.SubmitTaskChunkNum, 0)
			atomic.StoreInt64(&a.stat.RecvTaskChunkNum, 0)
			atomic.StoreInt64(&a.stat.ProcessTaskChunkNum, 0)
			atomic.StoreInt64(&a.stat.TotalDelay, 0)
			atomic.StoreInt64(&a.stat.TotalRecvDelay, 0)
			atomic.StoreInt64(&a.stat.MergeNum, 0)
			atomic.StoreInt64(&a.stat.TotalCallApiNum, 0)
			atomic.StoreInt64(&a.stat.TotalReduceCallApiNum, 0)
			atomic.StoreInt64(&a.stat.TimeoutTaskChunkNum, 0)
			atomic.StoreInt64(&a.stat.TimeoutTickerNum, 0)
		}
	}
}

func (a *Aggregator) process(ctx context.Context, key2Tasks map[interface{}][]Task) {
	keys := make([]interface{}, 0, len(key2Tasks))
	for k := range key2Tasks {
		keys = append(keys, k)
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(a.config.MGetTimeoutMs)*time.Millisecond)
	defer cancel()

	var err error
	var key2Info map[interface{}]interface{}
	if a.mgetFn != nil {
		key2Info, err = a.mgetFn(timeoutCtx, keys)
	} else {
		key2Info, err = a.mgetImpl.MGet(timeoutCtx, keys)
	}
	if err != nil {
		for _, tasks := range key2Tasks {
			for _, task := range tasks {
				task.ErrCh <- fmt.Errorf("id=%v fetch err=%w", a.ID, err)
				close(task.ErrCh)
			}
		}
		return
	}
	for key, tasks := range key2Tasks {
		info, ok := key2Info[key]
		if !ok {
			for _, task := range tasks {
				if !a.config.IgnoreKeyResNotExist {
					task.ErrCh <- fmt.Errorf("id=%v, not found key: %s", a.ID, key)
					close(task.ErrCh)
				} else {
					task.ResCh <- nil
					close(task.ResCh)
				}
			}
			continue
		}
		for _, task := range tasks {
			task.ResCh <- info
			close(task.ResCh)
		}
	}
}

func genTaskID() string {
	// 时间戳 + 随机数 + 随机数
	return fmt.Sprintf("%d_%d_%d", time.Now().UnixNano(), rand.Intn(1000), rand.Intn(1000))
}
