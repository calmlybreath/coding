package txmsg

// Logger 分级日志接口
type Logger interface {
	Debugf(format string, v ...interface{})
	Infof(format string, v ...interface{})
	Warnf(format string, v ...interface{})
	Errorf(format string, v ...interface{})
}

// NopLogger 空日志实现
type NopLogger struct{}

func (n *NopLogger) Debugf(format string, v ...interface{}) {}
func (n *NopLogger) Infof(format string, v ...interface{})  {}
func (n *NopLogger) Warnf(format string, v ...interface{})  {}
func (n *NopLogger) Errorf(format string, v ...interface{}) {}
