package main_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
)

const (
	limiterFile = "internal/notify/limiter.go"
	quizWhy     = "32 only caps the bucket at `l.burst`; 30 is where it grows: seconds since the last call × `l.rate`."
)

type quizOut struct {
	Request    string `json:"request"`
	Status     string `json:"status"`
	Answer     string `json:"answer"`
	Lines      []int  `json:"lines"`
	Confidence string `json:"confidence"`
	Verdict    string `json:"verdict"`
	Correct    *bool  `json:"correct"`
}

type quizCall struct {
	out quizOut
	err error
}

func quizRepo(t *testing.T) string {
	t.Helper()
	repo := newRepo(t)
	lines := make([]string, 0, 40)
	for i := 1; i <= 22; i++ {
		lines = append(lines, fmt.Sprintf("// line %d", i))
	}
	lines = append(lines, "\tb, ok := l.buckets[ws]", "\tif !ok {", "\t\tb = &bucket{tokens: l.burst, last: l.now()}", "\t\tl.buckets[ws] = b", "\t}", "",
		"\tnow := l.now()", "\tb.tokens += now.Sub(b.last).Seconds() * l.rate", "\tif b.tokens > l.burst {", "\t\tb.tokens = l.burst", "\t}", "\tb.last = now", "",
		"\tif b.tokens < 1 {", "\t\treturn false", "\t}", "\tb.tokens--", "\treturn true")
	require.NoError(t, os.MkdirAll(filepath.Join(repo, "internal/notify"), 0o700))
	write(t, filepath.Join(repo, limiterFile), strings.Join(lines, "\n")+"\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-qm", "limiter")
	return repo
}

func (f *fixture) askQuiz(t *testing.T, workspace, session string, args map[string]any) <-chan quizCall {
	t.Helper()
	done := make(chan quizCall, 1)
	go func() {
		var c quizCall
		c.err = f.runAs(t, workspace, session, "agents.quiz", args, &c.out)
		done <- c
	}()
	return done
}

func quizNode(t *testing.T, f *fixture, workspace string, match func(sdui.Node) bool) sdui.Node {
	t.Helper()
	var found sdui.Node
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
		i := slices.IndexFunc(cardsOf(n, "request.quiz"), match)
		if i >= 0 {
			found = cardsOf(n, "request.quiz")[i]
		}
		return i >= 0
	})
	return found
}

func inState(state string) func(sdui.Node) bool {
	return func(n sdui.Node) bool { return n.Props["state"] == state }
}

func quizEvent(t *testing.T, f *fixture, workspace string, n sdui.Node, action string, value map[string]any) {
	t.Helper()
	value["request"] = n.Props["request"]
	f.h.Event(t, workspace, "agents.main", n.ID, action, value)
}

func sampleProps(t *testing.T, name string, n sdui.Node) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	var sample sdui.Node
	require.NoError(t, json.Unmarshal(raw, &sample))
	sample.Props["request"] = n.Props["request"]
	sample.Props["excerpt"].(map[string]any)["commit"] = n.Props["excerpt"].(map[string]any)["commit"]
	return sample.Props
}

func jsonOf(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	return string(raw)
}

func waitQuiz(t *testing.T, done <-chan quizCall) quizOut {
	t.Helper()
	select {
	case c := <-done:
		require.NoError(t, c.err)
		return c.out
	case <-time.After(testutil.Timeout(30 * time.Second)):
		require.FailNow(t, "agents.quiz never returned")
	}
	return quizOut{}
}

func lineQuiz(extra map[string]any) map[string]any {
	args := map[string]any{"type": "line", "question": "Which line refills a workspace’s bucket?", "concept": "Refill", "step": "Step 4 of 8", "file": limiterFile, "from": 23, "to": 40, "lines": []int{30}, "why": quizWhy}
	for k, v := range extra {
		args[k] = v
	}
	return args
}

func TestQuizLinePickHoldsThenReveals(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{})
	workspace := f.workspace(t, quizRepo(t))
	session := f.newSession(t, workspace)
	done := f.askQuiz(t, workspace, session, lineQuiz(nil))

	asking := quizNode(t, f, workspace, inState("asking"))
	f.waitStatus(t, workspace, session, "waiting")
	assert.Equal(t, true, asking.Props["confidence"].(map[string]any)["needsRating"])
	quizEvent(t, f, workspace, asking, "quizPick", map[string]any{"from": 32})
	picked := quizNode(t, f, workspace, func(n sdui.Node) bool { return n.Props["excerpt"].(map[string]any)["picked"] != nil })
	assert.JSONEq(t, jsonOf(t, sampleProps(t, "quiz-asking.json", picked)), jsonOf(t, picked.Props))

	quizEvent(t, f, workspace, picked, "quizRate", map[string]any{"confidence": "think"})
	out := waitQuiz(t, done)
	assert.Equal(t, quizOut{Request: asking.Props["request"].(string), Status: "answered", Answer: "32", Lines: []int{32, 32}, Confidence: "think", Verdict: "wrong", Correct: new(false)}, out)
	answered := quizNode(t, f, workspace, inState("answered"))
	assert.JSONEq(t, jsonOf(t, sampleProps(t, "quiz-answered.json", answered)), jsonOf(t, answered.Props))
}

func TestQuizUndoKeepsThePick(t *testing.T) {
	t.Parallel()
	f := startEnv(t, []string{"CLUBMATE_AGENTS_QUIZ_HOLD=1h"}, map[string]any{})
	workspace := f.workspace(t, quizRepo(t))
	session := f.newSession(t, workspace)
	f.askQuiz(t, workspace, session, lineQuiz(map[string]any{"type": "range", "lines": []int{31, 33}}))

	asking := quizNode(t, f, workspace, inState("asking"))
	quizEvent(t, f, workspace, asking, "quizPick", map[string]any{"from": 33, "to": 31})
	quizEvent(t, f, workspace, asking, "quizRate", map[string]any{"confidence": "sure"})
	quizNode(t, f, workspace, inState("sending"))
	quizEvent(t, f, workspace, asking, "quizUndo", map[string]any{})
	back := quizNode(t, f, workspace, inState("asking"))
	assert.Equal(t, []any{31.0, 33.0}, back.Props["excerpt"].(map[string]any)["picked"])
	assert.NotContains(t, back.Props["confidence"], "value")
}

func TestQuizTypedAnswerWaitsForTheAgentGradeAndSurvivesRestart(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{})
	workspace := f.workspace(t, quizRepo(t))
	session := f.newSession(t, workspace)
	done := f.askQuiz(t, workspace, session, map[string]any{"type": "text", "question": "Why does line 36 check for less than 1, not for 0?", "concept": "Spend"})

	asking := quizNode(t, f, workspace, inState("asking"))
	assert.Equal(t, false, asking.Props["confidence"].(map[string]any)["sends"])
	main := f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool { return len(cardsOf(n, "composer")) == 1 })
	composer := cardsOf(main, "composer")[0].ID
	assert.Equal(t, "Say it in words…", cardsOf(main, "composer")[0].Props["placeholder"])
	params := map[string]any{"workspaceId": workspace, "slot": "agents.main", "nodeId": composer, "action": "send", "value": map[string]any{"text": "tokens are a float"}}
	require.ErrorContains(t, f.h.Client(t).Call(t.Context(), "ui.event", params, nil), "rate first")
	quizEvent(t, f, workspace, asking, "quizRate", map[string]any{"confidence": "sure"})
	quizNode(t, f, workspace, func(n sdui.Node) bool { return n.Props["confidence"].(map[string]any)["value"] == "sure" })
	f.h.Event(t, workspace, "agents.main", composer, "send", map[string]any{"text": "tokens are a float"})
	out := waitQuiz(t, done)
	assert.Equal(t, quizOut{Request: out.Request, Status: "answered", Answer: "tokens are a float", Confidence: "sure", Verdict: "pending"}, out)
	quizNode(t, f, workspace, inState("checking"))

	require.Error(t, f.runAs(t, workspace, session, "agents.quiz-grade", map[string]any{"request": out.Request, "verdict": "maybe"}, nil))
	require.NoError(t, f.runAs(t, workspace, session, "agents.quiz-grade", map[string]any{"request": out.Request, "verdict": "wrong", "why": "0.4 of a token is not enough to spend"}, nil))
	quizNode(t, f, workspace, inState("answered"))

	testutil.Eventually(t, func() bool { return len(f.calls(t, "start")) > 0 }, wait)
	f.restartPlugin(t, workspace)
	var tr struct {
		Entries []struct {
			ID   string          `json:"id"`
			Quiz *sdui.QuizProps `json:"quiz"`
		} `json:"entries"`
	}
	require.NoError(t, f.run(t, workspace, "agents.transcript", map[string]any{"session": session}, &tr))
	i := slices.IndexFunc(tr.Entries, func(e struct {
		ID   string          `json:"id"`
		Quiz *sdui.QuizProps `json:"quiz"`
	}) bool {
		return e.ID == out.Request
	})
	require.GreaterOrEqual(t, i, 0)
	q := tr.Entries[i].Quiz
	require.NotNil(t, q)
	require.NotNil(t, q.Reveal)
	assert.Equal(t, "answered", q.State)
	assert.Equal(t, sdui.QuizReveal{Verdict: "wrong", Label: "Not quite", Yours: "tokens are a float", Confidence: "sure", Why: "0.4 of a token is not enough to spend", Flag: "sure_wrong", Note: "You were sure. Spend goes back on the map as missed; the guide asks it again later."}, *q.Reveal)
}

func TestQuizPhoneAnswersAChoice(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{})
	workspace := f.workspace(t, quizRepo(t))
	session := f.newSession(t, workspace)
	options := []map[string]any{{"value": "float", "label": "tokens are a float"}, {"value": "zero", "label": "0 is a valid count"}}
	done := f.askQuiz(t, workspace, session, map[string]any{"type": "choice", "question": "Why < 1?", "options": options, "accept": []string{"float"}})
	asking := quizNode(t, f, workspace, inState("asking"))
	request := asking.Props["request"]
	assert.Equal(t, "b", asking.Props["options"].([]any)[1].(map[string]any)["key"])

	answer := func(args map[string]any) error {
		args["session"], args["request"] = session, request
		return f.h.PluginCallResult(t, "remote", "command.run", map[string]any{"id": "agents.quiz-answer", "workspaceId": workspace, "args": args}, nil)
	}
	require.Error(t, f.runAs(t, workspace, session, "agents.quiz-answer", map[string]any{"session": session, "request": request, "action": "skip"}, nil))
	require.NoError(t, answer(map[string]any{"action": "rate", "confidence": "guess"}))
	testutil.Never(t, func() bool { return len(done) > 0 }, time.Second)
	require.NoError(t, answer(map[string]any{"action": "pick", "option": "float"}))
	require.NoError(t, answer(map[string]any{"action": "rate", "confidence": "sure"}))
	out := waitQuiz(t, done)
	assert.Equal(t, "correct", out.Verdict)
	assert.Equal(t, "float", out.Answer)
}
