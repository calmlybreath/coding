// Package task 提供一个极简的 future/promise:Start 异步跑一个函数,Await 阻塞拿结果。
package task

import "context"

type result struct {
	data interface{}
	err  error
}

type TaskFunc func(ctx context.Context) (interface{}, error)

type Task struct {
	ctx        context.Context
	resultChan chan result
}

// Start 在新 goroutine 里执行 task,并把传入的 ctx 透传给它。
// 注意:TaskFunc 应当尊重 ctx,否则即便调用方取消了 ctx,task 也会跑到自然结束(造成 goroutine 泄漏)。
func Start(ctx context.Context, task TaskFunc) *Task {
	resultChan := make(chan result, 1)
	go func() {
		data, err := task(ctx)
		resultChan <- result{data, err}
		close(resultChan)
	}()
	return &Task{
		ctx:        ctx,
		resultChan: resultChan,
	}
}

// Await 阻塞等待结果:ctx 先结束则返回 ctx.Err(),否则返回 task 的 (data, err)。
// 只能被调用一次:第二次调用 resultChan 已空且关闭,会返回零值 (nil, nil)。
func (t *Task) Await() (interface{}, error) {
	select {
	case <-t.ctx.Done():
		return nil, t.ctx.Err()
	case result := <-t.resultChan:
		return result.data, result.err
	}
}
