package main

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"
)

const (
	pickerSearchAbove = 20
	pickerRecentMax   = 6
	pickerTaskShown   = 2
	pickerOtherShown  = 3
	pickerSearchStale = 2

	pickerCounted    = "%s · %d"
	pickerWordJoin   = " · "
	pickerMore       = "%d more · type to find one"
	pickerShow       = "show %d"
	pickerCleanAll   = "Clean up worktrees…"
	pickerCleanSome  = "Clean up %d…"
	pickerAddFolder  = "Add a folder to %s…"
	pickerRemove     = "Remove from %s…"
	pickerFilter     = "Filter %s"
	pickerSearchHint = "Matches folder, branch, task and session names. Live first, then recent, then the rest; stale last."
	pickerOneFolder  = "Switch worktree or branch"
	pickerManyFolder = "Switch folder, worktree or branch"

	sectionNow     = "Now"
	sectionLive    = "Live"
	sectionRecent  = "Recent"
	sectionTask    = "By task"
	sectionOther   = "Others"
	sectionHand    = "Made by hand"
	sectionStale   = "Stale"
	sectionFolders = "Folders"

	hintLive    = "a session runs or waits here"
	hintRecent  = "opened in the last 7 days"
	hintTask    = "sessions closed · newest first"
	hintHand    = "never cleaned up for you"
	hintStale   = "merged, or no commits for 30 d"
	hintFolders = "→ opens one"

	wordYou       = "you"
	wordStale     = "stale"
	wordToday     = "today"
	wordYesterday = "yesterday"
	daysFormat    = "%d d"
	weeksFormat   = "%d w"
	monthsFormat  = "%d mo"
	daysPerWeek   = 7
	daysPerMonth  = 30
	daysAsDays    = 14
	daysAsWeeks   = 60

	captionSince   = "since %s"
	captionAdded   = "added by %s · %s"
	captionAfk     = "AFK · %s"
	bylineAdded    = "added by %s"
	worktreeOne    = "1 worktree"
	worktreeMany   = "%d worktrees"
	folderOne      = "1 folder"
	folderMany     = "%d folders"
	liveFormat     = "%d live"
	dateLayout     = "Jan 2"
	todayLayout    = "15:04"
	branchIcon     = "arrow.triangle.branch"
	folderCaptions = "today %s"
)

var (
	glyphTones = []string{"accent", "warning", "danger"}
	alarmTones = []string{"warning", "danger"}
)

type pickerSection struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Hint  string `json:"hint,omitempty"`
	Rows  []int  `json:"rows,omitempty"`
	Shown int    `json:"shown"`
	More  string `json:"more,omitempty"`
	Fold  string `json:"fold,omitempty"`
	Clean string `json:"clean,omitempty"`
}

type pickerAddRepos struct {
	Folder string `json:"folder"`
	Title  string `json:"title"`
}

type worktreePicker struct {
	Placeholder string           `json:"placeholder"`
	Count       string           `json:"count,omitempty"`
	ByFolder    bool             `json:"byFolder"`
	AddFolder   string           `json:"addFolder"`
	AddRepos    []pickerAddRepos `json:"addRepos"`
	Remove      string           `json:"remove"`
	Cleanup     string           `json:"cleanup,omitempty"`
	SearchHint  string           `json:"searchHint"`
	SearchStale int              `json:"searchStale"`
	Sections    []pickerSection  `json:"sections"`
}

func buildPicker(workspace string, folders []worktreeFolder, rows []worktreeRow) worktreePicker {
	p := worktreePicker{
		Placeholder: pickerManyFolder,
		ByFolder:    len(rows) <= pickerSearchAbove,
		AddFolder:   fmt.Sprintf(pickerAddFolder, workspace),
		AddRepos:    []pickerAddRepos{},
		Remove:      fmt.Sprintf(pickerRemove, workspace),
		SearchHint:  pickerSearchHint,
		SearchStale: pickerSearchStale,
	}
	if len(folders) <= 1 {
		p.Placeholder = pickerOneFolder
	}
	if slices.ContainsFunc(rows, func(w worktreeRow) bool { return w.Stale || w.ByHand }) {
		p.Cleanup = pickerCleanAll
	}
	if !p.ByFolder {
		p.Count = strings.Join([]string{countWord(len(folders), folderOne, folderMany), countWord(len(rows), worktreeOne, worktreeMany)}, pickerWordJoin)
	}
	for i := range folders {
		folders[i].Summary = folderSummary(folders[i])
		folders[i].Filter = fmt.Sprintf(pickerFilter, folders[i].Name)
		if p.ByFolder {
			folders[i].Rows = folderRows(folders[i].Path, rows)
		} else {
			folders[i].Sections = folderSections(folders[i].Path, rows)
		}
	}
	if !p.ByFolder {
		p.Sections = homeSections(rows, len(folders))
	}
	return p
}

func homeSections(rows []worktreeRow, folders int) []pickerSection {
	groups := map[string][]int{}
	for i, w := range rows {
		groups[w.Group] = append(groups[w.Group], i)
	}
	out := []pickerSection{}
	if now := groups[groupNow]; len(now) > 0 {
		out = append(out, pickerSection{ID: groupNow, Title: sectionNow, Rows: now, Shown: len(now)})
	}
	out = appendSection(out, listed(groupLive, sectionLive, hintLive, groups[groupLive], len(groups[groupLive])))
	recent := groups[groupRecent]
	if len(recent) > pickerRecentMax {
		recent = recent[:pickerRecentMax]
	}
	out = appendSection(out, listed(groupRecent, sectionRecent, hintRecent, recent, len(recent)))
	return append(out, pickerSection{ID: "folders", Title: fmt.Sprintf(pickerCounted, sectionFolders, folders), Hint: hintFolders})
}

func folderSections(folder string, rows []worktreeRow) []pickerSection {
	groups := map[string][]int{}
	for i, w := range rows {
		if w.Repo != folder {
			continue
		}
		group := w.Group
		if group == groupNow {
			group = groupRecent
			if w.Live {
				group = groupLive
			}
		}
		groups[group] = append(groups[group], i)
	}
	live := groups[groupLive]
	return slices.DeleteFunc([]pickerSection{
		listed(groupLive, sectionLive, "", live, len(live)),
		listed(groupRecent, sectionRecent, "", groups[groupRecent], pickerRecentMax),
		listed(groupTask, sectionTask, hintTask, groups[groupTask], pickerTaskShown),
		listed(groupOther, sectionOther, "", groups[groupOther], pickerOtherShown),
		folded(groupHand, sectionHand, hintHand, groups[groupHand]),
		staleSection(groups[groupStale]),
	}, func(s pickerSection) bool { return len(s.Rows) == 0 })
}

func appendSection(out []pickerSection, s pickerSection) []pickerSection {
	if len(s.Rows) == 0 {
		return out
	}
	return append(out, s)
}

func listed(id, title, hint string, rows []int, shown int) pickerSection {
	s := pickerSection{ID: id, Title: fmt.Sprintf(pickerCounted, title, len(rows)), Hint: hint, Rows: rows, Shown: min(shown, len(rows))}
	if left := len(rows) - s.Shown; left > 0 {
		s.More = fmt.Sprintf(pickerMore, left)
	}
	return s
}

func folded(id, title, hint string, rows []int) pickerSection {
	s := listed(id, title, hint, rows, 0)
	s.More = ""
	s.Fold = fmt.Sprintf(pickerShow, len(rows))
	return s
}

func staleSection(rows []int) pickerSection {
	s := folded(groupStale, sectionStale, hintStale, rows)
	s.Clean = fmt.Sprintf(pickerCleanSome, len(rows))
	return s
}

func folderRows(folder string, rows []worktreeRow) []int {
	out := []int{}
	for i, w := range rows {
		if w.Repo == folder {
			out = append(out, i)
		}
	}
	slices.SortStableFunc(out, func(x, y int) int { return mainFirst(rows[x]) - mainFirst(rows[y]) })
	return out
}

func mainFirst(w worktreeRow) int {
	if w.Main {
		return 0
	}
	return 1
}

func (w *worktreeRow) describe(look repoLook, now time.Time) {
	w.Icon = branchIcon
	age := ageWord(now.Sub(w.lastTouched()))
	switch {
	case w.Live:
		w.Icon, w.Tone = look.icon, look.tone
		w.Detail = w.Task.Title + pickerWordJoin + w.Task.State
	case w.Group == groupNow:
	case w.Group == groupRecent:
		w.Detail = ageWord(now.Sub(w.LastOpened))
		if who := w.who(); who != "" {
			w.Detail = who + pickerWordJoin + w.Detail
		}
	case w.Group == groupTask:
		w.Icon = statusIcons[statusDone]
		w.Detail = strings.Join([]string{w.Task.Title, w.Task.State, age}, pickerWordJoin)
	case w.Group == groupStale:
		w.Icon = statusIcons[statusDone]
		w.Detail = wordStale + pickerWordJoin + age
		if w.Merged {
			w.Detail = wordMerged + pickerWordJoin + age
		}
	case w.Group == groupHand:
		w.Detail = wordYou + pickerWordJoin + age
	default:
		w.Detail = age
	}
	if !w.Live && slices.Contains(alarmTones, look.tone) {
		w.Icon, w.Tone = look.icon, look.tone
	}
}

func (w *worktreeRow) who() string {
	switch {
	case w.Main:
		return ""
	case w.Task != nil:
		return w.Task.Title
	}
	return wordYou
}

func ageWord(age time.Duration) string {
	days := int(age / (24 * time.Hour))
	switch {
	case days <= 0:
		return wordToday
	case days == 1:
		return wordYesterday
	case days < daysAsDays:
		return fmt.Sprintf(daysFormat, days)
	case days < daysAsWeeks:
		return fmt.Sprintf(weeksFormat, days/daysPerWeek)
	}
	return fmt.Sprintf(monthsFormat, days/daysPerMonth)
}

func taskGlyph(rec record, now time.Time) (string, string) {
	best := repoLook{}
	for _, e := range rec.repoEntries() {
		look := lookOf(e, nil, now)
		if glyphRank(look) > glyphRank(best) {
			best = look
		}
	}
	return cmp.Or(best.icon, statusIcons[statusDone]), best.tone
}

func glyphRank(look repoLook) int {
	return slices.Index(glyphTones, look.tone) + 1
}

func openSessionOf(f worktreeFolder, rows []worktreeRow, facts []sessionFact) string {
	if f.AddedBy != "" {
		for _, fact := range facts {
			if fact.rec.Title == f.AddedBy {
				return fact.rec.ID
			}
		}
	}
	for _, w := range rows {
		if w.Repo == f.Path && w.Session != nil {
			return w.Session.ID
		}
	}
	return ""
}

func folderWords(f worktreeFolder, first bool, now time.Time) (string, string) {
	if f.AddedAt.IsZero() {
		return "", ""
	}
	date := folderDate(f.AddedAt, now)
	switch {
	case f.AddedAfk:
		return fmt.Sprintf(captionAfk, date), fmt.Sprintf(bylineAdded, f.AddedBy)
	case first:
		return fmt.Sprintf(captionSince, date), ""
	case f.AddedBy == "":
		return fmt.Sprintf(captionAdded, wordYou, date), ""
	}
	return fmt.Sprintf(captionAdded, f.AddedBy, date), ""
}

func folderDate(at, now time.Time) string {
	if y, m, d := at.Date(); y == now.Year() && m == now.Month() && d == now.Day() {
		return fmt.Sprintf(folderCaptions, at.Format(todayLayout))
	}
	return at.Format(dateLayout)
}

func folderSummary(f worktreeFolder) string {
	summary := countWord(f.Worktrees, worktreeOne, worktreeMany)
	if f.Live > 0 {
		summary += pickerWordJoin + fmt.Sprintf(liveFormat, f.Live)
	}
	return summary
}

func countWord(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return fmt.Sprintf(many, n)
}
