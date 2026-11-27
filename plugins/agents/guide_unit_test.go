package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/acp"
)

func TestQuizConceptState(t *testing.T) {
	answered := func(confidence, verdict, flag string) entry {
		resp := acp.ElicitationResponse{Action: acp.ElicitationAccept, Content: map[string]any{"answer": "30", "confidence": confidence}}
		return entry{Response: &resp, Grade: &quizGrade{Verdict: verdict, Flag: flag}}
	}
	for _, c := range []struct {
		name string
		en   entry
		want string
	}{
		{"correct and sure", answered("sure", verdictCorrect, ""), sdui.TourKnown},
		{"correct and think so", answered("think", verdictCorrect, ""), sdui.TourShaky},
		{"close", answered("sure", verdictClose, ""), sdui.TourShaky},
		{"wrong", answered("guess", verdictWrong, ""), sdui.TourMissed},
		{"sure and wrong", answered("sure", verdictWrong, flagSureWrong), sdui.TourMissed},
		{"waiting for a grade", answered("sure", verdictPending, ""), ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, quizConceptState(c.en))
		})
	}
}

func TestQuizAndGuideReadsDoNotBumpTheRecordVersion(t *testing.T) {
	t.Parallel()
	s := newSession(record{ID: "agt_1"}, nil, true, nil)
	ver := s.mu.ver.Load()

	s.guideSnapshot()
	s.keptLedger()
	s.pendingQuiz()
	s.quizOf("q")
	s.quizAnsweredOf("q")
	_, err := s.awaitQuiz(t.Context(), "q", 0)

	assert.ErrorIs(t, err, errNotAQuiz)
	assert.Equal(t, ver, s.mu.ver.Load())
}

func TestQuizAnswerRefusesARelayedCall(t *testing.T) {
	t.Parallel()
	a := &app{registry: newRegistry()}
	_, err := a.quizAnswerCommand(t.Context(), pluginsdk.CommandRun{Via: "graphs"}, quizAct{Session: "agt_1"})
	assert.ErrorIs(t, err, errUserOnly)
}

func TestLedgerCacheForgetsAGoneFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "tour.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"concepts":[{"id":"c1","title":"One"}]}`), 0o600))
	_, ok := readLedger(path)
	require.True(t, ok)

	require.NoError(t, os.Remove(path))
	_, ok = readLedger(path)

	assert.False(t, ok)
	ledgers.mu.Lock()
	defer ledgers.mu.Unlock()
	assert.NotContains(t, ledgers.items, path)
}

func TestQuizKeyOfSeveralLinesIsPlural(t *testing.T) {
	t.Parallel()
	spec := quizSpec{Type: quizLine, Key: []int{3, 5}}
	assert.Equal(t, "lines 3, 5", keyWords(spec, quizEnglish))
	assert.Equal(t, "строки 3, 5", keyWords(spec, quizRussian))
}
