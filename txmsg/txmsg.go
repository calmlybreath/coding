package txmsg

import (
	"context"
	"encoding/gob"
	"sync"
	"time"
)

// BackoffFunc 根据重试次数计算下次检查延迟
type BackoffFunc func(retryCount int) time.Duration

type options struct {
	PollInterval      time.Duration
	BatchSize         int
	MaxRetries        int
	VisibilityTimeout time.Duration
	BackoffFn         BackoffFunc
	AlarmFn           AlarmFunc
	Concurrency       int
	Logger            Logger
}

// Option 功能选项
type Option func(o *options)

// WithPollInterval 设置轮询间隔，默认 1s
func WithPollInterval(d time.Duration) Option {
	return func(o *options) { o.PollInterval = d }
}

// WithBatchSize 设置每次拉取数量，默认 100
func WithBatchSize(n int) Option {
	return func(o *options) { o.BatchSize = n }
}

// WithMaxRetries 设置最大重试次数，默认 10
func WithMaxRetries(n int) Option {
	return func(o *options) { o.MaxRetries = n }
}

// WithVisibilityTimeout 设置认领后可见超时，默认 30s
func WithVisibilityTimeout(d time.Duration) Option {
	return func(o *options) { o.VisibilityTimeout = d }
}

// WithBackoff 设置退避函数，默认线性退避
func WithBackoff(fn BackoffFunc) Option {
	return func(o *options) { o.BackoffFn = fn }
}

// WithAlarmFunc 设置告警回调
func WithAlarmFunc(fn AlarmFunc) Option {
	return func(o *options) { o.AlarmFn = fn }
}

// WithConcurrency 设置并发处理数，默认 1
func WithConcurrency(n int) Option {
	return func(o *options) { o.Concurrency = n }
}

// WithLogger 设置日志
func WithLogger(l Logger) Option {
	return func(o *options) { o.Logger = l }
}

// WithBodyType 注册 Body 的具体类型，gob 编解码使用。须传入非 nil 指针示例。
func WithBodyType(sample interface{}) Option {
	return func(o *options) {
		if sample == nil {
			panic("txmsg: WithBodyType received nil")
		}
		gob.Register(sample)
	}
}

// DefaultBackoff 默认退避函数
func DefaultBackoff(retryCount int) time.Duration {
	d := time.Duration(retryCount) * time.Second
	if d > 60*time.Second {
		d = 60 * time.Second
	}
	return d
}

// TxMsg 事务消息组件
type TxMsg struct {
	store             Store
	topic             string
	handler           Handler
	shardAssigner     ShardAssigner
	pollInterval      time.Duration
	batchSize         int
	maxRetries        int
	visibilityTimeout time.Duration
	backoffFn         BackoffFunc
	alarmFn           AlarmFunc
	concurrency       int
	logger            Logger

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New 创建事务消息组件。store / handler 不能为 nil。
func New(store Store, assigner ShardAssigner, handler Handler, topic string, opts ...Option) *TxMsg {
	if store == nil {
		panic("txmsg: store is nil")
	}
	if handler == nil {
		panic("txmsg: handler is nil")
	}
	o := options{
		PollInterval:      1 * time.Second,
		BatchSize:         100,
		MaxRetries:        10,
		VisibilityTimeout: 30 * time.Second,
		BackoffFn:         DefaultBackoff,
		Concurrency:       1,
		Logger:            &NopLogger{},
	}
	for _, f := range opts {
		f(&o)
	}
	if o.PollInterval <= 0 {
		panic("txmsg: PollInterval must be > 0")
	}
	if o.BatchSize <= 0 {
		panic("txmsg: BatchSize must be > 0")
	}
	if o.MaxRetries <= 0 {
		panic("txmsg: MaxRetries must be > 0")
	}
	if o.VisibilityTimeout <= 0 {
		panic("txmsg: VisibilityTimeout must be > 0")
	}
	if o.Concurrency <= 0 {
		o.Concurrency = 1
	}
	if assigner == nil {
		assigner = &allShardAssigner{}
	}
	return &TxMsg{
		store:             store,
		topic:             topic,
		handler:           handler,
		shardAssigner:     assigner,
		pollInterval:      o.PollInterval,
		batchSize:         o.BatchSize,
		maxRetries:        o.MaxRetries,
		visibilityTimeout: o.VisibilityTimeout,
		backoffFn:         o.BackoffFn,
		alarmFn:           o.AlarmFn,
		concurrency:       o.Concurrency,
		logger:            o.Logger,
	}
}

// Add 预写消息到队列
func (t *TxMsg) Add(ctx context.Context, body interface{}, deliverAfter time.Duration) (*Message, error) {
	msg := NewMessage(body)
	if err := t.store.Add(ctx, t.topic, msg, deliverAfter); err != nil {
		return nil, err
	}
	return msg, nil
}

// Remove 业务主动删除消息
func (t *TxMsg) Remove(ctx context.Context, msg *Message) error {
	return t.store.Remove(ctx, t.topic, msg)
}

// Start 启动后台轮询
func (t *TxMsg) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	t.cancel = cancel
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		t.pollLoop(ctx)
	}()
}

// Close 停止后台轮询并等待退出
func (t *TxMsg) Close() {
	if t.cancel != nil {
		t.cancel()
	}
	t.wg.Wait()
}

func (t *TxMsg) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(t.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			t.pollOnce(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (t *TxMsg) pollOnce(ctx context.Context) {
	shards := t.shardAssigner.Shards(t.topic)
	msgs, err := t.store.Poll(ctx, t.topic, shards, t.batchSize, t.visibilityTimeout)
	if err != nil {
		t.logger.Errorf("poll topic %s: %v", t.topic, err)
		return
	}
	if len(msgs) == 0 {
		return
	}

	sem := make(chan struct{}, t.concurrency)
	var wg sync.WaitGroup
	for _, msg := range msgs {
		wg.Add(1)
		sem <- struct{}{}
		go func(m *Message) {
			defer wg.Done()
			defer func() { <-sem }()
			t.processOne(ctx, m)
		}(msg)
	}
	wg.Wait()
}

func (t *TxMsg) processOne(ctx context.Context, msg *Message) {
	resolved, err := t.handler.Handle(ctx, msg)
	if resolved {
		if err := t.store.Remove(ctx, t.topic, msg); err != nil {
			t.logger.Errorf("remove topic %s msg %s: %v", t.topic, msg.ID, err)
		}
		return
	}

	msg.RetryCount++
	if msg.RetryCount >= t.maxRetries {
		t.logger.Errorf("topic %s msg %s reached max retries %d, err: %v", t.topic, msg.ID, t.maxRetries, err)
		if t.alarmFn != nil {
			t.alarmFn(ctx, msg)
		}
		if err := t.store.Remove(ctx, t.topic, msg); err != nil {
			t.logger.Errorf("remove topic %s msg %s: %v", t.topic, msg.ID, err)
		}
		return
	}

	backoff := t.backoffFn(msg.RetryCount)
	t.logger.Debugf("reschedule topic %s msg %s retry %d after %v", t.topic, msg.ID, msg.RetryCount, backoff)
	if err := t.store.Reschedule(ctx, t.topic, msg, backoff); err != nil {
		t.logger.Errorf("reschedule topic %s msg %s: %v", t.topic, msg.ID, err)
	}
}

type allShardAssigner struct{}

func (a *allShardAssigner) Shards(topic string) []int { return nil }
