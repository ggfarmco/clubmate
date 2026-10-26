package id_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"clubmate/internal/id"
)

func TestNewFormat(t *testing.T) {
	assert.Regexp(t, `^prj_[0-9a-f]{16}$`, id.New("prj"))
}
