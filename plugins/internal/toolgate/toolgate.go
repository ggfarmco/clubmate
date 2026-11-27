package toolgate

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"clubmate/plugins/internal/bashcmd"
)

const (
	Bash        = "Bash"
	HowCommand  = "command"
	HowStarts   = "starts"
	HowContains = "contains"
	HowRegex    = "regex"
	ViaUnread   = "unread"
	RulesFile   = "rules.json"
	AskAllDir   = "all"
	MarksDir    = "marks"
	FindingsDir = "findings"
	ToldDir     = "told"
	QueueSuffix = ".queue"
	MessagesDir = "messages"
	BatchArg    = "batch"
	git         = "git"
	xargs       = "xargs"
	eval        = "eval"
	shellFlag   = " -c"
	fishCommand = "--command"
	mentionCuts = "\"'();&|<>{}$=`"
	nestMax     = 8
)

var Hows = []string{HowCommand, HowStarts, HowContains, HowRegex}

func SafeID(id string) bool {
	return id != "" && id != "." && id != ".." && !strings.ContainsAny(id, `/\`)
}

const (
	fromNone = iota
	fromFlag
	fromScript
	fromStdin
)

var (
	shellWords    = []string{"if", "then", "else", "elif", "while", "until", "do", "{", "!"}
	bareWrappers  = []string{"time", "sudo", "command", "exec", "nohup", "env"}
	shells        = []string{"sh", "bash", "zsh", "dash", "ksh", "fish"}
	stdinFiles    = []string{"/dev/stdin", "/dev/fd/0", "/proc/self/fd/0"}
	shellValued   = []string{"-o", "+o", "-O", "+O", "--rcfile", "--init-file"}
	valuedOptions = map[string][]string{
		git:  {"-C", "-c", "--git-dir", "--work-tree", "--namespace", "--config-env", "--attr-source"},
		"gh": {"-R", "--repo"},
	}
	wrappers = map[string]wrapper{
		"sudo":    {short: "ugpCDrtTURh", long: []string{"--user", "--group", "--prompt", "--close-from", "--chdir", "--host", "--role", "--type", "--command-timeout", "--other-user", "--chroot"}},
		"timeout": {short: "sk", long: []string{"--signal", "--kill-after"}, positional: 1},
		"env":     {short: "uCPS", long: []string{"--unset", "--chdir", "--split-string"}, assigns: true, split: true, dash: true},
		"time":    {short: "of", long: []string{"--output", "--format"}},
		"nohup":   {},
		"command": {},
		"exec":    {short: "a"},
	}
	xargsOptions = wrapper{short: "InPLsdEaJRS", long: []string{"--replace", "--max-args", "--max-procs", "--max-lines", "--max-chars", "--delimiter", "--eof", "--arg-file"}}
)

type Rule struct {
	ID   string `json:"id"`
	Tool string `json:"tool"`
	How  string `json:"how,omitempty"`
	Text string `json:"text,omitempty"`
}

type Hit struct {
	Rule
	Via string
	At  int
}

type wrapper struct {
	short      string
	long       []string
	positional int
	assigns    bool
	split      bool
	dash       bool
}

func (r Rule) Matches(tool string, input json.RawMessage) bool {
	_, ok := r.match(tool, Input(tool, input))
	return ok
}

func Hits(rules []Rule, tool string, input json.RawMessage) []Hit {
	text := Input(tool, input)
	var hits []Hit
	for _, r := range rules {
		if h, ok := r.match(tool, text); ok {
			hits = append(hits, h)
		}
	}
	slices.SortStableFunc(hits, func(x, y Hit) int { return cmp.Compare(x.At, y.At) })
	return hits
}

func All(rules []Rule, tool string, input json.RawMessage) []Rule {
	hits := Hits(rules, tool, input)
	out := make([]Rule, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.Rule)
	}
	return out
}

func Input(tool string, input json.RawMessage) string {
	if tool == Bash {
		var fields map[string]json.RawMessage
		var command *string
		if json.Unmarshal(input, &fields) == nil && json.Unmarshal(fields["command"], &command) == nil && command != nil {
			return trimCommand(*command)
		}
	}
	dec := json.NewDecoder(bytes.NewReader(input))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return string(bytes.TrimSpace(input))
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return string(bytes.TrimSpace(input))
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func trimCommand(command string) string {
	command = strings.TrimLeftFunc(command, unicode.IsSpace)
	trimmed := strings.TrimRightFunc(command, unicode.IsSpace)
	if len(trimmed) < len(command) && (len(trimmed)-len(strings.TrimRight(trimmed, `\`)))%2 == 1 {
		_, n := utf8.DecodeRuneInString(command[len(trimmed):])
		trimmed = command[:len(trimmed)+n]
	}
	return trimmed
}

func Key(gate, session, tool, input, head, tree string) string {
	h := sha256.New()
	for _, f := range []string{gate, session, tool, input, head, tree} {
		fmt.Fprintf(h, "%d:%s", len(f), f)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (r Rule) match(tool, text string) (Hit, bool) {
	at, via, ok := r.find(tool, text)
	return Hit{Rule: r, Via: via, At: at}, ok
}

func (r Rule) find(tool, text string) (int, string, bool) {
	if ok, err := path.Match(r.Tool, tool); err != nil || !ok {
		return 0, "", false
	}
	if r.Text == "" {
		return 0, "", true
	}
	switch r.How {
	case HowStarts:
		if tool == Bash {
			return startsAt(text, r.Text)
		}
	case HowCommand:
		if tool == Bash {
			return commandAt(text, strings.Fields(r.Text))
		}
	case HowContains:
		if i := strings.Index(text, r.Text); i >= 0 {
			return i, "", true
		}
	case HowRegex:
		re, err := regexp.Compile(r.Text)
		if err != nil {
			return 0, "", false
		}
		if loc := re.FindStringIndex(text); loc != nil {
			return loc[0], "", true
		}
	}
	return 0, "", false
}

func OneCommand(text string) bool {
	commands, ok := bashcmd.Parse(text)
	return ok && len(commands) == 1 && len(commands[0].Heredocs) == 0
}

func startsAt(text, rule string) (int, string, bool) {
	want, ok := bashcmd.Parse(rule)
	if !ok || len(want) != 1 {
		return 0, "", false
	}
	commands, read := bashcmd.Parse(text)
	for _, c := range commands {
		if startsWith(c.Words, want[0].Words) {
			return c.At, "", true
		}
	}
	if read {
		return 0, "", false
	}
	var words []string
	for _, t := range wordTokens(want[0].Words, want[0].Offsets) {
		words = append(words, t.text)
	}
	at, found := mention(rawTokens(text, 0), words)
	return at, ViaUnread, found
}

type matcher struct {
	want  []string
	depth int
	line  string
	stdin *lineMention
}

type lineMention struct {
	done bool
	at   int
	ok   bool
}

type token struct {
	text string
	at   int
}

func commandAt(text string, want []string) (int, string, bool) {
	commands, read := bashcmd.Parse(text)
	if !read {
		at, ok := mention(rawTokens(text, 0), want)
		return at, ViaUnread, ok
	}
	m := matcher{want: want, line: text, stdin: &lineMention{}}
	return m.commands(commands)
}

func (m matcher) commands(commands []bashcmd.Command) (int, string, bool) {
	at, via, ok := 0, "", false
	for _, c := range commands {
		if a, v, hit := m.command(c); hit && (!ok || a < at || a == at && v == "" && via != "") {
			at, via, ok = a, v, true
		}
	}
	return at, via, ok
}

func (m matcher) command(c bashcmd.Command) (int, string, bool) {
	words, offs, capped := unwrap(c.Words, c.Offsets)
	if capped {
		return c.At, ViaUnread, true
	}
	if len(m.want) == 0 || len(words) == 0 {
		return 0, "", false
	}
	if isCommand(words, m.want) {
		return c.At, "", true
	}
	name := path.Base(words[0])
	switch {
	case slices.Contains(shells, name):
		src, at, rest, from := shellSource(words[1:], offs[1:])
		if from == fromFlag {
			if a, ok := m.search(src, at, rest); ok {
				return a, name + shellFlag, true
			}
		}
		if from != fromStdin {
			break
		}
		for _, h := range c.Heredocs {
			if a, ok := m.search(h.Body, h.At, nil); ok {
				return a, name + " " + h.Op, true
			}
		}
		if len(c.Heredocs) == 0 {
			if a, ok := m.mentionLine(); ok {
				return a, name, true
			}
		}
	case name == eval && len(words) > 1:
		if a, ok := m.search(strings.Join(words[1:], " "), offs[1], nil); ok {
			return a, eval, true
		}
	case name == xargs:
		rest, roffs := xargsOptions.skip(words[1:], offs[1:], new(int))
		if len(rest) == 0 {
			break
		}
		if m.depth < nestMax {
			inner := m
			inner.depth++
			if a, _, ok := inner.command(bashcmd.Command{Words: rest, Offsets: roffs, At: roffs[0]}); ok {
				return a, xargs, true
			}
		}
		if a, ok := mention(wordTokens(dropMessages(rest, roffs)), m.want); ok {
			return a, xargs, true
		}
	}
	return 0, "", false
}

func (m matcher) search(text string, base int, extra []token) (int, bool) {
	commands, read := bashcmd.Parse(text)
	if !read || m.depth >= nestMax {
		return mention(append(rawTokens(text, base), extra...), m.want)
	}
	inner := matcher{want: m.want, depth: m.depth + 1, line: text, stdin: &lineMention{}}
	if at, _, ok := inner.commands(commands); ok {
		return base + at, true
	}
	var tokens []token
	for _, c := range commands {
		tokens = append(tokens, wordTokens(dropMessages(c.Words, c.Offsets))...)
	}
	for i := range tokens {
		tokens[i].at += base
	}
	return mention(append(tokens, extra...), m.want)
}

func (m matcher) mentionLine() (int, bool) {
	if !m.stdin.done {
		m.stdin.at, m.stdin.ok = mention(rawTokens(m.line, 0), m.want)
		m.stdin.done = true
	}
	return m.stdin.at, m.stdin.ok
}

func shellSource(args []string, offs []int) (string, int, []token, int) {
	flag, stdin, i := false, false, 0
options:
	for ; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			i++
			break options
		case a == "-":
			stdin = true
			break options
		case a == fishCommand:
			flag = true
		case slices.Contains(shellValued, a):
			i++
		case strings.HasPrefix(a, "--"):
		case strings.HasPrefix(a, "-") || strings.HasPrefix(a, "+"):
			flag = flag || a[0] == '-' && strings.Contains(a, "c")
			stdin = stdin || a[0] == '-' && strings.Contains(a, "s")
			i += strings.Count(a, "o") + strings.Count(a, "O")
		case !flag && (strings.HasPrefix(a, "$") || strings.HasPrefix(a, "`")):
		default:
			break options
		}
	}
	i = min(i, len(args))
	rest, offs := args[i:], offs[i:]
	switch {
	case flag && len(rest) > 0:
		return rest[0], offs[0], wordTokens(dropMessages(rest[1:], offs[1:])), fromFlag
	case flag:
		return "", 0, nil, fromNone
	case stdin || len(rest) == 0 || slices.Contains(stdinFiles, rest[0]):
		return "", 0, nil, fromStdin
	}
	return "", 0, nil, fromScript
}

func rawTokens(text string, base int) []token {
	var out []token
	start := -1
	for i, r := range text + " " {
		if unicode.IsSpace(r) || strings.ContainsRune(mentionCuts, r) {
			if start >= 0 {
				out = append(out, token{text: text[start:i], at: base + start})
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	return out
}

func wordTokens(words []string, offs []int) []token {
	var out []token
	for i, w := range words {
		out = append(out, rawTokens(w, offs[i])...)
	}
	return out
}

func dropMessages(words []string, offs []int) ([]string, []int) {
	var keep []string
	var at []int
	for i := 0; i < len(words); i++ {
		w := words[i]
		switch {
		case strings.HasPrefix(w, "--message="), strings.HasPrefix(w, "-m") && len(w) > 2 && !strings.HasPrefix(w, "--"):
			continue
		case w == "--message", len(w) > 1 && w[0] == '-' && w[1] != '-' && strings.HasSuffix(w, "m") && isLetters(w[1:]):
			keep, at = append(keep, w), append(at, offs[i])
			i++
			continue
		}
		keep, at = append(keep, w), append(at, offs[i])
	}
	return keep, at
}

func isLetters(s string) bool {
	return !strings.ContainsFunc(s, func(r rune) bool { return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') })
}

func mention(tokens []token, want []string) (int, bool) {
	if len(want) == 0 {
		return 0, false
	}
	slices.SortStableFunc(tokens, func(x, y token) int { return cmp.Compare(x.at, y.at) })
	i := slices.IndexFunc(tokens, func(t token) bool { return path.Base(t.text) == path.Base(want[0]) })
	if i < 0 {
		return 0, false
	}
	at := tokens[i].at
	tokens = tokens[i+1:]
	for _, w := range want[1:] {
		j := slices.IndexFunc(tokens, func(t token) bool { return t.text == w })
		if j < 0 {
			return 0, false
		}
		tokens = tokens[j+1:]
	}
	return at, true
}

func skipped(word string) bool {
	return assignment(word) || slices.Contains(shellWords, word) || slices.Contains(bareWrappers, word)
}

func startsWith(words, want []string) bool {
	for len(words) > 0 && skipped(words[0]) {
		words = words[1:]
	}
	return len(want) > 0 && len(words) >= len(want) && slices.Equal(words[:len(want)], want)
}

func isCommand(words, want []string) bool {
	if path.Base(words[0]) != want[0] {
		return false
	}
	valued := valuedOptions[want[0]]
	rest := words[1:]
	for _, w := range want[1:] {
		for len(rest) > 0 && strings.HasPrefix(rest[0], "-") {
			takes := slices.Contains(valued, rest[0])
			rest = rest[1:]
			if takes && len(rest) > 0 {
				rest = rest[1:]
			}
		}
		if len(rest) == 0 || rest[0] != w {
			return false
		}
		rest = rest[1:]
	}
	return true
}

func unwrap(words []string, offs []int) ([]string, []int, bool) {
	splits := 0
	for len(words) > 0 {
		switch {
		case assignment(words[0]) || slices.Contains(shellWords, words[0]):
			words, offs = words[1:], offs[1:]
		case words[0] == "function":
			n := min(2, len(words))
			words, offs = words[n:], offs[n:]
		case words[0] == "coproc":
			n := 1
			if len(words) > 2 && words[2] == "{" {
				n = 2
			}
			words, offs = words[n:], offs[n:]
		default:
			w, ok := wrappers[path.Base(words[0])]
			if !ok {
				return words, offs, false
			}
			words, offs = w.skip(words[1:], offs[1:], &splits)
			if splits > nestMax {
				return words, offs, true
			}
		}
	}
	return words, offs, false
}

func (w wrapper) skip(args []string, offs []int, splits *int) ([]string, []int) {
	for len(args) > 0 {
		a := args[0]
		if a == "--" {
			args, offs = args[1:], offs[1:]
			break
		}
		if w.assigns && assignment(a) || w.dash && a == "-" {
			args, offs = args[1:], offs[1:]
			continue
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			break
		}
		at := offs[0]
		args, offs = args[1:], offs[1:]
		name, value, next := w.option(a)
		if next && len(args) > 0 {
			value, at = args[0], offs[0]
			args, offs = args[1:], offs[1:]
		}
		if w.split && (name == "S" || name == "split-string") {
			if *splits++; *splits > nestMax {
				return args, offs
			}
			words, at := splitString(value, at)
			args = append(words, args...)
			offs = append(at, offs...)
		}
	}
	for w.assigns && len(args) > 0 && assignment(args[0]) {
		args, offs = args[1:], offs[1:]
	}
	n := min(w.positional, len(args))
	return args[n:], offs[n:]
}

func splitString(value string, base int) ([]string, []int) {
	commands, _ := bashcmd.Parse(value)
	if len(commands) == 0 {
		return nil, nil
	}
	offs := make([]int, 0, len(commands[0].Offsets))
	for _, o := range commands[0].Offsets {
		offs = append(offs, base+o)
	}
	return commands[0].Words, offs
}

func (w wrapper) option(flag string) (string, string, bool) {
	if long, ok := strings.CutPrefix(flag, "--"); ok {
		name, value, inline := strings.Cut(long, "=")
		return name, value, !inline && slices.Contains(w.long, flag)
	}
	i := strings.IndexAny(flag[1:], w.short)
	if i < 0 {
		return "", "", false
	}
	return flag[1+i : 2+i], flag[2+i:], i == len(flag)-2
}

func assignment(word string) bool {
	name, _, ok := strings.Cut(word, "=")
	if !ok || name == "" || name[0] >= '0' && name[0] <= '9' {
		return false
	}
	return !strings.ContainsFunc(name, func(r rune) bool {
		return !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9')
	})
}

func Readable(how, text string) string {
	if how == HowCommand {
		words := strings.Fields(text)
		words, _, _ = unwrap(words, make([]int, len(words)))
		if len(words) > 0 {
			words[0] = path.Base(words[0])
		}
		return strings.Join(words, " ")
	}
	commands, ok := bashcmd.Parse(text)
	if !ok || len(commands) != 1 {
		return text
	}
	c := commands[0]
	n := slices.IndexFunc(c.Words, func(w string) bool { return !skipped(w) })
	switch n {
	case -1:
		return ""
	case 0:
		return text
	}
	return text[c.Offsets[n]:]
}

func Accepts(how, text string) bool {
	if Readable(how, text) != text {
		return false
	}
	at, via, ok := Rule{Tool: Bash, How: how, Text: text}.find(Bash, text)
	return ok && via == "" && at == 0
}

func Suggest(how, text string) string {
	s := Readable(how, text)
	if how == HowCommand && len(strings.Fields(s)) < 2 {
		return ""
	}
	if how == HowStarts {
		commands, ok := bashcmd.Parse(text)
		if !ok || len(commands) != 1 {
			return ""
		}
		words, offs, _ := unwrap(commands[0].Words, commands[0].Offsets)
		if len(words) == 0 {
			return ""
		}
		s = text[offs[0]:]
	}
	if s == text || !Accepts(how, s) {
		return ""
	}
	return s
}
