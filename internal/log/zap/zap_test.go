package zap

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap/zapcore"
)

func TestParseLevelInfo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		level string
		want  zapcore.Level
	}{
		{
			level: "info",
			want:  zapcore.InfoLevel,
		},
		{
			level: "debug",
			want:  zapcore.Level(-5),
		},
		{
			level: "warn",
			want:  zapcore.WarnLevel,
		},
		{
			level: "error",
			want:  zapcore.ErrorLevel,
		},
	}

	for _, test := range tests {
		l, err := ParseLevel(test.level)
		assert.NoError(t, err)
		assert.Equal(t, test.want, l)
	}
}

func TestParseLevelError(t *testing.T) {
	t.Parallel()
	_, err := ParseLevel("")
	assert.Error(t, err)
}
