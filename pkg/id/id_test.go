package id_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"clubmate/pkg/id"
)

func TestNewFormat(t *testing.T) {
	assert.Regexp(t, `^prj_[0-9a-f]{16}$`, id.New("prj"))
	assert.Regexp(t, `^cj_[0-9a-f]{8}$`, id.Short("cj"))
}

func TestValid(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"ses_0123456789abcdef", true},
		{"prj_0123456789abcdef", false},
		{"ses_0123456789ABCDEF", false},
		{"ses_0123456789abcde", false},
		{"ses_../0123456789ab", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, id.Valid("ses", tt.in), tt.in)
	}
}
