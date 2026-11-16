package main

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

const (
	kindManual = "manual"
	kindAgents = "agents"
	kindAgent  = "agent"

	defaultHoldMinutes = 60
	maxHoldMinutes     = 720

	lidNote = "A closed lid still sleeps the Mac on battery."
)

type settings struct {
	Agents  bool `json:"agents"`
	Battery bool `json:"battery"`
	Display bool `json:"display"`
}

var defaultSettings = settings{}

type manualHold struct {
	On    bool  `json:"on"`
	Until int64 `json:"until,omitempty"`
}

type agentHold struct {
	ID        string `json:"id"`
	Reason    string `json:"reason"`
	Until     int64  `json:"until"`
	PID       int    `json:"pid,omitempty"`
	Session   string `json:"session,omitempty"`
	Workspace string `json:"workspace,omitempty"`
}

type holdView struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Label     string `json:"label"`
	Until     int64  `json:"until,omitempty"`
	PID       int    `json:"pid,omitempty"`
	Session   string `json:"session,omitempty"`
	Workspace string `json:"workspace,omitempty"`
}

type stateView struct {
	On      bool       `json:"on"`
	Text    string     `json:"text"`
	Detail  string     `json:"detail"`
	Manual  bool       `json:"manual"`
	Until   int64      `json:"until"`
	Holds   []holdView `json:"holds"`
	Agents  bool       `json:"agents"`
	Battery bool       `json:"battery"`
	Display bool       `json:"display"`
}

type keeper struct {
	settings settings
	manual   manualHold
	holds    []agentHold
	active   map[string]int
	holder   holder
	now      func() time.Time
	alive    func(pid int) bool
	err      error
	dirty    bool
}

func newKeeper(assert asserter, now func() time.Time, alive func(int) bool) *keeper {
	return &keeper{settings: defaultSettings, active: map[string]int{}, holder: holder{assert: assert}, now: now, alive: alive}
}

func (k *keeper) prune() bool {
	now := k.now().Unix()
	changed := false
	if k.manual.On && k.manual.Until > 0 && k.manual.Until <= now {
		k.manual = manualHold{}
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
	k.dirty = k.dirty || changed
	return changed
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

func (k *keeper) agentsWorking() int {
	if !k.settings.Agents {
		return 0
	}
	n := 0
	for _, c := range k.active {
		n += c
	}
	return n
}

func (k *keeper) types() []string {
	agents := k.agentsWorking() > 0
	if !k.manual.On && len(k.holds) == 0 && !agents {
		return nil
	}
	battery := k.manual.On || len(k.holds) > 0 || k.settings.Battery
	return assertionTypes(battery, k.settings.Display)
}

func (k *keeper) apply() {
	k.err = k.holder.want(k.types())
}

func (k *keeper) view() stateView {
	v := stateView{
		On:      k.holder.on(),
		Manual:  k.manual.On,
		Until:   k.manual.Until,
		Holds:   []holdView{},
		Agents:  k.settings.Agents,
		Battery: k.settings.Battery,
		Display: k.settings.Display,
	}
	var texts, labels []string
	if k.manual.On {
		v.Holds = append(v.Holds, holdView{ID: kindManual, Kind: kindManual, Label: "You, " + k.untilWords(k.manual.Until), Until: k.manual.Until})
		texts = append(texts, "Awake"+k.untilShort(k.manual.Until))
		labels = append(labels, "you "+k.untilWords(k.manual.Until))
	}
	for _, h := range k.holds {
		v.Holds = append(v.Holds, holdView{ID: h.ID, Kind: kindAgent, Label: h.Reason, Until: h.Until, PID: h.PID, Session: h.Session, Workspace: h.Workspace})
		texts = append(texts, "Awake · "+h.Reason)
		labels = append(labels, fmt.Sprintf("an agent's %s until %s", h.Reason, k.clock(h.Until)))
	}
	if n := k.agentsWorking(); n > 0 {
		label := plural(n, "agent") + " working"
		v.Holds = append(v.Holds, holdView{ID: kindAgents, Kind: kindAgents, Label: label})
		texts = append(texts, "Awake · "+plural(n, "agent"))
		labels = append(labels, label)
	}
	switch {
	case v.On && len(texts) > 0:
		v.Text = texts[0]
		if len(texts) > 1 {
			v.Text += fmt.Sprintf(" +%d", len(texts)-1)
		}
		v.Detail = k.detail(labels)
	case v.On:
		v.Text = "Awake"
		v.Detail = "Kept awake."
	case k.err != nil:
		v.Detail = "Could not keep the Mac awake: " + k.err.Error() + "."
	default:
		v.Detail = "The Mac sleeps as usual."
	}
	return v
}

func (k *keeper) detail(labels []string) string {
	var b strings.Builder
	b.WriteString("Kept awake by " + strings.Join(labels, ", "))
	if k.manual.On || len(k.holds) > 0 || k.settings.Battery {
		b.WriteString(", on battery too.")
	} else {
		b.WriteString(", on power only; on battery it sleeps as usual.")
	}
	if k.settings.Display {
		b.WriteString(" The screen stays on.")
	}
	b.WriteString(" " + lidNote)
	return b.String()
}

func (k *keeper) untilWords(until int64) string {
	if until == 0 {
		return "until turned off"
	}
	return "until " + k.clock(until)
}

func (k *keeper) untilShort(until int64) string {
	if until == 0 {
		return ""
	}
	return " until " + k.clock(until)
}

func (k *keeper) clock(unix int64) string {
	t := time.Unix(unix, 0).In(k.now().Location())
	now := k.now()
	if t.YearDay() == now.YearDay() && t.Year() == now.Year() {
		return t.Format("15:04")
	}
	return t.Format("Mon 15:04")
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
