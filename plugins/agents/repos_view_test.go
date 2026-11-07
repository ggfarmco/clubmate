package main

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internalsdui "clubmate/internal/sdui"
	"clubmate/pkg/sdui"
)

func reposScreen(repos ...taskRepo) screen {
	rec := record{ID: "agt_1", Task: &taskRecord{Slug: "campaign-pause-reason", Repos: repos}}
	return screen{View: transcriptView{Session: rec}}
}

func repoRow(name string, order int, state string) taskRepo {
	return taskRepo{Path: "/w/" + name, Name: name, Order: order, LandsBy: landsByPR, State: state}
}

func withKid(sc screen, r *taskRepo, kid record) screen {
	r.Session = kid.ID
	if sc.Repos.Kids == nil {
		sc.Repos.Kids = map[string]record{}
	}
	sc.Repos.Kids[kid.ID] = kid
	return sc
}

func cardProps(t *testing.T, sc screen) sdui.ReposProps {
	t.Helper()
	n, ok := reposCard(sc)
	require.True(t, ok)
	require.NoError(t, internalsdui.Validate(sdui.VStack(n)))
	p, _ := reposData(sc)
	return p
}

func TestReposCardWordsComeFromTheEngine(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		repo taskRepo
		want string
		tone string
	}{
		{"merged names its PR", taskRepo{State: landMerged, PR: "87"}, "merged · PR #87", "success"},
		{"a PR url is cut to its number", taskRepo{State: landPROpen, PR: "https://git.example/acme/pull/412"}, "PR #412 open", ""},
		{"no number", taskRepo{State: landPROpen}, "PR open", ""},
		{"on branch", taskRepo{State: landOnBranch}, "on its branch", "success"},
		{"a Land request is amber", taskRepo{State: landReady, Request: "lr_1"}, "ready to land", "warning"},
		{"ready with no request isn't", taskRepo{State: landReady}, "ready to land", ""},
		{"failed", taskRepo{State: landFailed}, "failed", "danger"},
		{"landing", taskRepo{State: landRunning, Landing: true}, "landing", ""},
		{"not started", taskRepo{State: landWaits}, "waits", ""},
		{"waits for what", taskRepo{State: landWaits, For: waitsForReport}, "waits for its report", ""},
		{"asking the parent isn't amber", taskRepo{State: landAsking}, "asking", ""},
		{"left", taskRepo{State: landLeft}, "left unlanded", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			text, tone, _ := repoStateWord(taskRecord{Repos: []taskRepo{tc.repo}}, 0, nil)
			assert.Equal(t, tc.want, text)
			assert.Equal(t, tc.tone, tone)
		})
	}
}

func TestHeldRepoNamesTheOrderItWaitsFor(t *testing.T) {
	t.Parallel()
	failed, held := repoRow("internal-auth", 20, landFailed), repoRow("ads-management", 30, landWaits)
	held.Finished, held.For = true, "internal-auth, which failed"
	task := taskRecord{Repos: []taskRepo{repoRow("ads-snapshot", 10, landMerged), failed, held}}

	text, _, _ := repoStateWord(task, 2, nil)

	assert.Equal(t, "on its branch · waits for 2", text)
}

func TestReposHeaderSumsTheStates(t *testing.T) {
	t.Parallel()
	p := cardProps(t, reposScreen(repoRow("a", 1, landMerged), repoRow("b", 2, landFailed), repoRow("c", 3, landWaits)))

	assert.Equal(t, "3 repos", p.Title)
	assert.Equal(t, []sdui.ReposSegment{{Text: "1 merged"}, {Text: "1 failed", Tone: "danger"}, {Text: "1 waits"}}, p.Summary)
	assert.Equal(t, "feat/campaign-pause-reason", p.Branch)
	assert.Equal(t, "Lands in order 1 → 2 → 3 · a repo waits until the ones before it landed", p.Footer)
}

func TestReposHeaderCountsOpenPRsInOrder(t *testing.T) {
	t.Parallel()
	p := cardProps(t, reposScreen(repoRow("a", 1, landMerged), repoRow("b", 2, landPROpen), repoRow("c", 3, landPROpen)))

	assert.Equal(t, sdui.ReposSegment{Text: "2 PRs open, in order"}, p.Summary[1])
}

func TestSameOrderSharesARank(t *testing.T) {
	t.Parallel()
	p := cardProps(t, reposScreen(repoRow("a", 5, landWaits), repoRow("b", 5, landWaits), repoRow("c", 9, landWaits)))

	assert.Equal(t, []int{1, 1, 2}, []int{p.Rows[0].Order, p.Rows[1].Order, p.Rows[2].Order})
	assert.True(t, strings.HasPrefix(p.Footer, "Lands in order 1 → 2 "), p.Footer)
}

func TestCardIsFoldedUntilARepoFails(t *testing.T) {
	t.Parallel()
	a := repoRow("a", 1, landWaits)
	assert.True(t, cardProps(t, reposScreen(a)).Collapsed)

	a.State = landFailed
	p := cardProps(t, reposScreen(a))

	assert.False(t, p.Collapsed)
	assert.True(t, p.Rows[0].Open)
}

func TestCursorStartsOnTheFailedRow(t *testing.T) {
	t.Parallel()
	sc := reposScreen(repoRow("a", 1, landMerged), repoRow("b", 2, landFailed))
	sc = withKid(sc, &sc.View.Session.Task.Repos[1], record{ID: "agt_kid", Status: statusDone})

	p := cardProps(t, sc)

	assert.False(t, p.Rows[0].Cursor)
	assert.True(t, p.Rows[1].Cursor)
	assert.Equal(t, "↑↓ repo · ↩ ask it to fix", p.Hint)
}

func TestFoldingSticksUntilANewFailure(t *testing.T) {
	t.Parallel()
	since := time.Unix(100, 0)
	a := repoRow("a", 1, landFailed)
	a.Since = since
	sc := reposScreen(a)
	sc.Repos.Choice.toggleCard(*sc.View.Session.Task)

	assert.True(t, cardProps(t, sc).Collapsed)

	a.Since = since.Add(time.Hour)
	choice := sc.Repos.Choice
	sc = reposScreen(a)
	sc.Repos.Choice = choice

	assert.False(t, cardProps(t, sc).Collapsed)
}

func TestFailedRowOpensWithItsTailAndActions(t *testing.T) {
	t.Parallel()
	a, b := repoRow("internal-auth", 1, landFailed), repoRow("ads-management", 2, landWaits)
	a.Tail = "--- FAIL: TestScopeOrder"
	sc := reposScreen(a, b)
	task := sc.View.Session.Task
	sc = withKid(sc, &task.Repos[0], record{ID: "agt_kid", Status: statusDone, Worktree: "/w/.worktrees/x"})

	n, ok := reposCard(sc)
	require.True(t, ok)
	p := cardProps(t, sc)

	require.True(t, p.Rows[0].Open)
	assert.Equal(t, a.Tail, p.Rows[0].Tail)
	assert.Equal(t, "Leaving it unlanded holds ads-management too.", p.Rows[0].Note)
	assert.Empty(t, p.Rows[1].Actions)
	var labels []string
	for _, b := range nodesOf(n, "button") {
		labels = append(labels, b.Props["label"].(string))
	}
	assert.Equal(t, []string{"Ask it to fix", "Open its session", "Open in Code", "Leave it unlanded…"}, labels)
}

func TestClosedOrchestratorOffersNoActions(t *testing.T) {
	t.Parallel()
	sc := reposScreen(repoRow("a", 1, landFailed))
	sc.View.Session.Status = statusClosed

	n, ok := reposCard(sc)

	require.True(t, ok)
	assert.Empty(t, nodesOf(n, "button"))
}

func TestRepoWithNoChildCannotBeAskedToFix(t *testing.T) {
	t.Parallel()
	n, ok := reposCard(reposScreen(repoRow("a", 1, landFailed)))

	require.True(t, ok)
	for _, b := range nodesOf(n, "button") {
		assert.NotEqual(t, "Ask it to fix", b.Props["label"])
	}
}

func TestCardStaysOutOfTheCodePanel(t *testing.T) {
	t.Parallel()
	sc := reposScreen(repoRow("a", 1, landFailed))
	sc.Code = &codePanel{}

	_, ok := reposCard(sc)

	assert.False(t, ok)
}

func TestPillNamesWhatNeedsYou(t *testing.T) {
	t.Parallel()
	ready := repoRow("a", 1, landReady)
	ready.Request = "lr_1"
	cases := []struct {
		repos  []taskRepo
		detail string
		tone   string
	}{
		{[]taskRepo{repoRow("a", 1, landFailed)}, "1 failed", "danger"},
		{[]taskRepo{ready}, "", ""},
		{[]taskRepo{repoRow("a", 1, landMerged)}, "", ""},
	}
	for _, tc := range cases {
		n, ok := reposPill(record{ID: "agt_1", Task: &taskRecord{Slug: "x", Repos: tc.repos}})
		require.True(t, ok)
		assert.Equal(t, "1 repo", n.Props["label"])
		if tc.detail == "" {
			assert.NotContains(t, n.Props, "detail")
			continue
		}
		assert.Equal(t, tc.detail, n.Props["detail"])
		assert.Equal(t, tc.tone, n.Props["detailTone"])
	}
}

func TestRowNamesTheDefaultBranch(t *testing.T) {
	t.Parallel()
	a := repoRow("internal-auth", 1, landWaits)
	a.LandsBy, a.Into = landsByMerge, "master"
	assert.Equal(t, "merge to master", cardProps(t, reposScreen(a)).Rows[0].LandsBy)

	a.Into = ""
	assert.Equal(t, "merge", cardProps(t, reposScreen(a)).Rows[0].LandsBy)
}

func TestAFKConnectedRepoReadsAsAPullRequest(t *testing.T) {
	t.Parallel()
	a := repoRow("internal-auth", 1, landWaits)
	a.LandsBy, a.Source, a.Capped = landsByMerge, "its CLAUDE.md", true

	p := cardProps(t, reposScreen(a))

	assert.Equal(t, "pull request", p.Rows[0].LandsBy)
	assert.Equal(t, reposCappedRule, p.Rows[0].Source)
}
