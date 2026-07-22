package txmsg_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
	"tools/txmsg"
	"tools/txmsg/store/memory"
)

type testData struct{ S string }
type orderData struct {
	OrderID string
	Amount  int64
	Paid    bool
}
type paymentData struct {
	TxID   string
	UserID int64
	Status int32
}

type testHandler struct {
	handleFn func(ctx context.Context, msg *txmsg.Message) (bool, error)
}

func (h *testHandler) Handle(ctx context.Context, msg *txmsg.Message) (bool, error) {
	return h.handleFn(ctx, msg)
}

func TestTxMsg_AddAndRemove(t *testing.T) {
	store := memory.New()
	handler := &testHandler{handleFn: func(context.Context, *txmsg.Message) (bool, error) { return true, nil }}

	tx := txmsg.New(store, nil, handler, "test_topic",
		txmsg.WithPollInterval(100*time.Millisecond),
		txmsg.WithBatchSize(10),
		txmsg.WithBodyType(&testData{}),
	)
	defer tx.Close()

	ctx := context.Background()
	msg, err := tx.Add(ctx, &testData{S: "hello"}, 0)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if msg == nil || msg.ID == "" {
		t.Fatal("expected non-empty msg ID")
	}
	if d, ok := msg.Body.(*testData); !ok || d.S != "hello" {
		t.Fatalf("unexpected body: %+v", msg.Body)
	}

	if err := tx.Remove(ctx, msg); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	msgs, err := store.Poll(ctx, "test_topic", nil, 10, time.Second)
	if err != nil {
		t.Fatalf("Poll failed: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages after remove, got %d", len(msgs))
	}
}

func TestTxMsg_CompensationFlow(t *testing.T) {
	store := memory.New()
	var alarmCalled, handleCalls, removeCalls int32

	handler := &testHandler{handleFn: func(ctx context.Context, msg *txmsg.Message) (bool, error) {
		atomic.AddInt32(&handleCalls, 1)
		if msg.RetryCount >= 2 {
			atomic.AddInt32(&removeCalls, 1)
			return true, nil
		}
		return false, nil
	}}

	tx := txmsg.New(store, nil, handler, "test_topic",
		txmsg.WithPollInterval(50*time.Millisecond),
		txmsg.WithBatchSize(10),
		txmsg.WithMaxRetries(5),
		txmsg.WithConcurrency(4),
		txmsg.WithVisibilityTimeout(500*time.Millisecond),
		txmsg.WithBackoff(func(rc int) time.Duration { return time.Duration(rc) * 5 * time.Millisecond }),
		txmsg.WithAlarmFunc(func(context.Context, *txmsg.Message) { atomic.AddInt32(&alarmCalled, 1) }),
		txmsg.WithBodyType(&testData{}),
	)
	tx.Start()
	defer tx.Close()

	ctx := context.Background()
	_, err := tx.Add(ctx, &testData{S: "world"}, 0)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline) && atomic.LoadInt32(&handleCalls) < 3; {
		time.Sleep(10 * time.Millisecond)
	}
	if atomic.LoadInt32(&handleCalls) < 3 {
		t.Fatalf("expected at least 3 handle calls, got %d", handleCalls)
	}
	if atomic.LoadInt32(&removeCalls) != 1 {
		t.Fatalf("expected 1 remove after resolved, got %d", removeCalls)
	}
	if atomic.LoadInt32(&alarmCalled) != 0 {
		t.Fatalf("expected alarm not called, got %d", alarmCalled)
	}
}

func TestTxMsg_AlarmOnMaxRetries(t *testing.T) {
	store := memory.New()
	var alarmCalled int32

	handler := &testHandler{handleFn: func(context.Context, *txmsg.Message) (bool, error) { return false, nil }}

	tx := txmsg.New(store, nil, handler, "test_topic",
		txmsg.WithPollInterval(10*time.Millisecond),
		txmsg.WithBatchSize(10),
		txmsg.WithMaxRetries(3),
		txmsg.WithVisibilityTimeout(500*time.Millisecond),
		txmsg.WithBackoff(func(rc int) time.Duration { return time.Duration(rc) * 5 * time.Millisecond }),
		txmsg.WithAlarmFunc(func(context.Context, *txmsg.Message) { atomic.AddInt32(&alarmCalled, 1) }),
		txmsg.WithBodyType(&testData{}),
	)
	tx.Start()
	defer tx.Close()

	ctx := context.Background()
	_, err := tx.Add(ctx, &testData{S: "data"}, 0)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline) && atomic.LoadInt32(&alarmCalled) == 0; {
		time.Sleep(10 * time.Millisecond)
	}
	if atomic.LoadInt32(&alarmCalled) == 0 {
		t.Fatal("expected alarm to be called after max retries")
	}
}

func TestTxMsg_MultiInstance_DifferentStructs(t *testing.T) {
	var orderChecked, paymentChecked int32

	orderHandler := &testHandler{handleFn: func(ctx context.Context, msg *txmsg.Message) (bool, error) {
		o, ok := msg.Body.(*orderData)
		if !ok {
			t.Errorf("expected *orderData, got %T", msg.Body)
			return true, nil
		}
		if o.OrderID != "ORD-1" || o.Amount != 9999 || !o.Paid {
			t.Errorf("unexpected orderData: %+v", o)
		}
		atomic.StoreInt32(&orderChecked, 1)
		return true, nil
	}}

	paymentHandler := &testHandler{handleFn: func(ctx context.Context, msg *txmsg.Message) (bool, error) {
		p, ok := msg.Body.(*paymentData)
		if !ok {
			t.Errorf("expected *paymentData, got %T", msg.Body)
			return true, nil
		}
		if p.TxID != "TXN-1" || p.UserID != 42 || p.Status != 1 {
			t.Errorf("unexpected paymentData: %+v", p)
		}
		atomic.StoreInt32(&paymentChecked, 1)
		return true, nil
	}}

	orderStore := memory.New()
	paymentStore := memory.New()

	baseOpts := []txmsg.Option{txmsg.WithPollInterval(100 * time.Millisecond), txmsg.WithBatchSize(10)}
	orderTx := txmsg.New(orderStore, nil, orderHandler, "order_topic",
		append(baseOpts, txmsg.WithBodyType(&orderData{}))...,
	)
	paymentTx := txmsg.New(paymentStore, nil, paymentHandler, "payment_topic",
		append(baseOpts, txmsg.WithBodyType(&paymentData{}))...,
	)
	defer orderTx.Close()
	defer paymentTx.Close()

	ctx := context.Background()
	_, err := orderTx.Add(ctx, &orderData{OrderID: "ORD-1", Amount: 9999, Paid: true}, 0)
	if err != nil {
		t.Fatalf("order Add failed: %v", err)
	}
	_, err = paymentTx.Add(ctx, &paymentData{TxID: "TXN-1", UserID: 42, Status: 1}, 0)
	if err != nil {
		t.Fatalf("payment Add failed: %v", err)
	}

	orderTx.Start()
	paymentTx.Start()

	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline) &&
		(atomic.LoadInt32(&orderChecked) == 0 || atomic.LoadInt32(&paymentChecked) == 0); {
		time.Sleep(10 * time.Millisecond)
	}

	if atomic.LoadInt32(&orderChecked) == 0 {
		t.Fatal("order handler was not called")
	}
	if atomic.LoadInt32(&paymentChecked) == 0 {
		t.Fatal("payment handler was not called")
	}
}
