package main

import (
	"cmp"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	statusIdea       = "idea"
	statusTodo       = "todo"
	statusInProgress = "in_progress"
	statusDone       = "done"
	statusNotNow     = "not_now"

	priorityHigh   = "high"
	priorityNormal = "normal"
	priorityLow    = "low"

	maxTitleChars = 200
	maxDescBytes  = 32 << 10
	maxTagChars   = 32
	maxTags       = 10
	maxLinks      = 20
	maxAttach     = 20
	maxRefChars   = 2048
	maxMoves      = 100

	linkBlockedBy = "blocked_by"
	linkBlocks    = "blocks"
	linkRelates   = "relates"

	attachFile     = "file"
	attachArtifact = "artifact"
	attachImage    = "image"
	attachURL      = "url"

	whyCreated   = "created"
	whyStarted   = "started"
	whyClaimed   = "claimed"
	whyReleased  = "released"
	whyDone      = "done"
	whyTakenBack = "taken back"
	whyStopped   = "stopped"
)

var (
	errTitleEmpty   = errors.New("title must not be empty")
	errTitleLine    = errors.New("title must be one line")
	errTitleLong    = fmt.Errorf("title must be at most %d characters", maxTitleChars)
	errDescLong     = fmt.Errorf("description must be at most %d KiB", maxDescBytes>>10)
	errBadStatus    = errors.New("status must be one of idea, todo, in_progress, done, not_now")
	errBadPriority  = errors.New("priority must be high, normal or low")
	errTaskNotFound = errors.New("task not found")
	errTagChars     = errors.New("a tag must be one word of letters, digits, - _ . or /")
	errTagLong      = fmt.Errorf("a tag must be at most %d characters", maxTagChars)
	errTooManyTags  = fmt.Errorf("a task takes at most %d tags", maxTags)
	errLinkKind     = errors.New("a link kind must be blocked_by, blocks or relates")
	errLinkSelf     = errors.New("a task cannot link to itself")
	errLinkTwice    = errors.New("a task links to another task once")
	errTooManyLinks = fmt.Errorf("a task takes at most %d links", maxLinks)
	errAttachKind   = errors.New("an attachment kind must be file, artifact, image or url")
	errAttachRef    = fmt.Errorf("an attachment needs a ref of at most %d characters", maxRefChars)
	errAttachPath   = errors.New("a file is a path relative to the worktree, without ..")
	errAttachWhere  = errors.New("an image or artifact is a path relative to the worktree or an http(s) URL")
	errAttachURL    = errors.New("a url attachment is an http(s) URL")
	errTooManyFiles = fmt.Errorf("a task takes at most %d attachments", maxAttach)
	errLinkText     = errors.New("write a link as blocks #3, blocked by #5 or relates #7")
)

type Task struct {
	ID            int          `json:"id"`
	Title         string       `json:"title"`
	Description   string       `json:"description,omitempty"`
	Status        string       `json:"status"`
	Priority      string       `json:"priority"`
	Tags          []string     `json:"tags,omitempty"`
	Links         []Link       `json:"links,omitempty"`
	Attachments   []Attachment `json:"attachments,omitempty"`
	Owners        []OwnerMove  `json:"owners,omitempty"`
	ClaimedBy     string       `json:"claimedBy,omitempty"`
	ClaimedAt     time.Time    `json:"claimedAt,omitzero"`
	ClaimedFrom   string       `json:"claimedFrom,omitempty"`
	LastClaimedBy string       `json:"lastClaimedBy,omitempty"`
	Note          string       `json:"note,omitempty"`
	CreatedBy     string       `json:"createdBy"`
	CreatedAt     time.Time    `json:"createdAt"`
	UpdatedAt     time.Time    `json:"updatedAt"`
}

type Link struct {
	Kind string `json:"kind"`
	Task int    `json:"task"`
}

type Attachment struct {
	Kind  string `json:"kind"`
	Ref   string `json:"ref"`
	Title string `json:"title,omitempty"`
}

type OwnerMove struct {
	Owner    string    `json:"owner"`
	Title    string    `json:"title,omitempty"`
	Run      string    `json:"run,omitempty"`
	RunTitle string    `json:"runTitle,omitempty"`
	At       time.Time `json:"at,omitzero"`
	Why      string    `json:"why"`
	Failure  string    `json:"failure,omitempty"`
}

func inverse(kind string) string {
	switch kind {
	case linkBlocks:
		return linkBlockedBy
	case linkBlockedBy:
		return linkBlocks
	}
	return kind
}

func cleanLinks(self int, links []Link) ([]Link, error) {
	out := make([]Link, 0, len(links))
	for _, l := range links {
		switch {
		case l.Kind != linkBlockedBy && l.Kind != linkBlocks && l.Kind != linkRelates:
			return nil, errLinkKind
		case l.Task == self:
			return nil, errLinkSelf
		case slices.ContainsFunc(out, func(o Link) bool { return o.Task == l.Task }):
			return nil, fmt.Errorf("%w: #%d", errLinkTwice, l.Task)
		}
		out = append(out, l)
	}
	if len(out) > maxLinks {
		return nil, errTooManyLinks
	}
	return out, nil
}

func cleanAttachments(in []Attachment) ([]Attachment, error) {
	out := make([]Attachment, 0, len(in))
	for _, a := range in {
		a.Ref, a.Title = strings.TrimSpace(a.Ref), strings.TrimSpace(a.Title)
		if a.Ref == "" || utf8.RuneCountInString(a.Ref) > maxRefChars || strings.ContainsAny(a.Ref, "\r\n") {
			return nil, errAttachRef
		}
		switch a.Kind {
		case attachFile:
			if !relativePath(a.Ref) {
				return nil, errAttachPath
			}
		case attachURL:
			if !webURL(a.Ref) {
				return nil, errAttachURL
			}
		case attachImage, attachArtifact:
			if !webURL(a.Ref) && !relativePath(a.Ref) {
				return nil, errAttachWhere
			}
		default:
			return nil, errAttachKind
		}
		if !slices.Contains(out, a) {
			out = append(out, a)
		}
	}
	if len(out) > maxAttach {
		return nil, errTooManyFiles
	}
	return out, nil
}

func imageExt(ref string) bool {
	ext := strings.ToLower(filepath.Ext(strings.SplitN(ref, "?", 2)[0]))
	return slices.Contains([]string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".heic"}, ext)
}

func relativePath(p string) bool {
	if filepath.IsAbs(p) || strings.HasPrefix(p, "~") || strings.Contains(p, "://") {
		return false
	}
	clean := filepath.Clean(p)
	return clean != ".." && !strings.HasPrefix(clean, "../")
}

func webURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func (t *Task) move(m OwnerMove) {
	if m.At.IsZero() {
		m.At = time.Now().UTC()
	}
	t.Owners = appendMove(t.Owners, m)
	if over := len(t.Owners) - maxMoves; over > 0 {
		t.Owners = slices.Delete(t.Owners, 1, over+1)
	}
}

func appendMove(moves []OwnerMove, m OwnerMove) []OwnerMove {
	if m.Owner == "" {
		m.Owner = userOwner
	}
	if n := len(moves); n > 0 && moves[n-1].Owner == m.Owner {
		return moves
	}
	return append(moves, m)
}

func (m OwnerMove) failed() bool {
	return m.Why == whyStopped && m.Failure != ""
}

func claimFailure(t Task) string {
	for _, m := range slices.Backward(t.Owners) {
		if m.Owner == t.ClaimedBy {
			return m.Failure
		}
	}
	return ""
}

func ownerHistory(t Task) []OwnerMove {
	if len(t.Owners) > 0 {
		return t.Owners
	}
	moves := []OwnerMove{{Owner: userOwner, At: t.CreatedAt, Why: whyCreated}}
	if t.LastClaimedBy != "" {
		moves = appendMove(moves, OwnerMove{Owner: t.LastClaimedBy, Why: whyClaimed})
	}
	if t.ClaimedBy != "" {
		return appendMove(moves, OwnerMove{Owner: t.ClaimedBy, At: t.ClaimedAt, Why: whyClaimed})
	}
	return appendMove(moves, OwnerMove{Owner: userOwner, Why: whyReleased})
}

func validStatus(s string) bool {
	return slices.Contains([]string{statusIdea, statusTodo, statusInProgress, statusDone, statusNotNow}, s)
}

func validPriority(p string) bool {
	return slices.Contains([]string{priorityHigh, priorityNormal, priorityLow}, p)
}

func cleanTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	switch {
	case title == "":
		return "", errTitleEmpty
	case strings.ContainsAny(title, "\r\n"):
		return "", errTitleLine
	case utf8.RuneCountInString(title) > maxTitleChars:
		return "", errTitleLong
	}
	return title, nil
}

func cleanTags(tags []string) ([]string, error) {
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(tag), "#"))
		switch {
		case tag == "" || slices.Contains(out, tag):
			continue
		case strings.ContainsFunc(tag, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("-_./", r)
		}):
			return nil, errTagChars
		case utf8.RuneCountInString(tag) > maxTagChars:
			return nil, errTagLong
		}
		out = append(out, tag)
	}
	if len(out) > maxTags {
		return nil, errTooManyTags
	}
	return out, nil
}

func splitTags(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool { return r == ',' || unicode.IsSpace(r) })
}

func checkDescription(d string) error {
	if len(d) > maxDescBytes {
		return errDescLong
	}
	return nil
}

func priorityRank(p string) int {
	switch p {
	case priorityHigh:
		return 0
	case priorityLow:
		return 2
	}
	return 1
}

func sortTasks(tasks []Task) {
	slices.SortFunc(tasks, func(a, b Task) int {
		aDone, bDone := a.Status == statusDone, b.Status == statusDone
		switch {
		case aDone != bDone:
			if aDone {
				return 1
			}
			return -1
		case aDone:
			return cmp.Or(b.UpdatedAt.Compare(a.UpdatedAt), cmp.Compare(b.ID, a.ID))
		}
		return cmp.Or(cmp.Compare(priorityRank(a.Priority), priorityRank(b.Priority)), cmp.Compare(a.ID, b.ID))
	})
}

func parseLink(text string) (Link, error) {
	rest := strings.ToLower(strings.TrimSpace(text))
	kind := linkRelates
	for _, p := range []struct{ prefix, kind string }{{"blocked by", linkBlockedBy}, {"blocked_by", linkBlockedBy}, {"blocks", linkBlocks}, {"relates to", linkRelates}, {"relates", linkRelates}} {
		if r, ok := strings.CutPrefix(rest, p.prefix); ok {
			rest, kind = r, p.kind
			break
		}
	}
	id, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(rest), "#"))
	if err != nil || id <= 0 {
		return Link{}, errLinkText
	}
	return Link{Kind: kind, Task: id}, nil
}

func parseAttachment(text string) Attachment {
	ref := strings.TrimSpace(text)
	ext := strings.ToLower(filepath.Ext(strings.SplitN(ref, "?", 2)[0]))
	switch {
	case webURL(ref) && strings.Contains(ref, "/artifact/"):
		return Attachment{Kind: attachArtifact, Ref: ref}
	case imageExt(ref):
		return Attachment{Kind: attachImage, Ref: ref}
	case webURL(ref):
		return Attachment{Kind: attachURL, Ref: ref}
	case ext == ".html":
		return Attachment{Kind: attachArtifact, Ref: ref}
	}
	return Attachment{Kind: attachFile, Ref: ref}
}

func matches(t Task, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	if id, err := strconv.Atoi(strings.TrimPrefix(query, "#")); err == nil && id == t.ID {
		return true
	}
	for _, field := range append([]string{t.Title, t.Description, t.Note}, t.Tags...) {
		if strings.Contains(strings.ToLower(field), query) {
			return true
		}
	}
	return false
}
