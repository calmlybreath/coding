package agg

import "context"

// ImportantKeyChecker 判断一个 key 是否"重要":重要 key 走独立缓存,不会被普通 LFU/LRU 淘汰。
type ImportantKeyChecker interface {
	IsImportant(ctx context.Context, key interface{}) bool
}

// ImportantStringKey 是 ImportantKeyChecker 的 string-key 实现。
type ImportantStringKey struct {
	keys map[string]struct{}
}

// NewImportantStringKey 用一组重要 key 构造。
func NewImportantStringKey(keys []string) *ImportantStringKey {
	km := make(map[string]struct{})
	for _, k := range keys {
		km[k] = struct{}{}
	}
	return &ImportantStringKey{keys: km}
}

func (k *ImportantStringKey) IsImportant(ctx context.Context, key interface{}) bool {
	_, ok := k.keys[key.(string)]
	return ok
}
