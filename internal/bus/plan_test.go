package bus

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExactTopicListReadsPagesFromTopicIndex(t *testing.T) {
	b, err := Open(filepath.Join(t.TempDir(), "bus.db"))
	require.NoError(t, err)
	t.Cleanup(func() { b.Close() })
	query, args := readQuery("remote.watching,agents.changed", 0, 10, 100)
	rows, err := b.db.Query(`EXPLAIN QUERY PLAN `+query, args...)
	require.NoError(t, err)
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		require.NoError(t, rows.Scan(&id, &parent, &unused, &detail))
		plan = append(plan, detail)
	}
	got := strings.Join(plan, "\n")
	assert.Contains(t, got, "MERGE (UNION ALL)")
	assert.Equal(t, 2, strings.Count(got, "USING INDEX events_topic_seq (topic=? AND seq>? AND seq<?)"), got)
}
