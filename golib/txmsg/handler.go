package txmsg

import "context"

type Handler interface {
	Handle(ctx context.Context, msg *Message) (resolved bool, err error)
}

type AlarmFunc func(ctx context.Context, msg *Message)
