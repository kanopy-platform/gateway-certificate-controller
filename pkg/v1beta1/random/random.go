package random

import (
	"crypto/rand"
	"math/big"
)

func SecureString(n int) string {
	if n < 1 {
		return ""
	}

	const chars = "0123456789abcdefghijklmnopqrstuvwxyz"
	bytes := make([]byte, n)

	for i := range bytes {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		if err != nil {
			return ""
		}

		bytes[i] = chars[num.Int64()]
	}

	return string(bytes)
}
