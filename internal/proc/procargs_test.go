package proc_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/proc"
)

func TestParseProcArgs(t *testing.T) {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.LittleEndian, int32(2))
	b.WriteString("/bin/sh\x00\x00\x00sh\x00-c\x00A=1\x00CLUBMATE_SESSION_ID=ses_x\x00\x00")
	argv, env, err := proc.ParseProcArgs(b.Bytes())
	require.NoError(t, err)
	assert.Equal(t, []string{"sh", "-c"}, argv)
	assert.Equal(t, []string{"A=1", "CLUBMATE_SESSION_ID=ses_x"}, env)
}

func TestParseProcArgsTruncated(t *testing.T) {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.LittleEndian, int32(3))
	b.WriteString("/bin/sh\x00sh\x00-c")
	argv, env, err := proc.ParseProcArgs(b.Bytes())
	require.NoError(t, err)
	assert.Equal(t, []string{"sh", "-c"}, argv)
	assert.Empty(t, env)

	_, _, err = proc.ParseProcArgs([]byte{1, 0})
	assert.Error(t, err)
}
