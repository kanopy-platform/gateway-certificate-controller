package random

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSecureString(t *testing.T) {
	t.Parallel()

	s1 := SecureString(32)
	assert.Len(t, s1, 32)

	s2 := SecureString(32)
	assert.Len(t, s2, 32)

	assert.NotEqual(t, s1, s2)

	s3 := SecureString(-1)
	assert.Len(t, s3, 0)
}
