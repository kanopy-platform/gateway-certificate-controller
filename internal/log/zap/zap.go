package zap

import (
	"fmt"

	"go.uber.org/zap/zapcore"
)

func ParseLevel(level string) (zapcore.Level, error) {
	switch level {
	case "info":
		return zapcore.InfoLevel, nil
	case "debug":
		return zapcore.DebugLevel, nil
	case "error":
		return zapcore.ErrorLevel, nil
	case "warn":
		return zapcore.WarnLevel, nil
	default:
		return -1, fmt.Errorf("unknown level: %s", level)
	}
}
