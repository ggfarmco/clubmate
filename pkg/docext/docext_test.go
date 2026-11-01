package docext_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"clubmate/pkg/docext"
)

func TestExt(t *testing.T) {
	assert.True(t, docext.Is("/r/A.MD"))
	assert.True(t, docext.Is("x.markdown"))
	assert.True(t, docext.Is("x.mmd"))
	assert.False(t, docext.Is("x.html"))
	assert.True(t, docext.IsArtifact("/r/report.HTML"))
	assert.True(t, docext.IsArtifact("x.htm"))
	assert.False(t, docext.IsArtifact("x.md"))
}
