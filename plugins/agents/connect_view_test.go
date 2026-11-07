package main

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internalsdui "clubmate/internal/sdui"
	"clubmate/pkg/sdui"
)

var cardNow = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

func factsJSON(t *testing.T, f map[string]any) json.RawMessage {
	t.Helper()
	base := map[string]any{"remote": "git@github.com:acme/ads.git", "branch": "main", "clean": true, "defaultBranch": "main", "lastCommit": cardNow.Add(-5 * time.Hour)}
	for k, v := range f {
		base[k] = v
	}
	raw, err := json.Marshal(base)
	require.NoError(t, err)
	return raw
}

func pendingRepo(t *testing.T, name string, f map[string]any) connectRepo {
	t.Helper()
	return connectRepo{Path: "/src/" + name, Name: name, Reason: "needs `scope`", LandsBy: landsByMerge, Source: "its CLAUDE.md", Facts: factsJSON(t, f), State: repoPending}
}

func connectScreen(repos ...connectRepo) screen {
	rec := record{ID: "agt_1", Agent: agentClaude, ConnectRequests: []connectRequest{{ID: "cr_1", Repos: repos}}}
	return screen{View: transcriptView{Session: rec}, Workspace: "ads", Now: cardNow}
}

func dockedRequest(t *testing.T, sc screen) sdui.Node {
	t.Helper()
	card, ok := dockedPermission(sc)
	require.True(t, ok)
	require.NoError(t, internalsdui.Validate(sdui.VStack(card)))
	return card
}

func connectRows(t *testing.T, card sdui.Node) []sdui.ConnectRepoRow {
	t.Helper()
	rows, ok := card.Props["repos"].([]sdui.ConnectRepoRow)
	require.True(t, ok)
	return rows
}

func TestConnectCardForSeveralReposFlagsAStaleClone(t *testing.T) {
	t.Parallel()
	sc := connectScreen(
		pendingRepo(t, "ads-snapshot", map[string]any{"behind": 14, "brings": map[string]any{"instructions": []string{"CLAUDE.md"}}}),
		pendingRepo(t, "internal-auth", map[string]any{"brings": map[string]any{"instructions": []string{"CLAUDE.md"}, "skills": []string{"a", "b"}, "graphs": []string{"scope-lint"}, "hooks": []string{"setup"}}}),
	)

	card := dockedRequest(t, sc)
	rows := connectRows(t, card)

	assert.Equal(t, "Connect 2 repos?", card.Props["title"])
	assert.Contains(t, card.Props["effect"], "folder of **ads**")
	assert.Contains(t, card.Props["declined"], "Un-ticked repos are skipped")
	assert.Equal(t, sdui.ConnectRepoRow{
		Key: "/src/ads-snapshot", Name: "ads-snapshot", Path: "/src/ads-snapshot", Remote: "github.com/acme/ads", State: "14 behind origin/main", Warn: true,
		Reason: "needs `scope`", LandsBy: "merge to main", Source: "from its CLAUDE.md", Brings: "CLAUDE.md · no graphs or hooks",
	}, rows[0])
	assert.Equal(t, "main · clean", rows[1].State)
	assert.Equal(t, "CLAUDE.md, 2 skills · 1 graph **scope-lint** · 1 worktree hook", rows[1].Brings)
	assert.Equal(t, trustBoth, rows[1].Trust)
}

func TestConnectCardForOneRepoSaysWhatAYesDoes(t *testing.T) {
	t.Parallel()
	sc := connectScreen(pendingRepo(t, "internal-auth", map[string]any{"brings": map[string]any{"instructions": []string{"CLAUDE.md"}, "graphs": []string{"g"}}}))
	sc.View.Session.Task = &taskRecord{Slug: "pause", Repos: []taskRepo{{Name: "ads-management"}}}

	card := dockedRequest(t, sc)

	assert.Equal(t, "Connect a repo?", card.Props["title"])
	assert.Equal(t, "on main · clean · last commit 5 h ago", connectRows(t, card)[0].State)
	assert.Equal(t, []sdui.ConnectEffect{
		{Text: "Becomes a folder of **ads**", Note: "stays after the task · remove it any time"},
		{Text: "Claude starts a child session there", Note: "new branch `feat/pause` · internal-auth's own CLAUDE.md and memory"},
		{Text: "Lands by merge to main", Note: "from its CLAUDE.md · its rules win over the plan"},
		{Text: "Brings CLAUDE.md and 1 graph", Note: trustSteps},
	}, card.Props["effects"])
	assert.Equal(t, "Don't connect: Claude goes on with ads-management only and says what is missing.", card.Props["declined"])
}

func TestConnectCardClonesARepoNotOnDisk(t *testing.T) {
	t.Parallel()
	sc := connectScreen(connectRepo{URL: "https://github.com/acme/auth.git", Name: "auth", Into: "/src/auth", State: repoPending})

	card := dockedRequest(t, sc)
	row := connectRows(t, card)[0]

	assert.Equal(t, "Clone and connect", card.Props["connect"])
	assert.True(t, row.Clone)
	assert.Equal(t, "Clone github.com/acme/auth into /src?", row.State)
	assert.Equal(t, "/src/auth", row.Path)
	assert.Equal(t, "https://github.com/acme/auth.git", row.Key)
}

func TestConnectCardFoldsAfterThreeRepos(t *testing.T) {
	t.Parallel()
	var repos []connectRepo
	for i := range 5 {
		repos = append(repos, pendingRepo(t, fmt.Sprintf("r%d", i), nil))
	}

	card := dockedRequest(t, connectScreen(repos...))
	rows := connectRows(t, card)

	assert.Equal(t, "2 more repos", card.Props["more"])
	assert.False(t, rows[2].Folded)
	assert.True(t, rows[3].Folded)

	one := dockedRequest(t, connectScreen(repos[:4]...))
	assert.Equal(t, "1 more repo", one.Props["more"])
}

func landScreen() screen {
	rec := record{ID: "agt_1", Agent: agentClaude, Task: &taskRecord{Slug: "pause", Repos: []taskRepo{
		{Name: "ads-snapshot", Order: 1, State: landMerged},
		{Name: "internal-auth", Order: 2, State: landReady, Request: "lr_1", LandsBy: landsByMerge, Source: "its CLAUDE.md", Branch: "feat/pause", Into: "main", Commits: 3},
		{Name: "ads-management", Order: 3, State: landWaits},
	}}}
	return screen{View: transcriptView{Session: rec}, Workspace: "ads", Now: cardNow}
}

func TestLandCardNamesItsOrderAndWhatItHolds(t *testing.T) {
	t.Parallel()
	card := dockedRequest(t, landScreen())

	assert.Equal(t, "request.land", card.Type)
	assert.Equal(t, "Land internal-auth?", card.Props["title"])
	assert.Equal(t, "2 of 3 in order · ask before merging", card.Props["caption"])
	assert.Equal(t, "By its rule: merge to main (its CLAUDE.md). ads-snapshot already landed; ads-management waits for this one.", card.Props["rule"])
	assert.Equal(t, "Merge `feat/pause` into `main` · 3 commits", card.Props["detail"])
	assert.Contains(t, card.Props["hint"], "Leaving it unlanded holds ads-management too.")
	assert.Equal(t, "Leave internal-auth unlanded? Leaving it unlanded holds ads-management too.", card.Props["confirm"])
	options := card.Props["options"].([]sdui.PermissionOption)
	require.Len(t, options, 3)
	assert.Equal(t, "opt+3", options[2].Shortcut)
}

func TestAConnectRequestDocksBeforeALandRequest(t *testing.T) {
	t.Parallel()
	sc := landScreen()
	sc.View.Session.ConnectRequests = []connectRequest{{ID: "cr_1", Repos: []connectRepo{pendingRepo(t, "a", nil)}}}

	assert.Equal(t, "request.connectRepo", dockedRequest(t, sc).Type)
	assert.Equal(t, "Answer Claude…", composerPlaceholder(sc))
	assert.Equal(t, "Message Claude…", composerPlaceholder(landScreen()))
}

func TestLandBadgeCountsLandRequests(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "1 to land", landBadge(landScreen().View.Session))
	assert.Empty(t, landBadge(record{}))
}

func TestRuleLineSaysWhoWaits(t *testing.T) {
	t.Parallel()
	r := taskRepo{LandsBy: landsByPR, Source: "from its CLAUDE.md"}

	assert.Equal(t, "By its rule: pull request (its CLAUDE.md). a and b wait for this one.", r.ruleLine(nil, []string{"a", "b"}))
}

func TestRemoteHostDropsSchemeUserAndSuffix(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "github.com/acme/ads", remoteHost("git@github.com:acme/ads.git"))
	assert.Equal(t, "github.com/acme/ads", remoteHost("https://github.com/acme/ads.git"))
}

func TestTildePathShortensHome(t *testing.T) {
	t.Setenv("HOME", "/home/me")
	assert.Equal(t, "~/src/ads", tildePath("/home/me/src/ads"))
	assert.Equal(t, "/src/ads", tildePath("/src/ads"))
}
