package id

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

const (
	hexLen      = 16
	shortHexLen = 8
)

func New(prefix string) string {
	return generate(prefix, hexLen)
}

func Short(prefix string) string {
	return generate(prefix, shortHexLen)
}

func Valid(prefix, s string) bool {
	rest, ok := strings.CutPrefix(s, prefix+"_")
	if !ok || len(rest) != hexLen {
		return false
	}
	for _, c := range rest {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}

func generate(prefix string, n int) string {
	b := make([]byte, n/2)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return prefix + "_" + hex.EncodeToString(b)
}
