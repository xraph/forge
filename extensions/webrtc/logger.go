package webrtc

import "github.com/xraph/forge"

// nullLogger is a no-op logger for when no logger is provided
// This prevents nil pointer panics while maintaining the logging interface
type nullLogger struct{}

func (n *nullLogger) Debug(msg string, fields ...forge.Field) {}
func (n *nullLogger) Info(msg string, fields ...forge.Field)  {}
func (n *nullLogger) Warn(msg string, fields ...forge.Field)  {}
func (n *nullLogger) Error(msg string, fields ...forge.Field) {}
