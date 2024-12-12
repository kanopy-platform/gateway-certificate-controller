package zap

import (
	"fmt"

	"go.uber.org/zap/zapcore"
)

func ParseLevel(level string) (zapcore.Level, error) {
	switch level {
	case "debug":
		return zapcore.Level(-5), nil // Match k8s controller-runtime V(5) as zapcore.DebugLevel is not verbose enough
	case "info":
		return zapcore.InfoLevel, nil
	case "error":
		return zapcore.ErrorLevel, nil
	case "warn":
		return zapcore.WarnLevel, nil
	default:
		return -1, fmt.Errorf("unknown level: %s", level)
	}
}
