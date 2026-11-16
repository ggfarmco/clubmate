package main

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

const (
	modeOff    = ""
	modeHours  = "hours"
	modeAgents = "agents"
	modeOn     = "on"

	kindYou   = "you"
	kindRule  = "rule"
	kindAgent = "agent"

	defaultHoldMinutes = 60
	maxHoldMinutes     = 720

	lidNote = "A closed lid still sleeps the Mac on battery."
)

var hourChoices = []int{1, 2, 4, 8, 24}

type settings struct {
	Battery bool `json:"battery"`
	Display bool `json:"display"`
}

type choice struct {
	Mode      string `json:"mode,omitempty"`
	Hours     int    `json:"hours,omitempty"`
	Until     int64  `json:"until,omitempty"`
	LastHours int    `json:"lastHours,omitempty"`
}

type power struct {
	Laptop    bool
	OnBattery bool
}

type agentHold struct {
	ID        string `json:"id"`
	Reason    string `json:"reason"`
	Until     int64  `json:"until"`
	PID       int    `json:"pid,omitempty"`
	Session   string `json:"session,omitempty"`
	Workspace string `json:"workspace,omitempty"`
}

type holdLine struct {
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Trailing  string `json:"trailing"`
	ID        string `json:"id,omitempty"`
	Session   string `json:"session,omitempty"`
	Workspace string `json:"workspace,omitempty"`
}

type hourOption struct {
	Hours int    `json:"hours"`
	Title string `json:"title"`
	Short string `json:"short"`
}

type hoursLabels struct {
	Title   string       `json:"title"`
	Active  string       `json:"active"`
	Options []hourOption `json:"options"`
}

type itemLabel struct {
	Title    string `json:"title"`
	Subtitle string `json:"subtitle,omitempty"`
}

type labels struct {
	Section string      `json:"section"`
	Picker  string      `json:"picker"`
	Hours   hoursLabels `json:"hours"`
	Agents  itemLabel   `json:"agents"`
	On      itemLabel   `json:"on"`
	Off     itemLabel   `json:"off"`
}

type stateView struct {
	On        bool       `json:"on"`
	Armed     bool       `json:"armed"`
	Text      string     `json:"text"`
	Detail    string     `json:"detail"`
	Mode      string     `json:"mode"`
	Hours     int        `json:"hours"`
	Until     int64      `json:"until"`
	Value     string     `json:"value"`
	Ends      string     `json:"ends"`
	Holds     []holdLine `json:"holds"`
	Labels    labels     `json:"labels"`
	Laptop    bool       `json:"laptop"`
	OnBattery bool       `json:"onBattery"`
	Battery   bool       `json:"battery"`
	Display   bool       `json:"display"`
}

type keeper struct {
	settings settings
	choice   choice
	holds    []agentHold
	active   map[string]int
	titles   map[string]string
	power    power
	holder   holder
	now      func() time.Time
	alive    func(pid int) bool
	err      error
	dirty    bool
}

func newKeeper(assert asserter, now func() time.Time, alive func(int) bool) *keeper {
	return &keeper{active: map[string]int{}, titles: map[string]string{}, holder: holder{assert: assert}, now: now, alive: alive}
}

func validHours(h int) bool {
	return slices.Contains(hourChoices, h)
}

func (k *keeper) choose(mode string, hours int) error {
	last := k.choice.LastHours
	switch mode {
	case modeHours:
		if !validHours(hours) {
			return fmt.Errorf("hours must be one of %v", hourChoices)
		}
		k.choice = choice{Mode: modeHours, Hours: hours, Until: k.now().Add(time.Duration(hours) * time.Hour).Unix(), LastHours: hours}
	case modeAgents, modeOn:
		k.choice = choice{Mode: mode, LastHours: last}
	case "off", modeOff:
		k.choice = choice{LastHours: last}
	default:
		return fmt.Errorf("unknown mode %q", mode)
	}
	k.dirty = true
	return nil
}

func (k *keeper) lastHours() int {
	if validHours(k.choice.LastHours) {
		return k.choice.LastHours
	}
	return hourChoices[0]
}

func (k *keeper) prune() bool {
	now := k.now().Unix()
	changed := false
	if k.choice.Mode == modeHours && k.choice.Until <= now {
		k.choice = choice{LastHours: k.choice.LastHours}
		changed = true
	}
	kept := k.holds[:0]
	for _, h := range k.holds {
		if h.Until <= now || h.PID > 0 && !k.alive(h.PID) {
			changed = true
			continue
		}
		kept = append(kept, h)
	}
	k.holds = kept
	return k.mark(changed)
}

func (k *keeper) dropSession(session string) bool {
	n := len(k.holds)
	k.holds = slices.DeleteFunc(k.holds, func(h agentHold) bool { return h.Session == session })
	return k.mark(len(k.holds) != n)
}

func (k *keeper) keepSessions(workspace string, open map[string]bool) bool {
	n := len(k.holds)
	k.holds = slices.DeleteFunc(k.holds, func(h agentHold) bool {
		return h.Workspace == workspace && h.Session != "" && !open[h.Session]
	})
	return k.mark(len(k.holds) != n)
}

func (k *keeper) dropWorkspace(workspace string) bool {
	n := len(k.holds)
	k.holds = slices.DeleteFunc(k.holds, func(h agentHold) bool { return h.Workspace == workspace })
	_, had := k.active[workspace]
	delete(k.active, workspace)
	return k.mark(len(k.holds) != n) || had
}

func (k *keeper) mark(changed bool) bool {
	k.dirty = k.dirty || changed
	return changed
}

func (k *keeper) working() int {
	n := 0
	for _, c := range k.active {
		n += c
	}
	return n
}

func (k *keeper) ruleHolds() bool {
	return k.choice.Mode == modeAgents && k.working() > 0 && (!k.power.OnBattery || k.settings.Battery)
}

func (k *keeper) choiceHolds() bool {
	switch k.choice.Mode {
	case modeHours, modeOn:
		return true
	case modeAgents:
		return k.ruleHolds()
	}
	return false
}

func (k *keeper) types() []string {
	if !k.choiceHolds() && len(k.holds) == 0 {
		return nil
	}
	return assertionTypes(k.settings.Display)
}

func (k *keeper) apply() {
	k.err = k.holder.want(k.types())
}

func (k *keeper) view() stateView {
	v := stateView{
		On:        k.holder.on(),
		Armed:     k.choice.Mode == modeAgents && !k.ruleHolds(),
		Mode:      k.choice.Mode,
		Hours:     k.choice.Hours,
		Until:     k.choice.Until,
		Value:     k.value(),
		Ends:      k.ends(),
		Holds:     k.lines(),
		Labels:    k.labels(),
		Laptop:    k.power.Laptop,
		OnBattery: k.power.OnBattery,
		Battery:   k.settings.Battery,
		Display:   k.settings.Display,
	}
	switch {
	case v.On:
		v.Text = k.text()
		v.Detail = k.awakeDetail()
	case k.err != nil:
		v.Detail = "Could not keep the Mac awake: " + k.err.Error() + "."
	default:
		v.Detail = k.asleepDetail()
	}
	return v
}

func (k *keeper) text() string {
	others := len(k.holds)
	var lead string
	switch {
	case k.choiceHolds() && k.choice.Mode == modeHours:
		lead = "Awake until " + k.clock(k.choice.Until)
	case k.choiceHolds() && k.choice.Mode == modeOn:
		lead = "Awake"
	case k.choiceHolds():
		lead = "Awake · " + plural(k.working(), "agent")
	case len(k.holds) > 0:
		lead = "Awake · " + k.holds[0].Reason
		others--
	default:
		return "Awake"
	}
	if others > 0 {
		lead += fmt.Sprintf(" +%d", others)
	}
	return lead
}

func (k *keeper) awakeDetail() string {
	var phrases []string
	if k.choiceHolds() {
		switch k.choice.Mode {
		case modeHours:
			phrases = append(phrases, "by you until "+k.clock(k.choice.Until))
		case modeOn:
			phrases = append(phrases, "by you until you turn it off")
		case modeAgents:
			verb := " work"
			if k.working() == 1 {
				verb = " works"
			}
			phrases = append(phrases, "while "+plural(k.working(), "agent")+verb)
		}
	}
	for _, h := range k.holds {
		phrases = append(phrases, "by an agent's "+h.Reason)
	}
	if len(phrases) == 1 && len(k.holds) == 1 {
		phrases[0] += " until it ends, " + k.clock(k.holds[0].Until) + " at the latest"
	}
	s := "Kept awake " + strings.Join(phrases, " and ")
	if k.ruleHolds() {
		if k.power.OnBattery {
			s += ", on battery too"
		} else {
			s += ", on power"
		}
	}
	s += "."
	if k.power.Laptop && k.power.OnBattery {
		s += " " + lidNote
	}
	return s
}

func (k *keeper) asleepDetail() string {
	if k.choice.Mode != modeAgents {
		return "The Mac sleeps as usual."
	}
	if k.working() > 0 && k.power.OnBattery && !k.settings.Battery {
		return "The Mac sleeps as usual: on battery, agents wait for power."
	}
	return "The Mac sleeps as usual until an agent is running or waiting for you."
}

func (k *keeper) value() string {
	switch k.choice.Mode {
	case modeHours:
		return "Until " + k.clock(k.choice.Until)
	case modeAgents:
		return "While agents work"
	case modeOn:
		return "Until turned off"
	}
	return "Off"
}

func (k *keeper) ends() string {
	if k.choice.Mode != modeHours {
		return ""
	}
	return "until " + k.clock(k.choice.Until)
}

func (k *keeper) lines() []holdLine {
	lines := []holdLine{}
	switch k.choice.Mode {
	case modeHours:
		lines = append(lines, holdLine{Kind: kindYou, Title: "You", Trailing: "for " + hoursWords(k.choice.Hours) + " · until " + k.clock(k.choice.Until)})
	case modeOn:
		lines = append(lines, holdLine{Kind: kindYou, Title: "You", Trailing: "until turned off"})
	case modeAgents:
		lines = append(lines, holdLine{Kind: kindRule, Title: "While agents work", Trailing: k.ruleTrailing()})
	}
	for _, h := range k.holds {
		trailing := "≤ " + k.clock(h.Until)
		if t := k.titles[h.Session]; t != "" {
			trailing = t + " · " + trailing
		}
		lines = append(lines, holdLine{Kind: kindAgent, Title: h.Reason, Trailing: trailing, ID: h.ID, Session: h.Session, Workspace: h.Workspace})
	}
	return lines
}

func (k *keeper) ruleTrailing() string {
	n := k.working()
	switch {
	case n == 0:
		return "no agent working"
	case !k.ruleHolds():
		return "waits for power · " + plural(n, "session")
	}
	return plural(n, "session")
}

func (k *keeper) labels() labels {
	l := labels{
		Section: "Keep Awake",
		Picker:  "Keep awake",
		Hours:   hoursLabels{Title: "For Hours", Active: "For Hours"},
		Agents:  itemLabel{Title: "While Agents Work", Subtitle: "Running or waiting for you"},
		On:      itemLabel{Title: "Until Turned Off"},
		Off:     itemLabel{Title: "Turn Off"},
	}
	for _, h := range hourChoices {
		l.Hours.Options = append(l.Hours.Options, hourOption{Hours: h, Title: titleCase(hoursWords(h)), Short: fmt.Sprintf("%d h", h)})
	}
	if k.choice.Mode == modeHours {
		l.Hours.Active = "For " + titleCase(hoursWords(k.choice.Hours))
	}
	return l
}

func (k *keeper) clock(unix int64) string {
	t := time.Unix(unix, 0).In(k.now().Location())
	now := k.now()
	if t.YearDay() == now.YearDay() && t.Year() == now.Year() {
		return t.Format("15:04")
	}
	return t.Format("Mon 15:04")
}

func hoursWords(h int) string {
	if h == 1 {
		return "1 hour"
	}
	return fmt.Sprintf("%d hours", h)
}

func titleCase(s string) string {
	n, word, _ := strings.Cut(s, " ")
	return n + " " + strings.ToUpper(word[:1]) + word[1:]
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
