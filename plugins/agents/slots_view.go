package main

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/internal/automation"
)

const (
	slotActPrefix   = "slots-"
	actSlotsOn      = "slots-enabled"
	actSlotsWait    = "slots-maxwait"
	actSlotEdit     = "slots-edit"
	actSlotAdd      = "slots-add"
	actSlotID       = "slots-id"
	actSlotLabel    = "slots-label"
	actSlotCount    = "slots-count"
	actSlotMax      = "slots-max"
	actSlotCPU      = "slots-cpu"
	actSlotMem      = "slots-mem"
	actSlotSettle   = "slots-settle"
	actSlotPatterns = "slots-patterns"
	actSlotSave     = "slots-save"
	actSlotCancel   = "slots-cancel"
	actSlotRemove   = "slots-remove"
	actSlotReset    = "slots-reset"
	actSlotRelease  = "slots-release"
	slotSheetID     = "slots-sheet"

	slotsTitle     = "Resource limits"
	slotsHint      = "Agents' heavy commands (tests, simulator boots) wait for a free slot before they start. Your own terminals are not limited."
	slotsWaitTitle = "Max wait"
	slotsWaitHint  = "A command that gets no slot in this time is not run; the agent is told why."
	slotsAddLabel  = "Add kind"
	slotsOffLine   = "off"
	slotNewTitle   = "New kind"
	slotEditTitle  = "%s slots"
	slotSheetNote  = "A Bash command that matches a pattern waits for a slot. The first slot is always free; more need CPU at or below the limit, the memory floor and the settle pause since the last start."
	slotRemoveAsk  = "Remove this kind? Its commands run without a slot."
	slotNoHolders  = "Nobody holds or waits for a slot."
	slotHolds      = "holds"
	slotWaits      = "waits %d"
	slotPattHint   = "one regular expression per line"
	slotIDHint     = "id, e.g. build"
	slotLabelHint  = "name, e.g. Builds"
)

var (
	slotWaitChoices   = []int{2, 5, 8, 10, 15, 20, 30, 60}
	slotMaxChoices    = []int{1, 2, 3, 4, 5, 6, 8, 10, 12, 16}
	slotCPUChoices    = []int{0, 50, 60, 70, 80, 90, 100}
	slotMemChoices    = []int{0, 1, 2, 4, 6, 8, 12, 16}
	slotSettleChoices = []int{0, 10, 20, 30, 60}
)

type slotDraft struct {
	orig     string
	kind     slotKind
	patterns string
	err      string
}

func (a *app) slotSettingsRows() []sdui.Node {
	v := a.slotsSnapshot()
	rows := []sdui.Node{
		sdui.SettingRow(slotsTitle, slotsHint, sdui.WithAction(sdui.Toggle(actSlotsOn, v.Enabled), actSlotsOn)),
	}
	if !v.Enabled {
		return rows
	}
	wait := int(time.Duration(v.MaxWait) / time.Minute)
	rows = append(rows, sdui.SettingRow(slotsWaitTitle, slotsWaitHint, sdui.WithAction(sdui.Select(actSlotsWait, minuteOptions(slotWaitChoices, wait), strconv.Itoa(wait)), actSlotsWait)))
	for _, k := range v.Kinds {
		edit := sdui.WithAction(sdui.Button(actSlotEdit+"/"+k.ID, "Edit", ""), actSlotEdit)
		rows = append(rows, sdui.SettingRow(k.Label, k.Line, edit))
	}
	rows = append(rows, sdui.SettingRow("", "", sdui.WithVariant(sdui.WithAction(sdui.Button(actSlotAdd, slotsAddLabel, "plus"), actSlotAdd), sdui.VariantSecondary)))
	if sheet, ok := a.slotSheet(v); ok {
		rows = append(rows, sheet)
	}
	return rows
}

func minuteOptions(values []int, cur int) []sdui.Option {
	if !slices.Contains(values, cur) {
		values = append(slices.Clone(values), cur)
		slices.Sort(values)
	}
	out := make([]sdui.Option, 0, len(values))
	for _, m := range values {
		out = append(out, sdui.Option{Value: strconv.Itoa(m), Label: fmt.Sprintf("%d min", m)})
	}
	return out
}

func numberOptions(values []int, cur int, unit, off string) []sdui.Option {
	if !slices.Contains(values, cur) {
		values = append(slices.Clone(values), cur)
		slices.Sort(values)
	}
	out := make([]sdui.Option, 0, len(values))
	for _, n := range values {
		label := strconv.Itoa(n) + unit
		if n == 0 && off != "" {
			label = off
		}
		out = append(out, sdui.Option{Value: strconv.Itoa(n), Label: label})
	}
	return out
}

func (a *app) slotSheet(v slotsView) (sdui.Node, bool) {
	b := &a.resources
	b.mu.Lock()
	d := b.draft
	b.mu.Unlock()
	if d == nil {
		return sdui.Node{}, false
	}
	k := d.kind
	title := slotNewTitle
	if d.orig != "" {
		title = fmt.Sprintf(slotEditTitle, k.Label)
	}
	field := func(id, value, hint, act string) sdui.Node {
		return sdui.WithAction(sdui.WithLive(sdui.TextField(slotSheetID+"/"+id, value, hint)), act)
	}
	pick := func(id string, options []sdui.Option, value int, act string) sdui.Node {
		return sdui.WithAction(sdui.Select(slotSheetID+"/"+id, options, strconv.Itoa(value)), act)
	}
	count := sdui.WithAction(sdui.Select(slotSheetID+"/count", []sdui.Option{{Value: countProcesses, Label: "commands that hold it"}, {Value: countSimulators, Label: "booted cm-* simulators"}}, k.Count), actSlotCount)
	rows := []sdui.Node{
		sdui.HStack(sdui.MutedText("Id"), field("id", k.ID, slotIDHint, actSlotID)),
		sdui.HStack(sdui.MutedText("Name"), field("label", k.Label, slotLabelHint, actSlotLabel)),
		sdui.HStack(sdui.MutedText("In use counts"), count),
		sdui.HStack(sdui.MutedText("Slots"), pick("max", numberOptions(slotMaxChoices, k.Max, "", ""), k.Max, actSlotMax)),
		sdui.HStack(sdui.MutedText("CPU limit"), pick("cpu", numberOptions(slotCPUChoices, int(k.CPU), " %", "no check"), int(k.CPU), actSlotCPU)),
		sdui.HStack(sdui.MutedText("Free memory"), pick("mem", numberOptions(slotMemChoices, int(k.MemFreeGB), " GB", "no check"), int(k.MemFreeGB), actSlotMem)),
		sdui.HStack(sdui.MutedText("Settle pause"), pick("settle", numberOptions(slotSettleChoices, int(time.Duration(k.Settle)/time.Second), " s", ""), int(time.Duration(k.Settle)/time.Second), actSlotSettle)),
		sdui.HStack(sdui.MutedText("Patterns"), sdui.WithLines(field("patterns", d.patterns, slotPattHint, actSlotPatterns), 5)),
	}
	if i := slices.IndexFunc(v.Kinds, func(kv slotKindView) bool { return kv.ID == d.orig }); i >= 0 {
		rows = append(rows, a.slotPeople(v.Kinds[i])...)
	}
	buttons := []sdui.Node{}
	if d.orig != "" {
		buttons = append(buttons, sdui.WithVariant(sdui.WithConfirm(sdui.WithAction(sdui.Button(slotSheetID+"/remove", "Remove", ""), actSlotRemove), slotRemoveAsk), sdui.VariantDanger))
	}
	if slices.ContainsFunc(defaultSlotKinds(runtime.NumCPU()), func(dk slotKind) bool { return dk.ID == k.ID }) {
		buttons = append(buttons, sdui.WithVariant(sdui.WithAction(sdui.Button(slotSheetID+"/reset", "Reset to defaults", ""), actSlotReset), sdui.VariantSecondary))
	}
	buttons = append(buttons,
		sdui.WithVariant(sdui.WithAction(sdui.Button(slotSheetID+"/cancel", "Cancel", ""), actSlotCancel), sdui.VariantSecondary),
		sdui.WithShortcut(sdui.WithVariant(sdui.WithAction(sdui.Button(slotSheetID+"/save", "Save", ""), actSlotSave), sdui.VariantPrimary), "cmd+return"),
	)
	note, tone := slotSheetNote, ""
	if d.err != "" {
		note, tone = d.err, "danger"
	}
	return sdui.Sheet(slotSheetID, sdui.SheetProps{Title: title, Icon: "gauge", Cancel: actSlotCancel, Note: note, NoteTone: tone},
		automation.Properties(rows...),
		sdui.HStack(buttons...),
	), true
}

func (a *app) slotPeople(k slotKindView) []sdui.Node {
	if len(k.Holders) == 0 && len(k.Waiters) == 0 {
		return []sdui.Node{sdui.MutedText(slotNoHolders)}
	}
	rows := []sdui.Node{sdui.MutedText(k.Line)}
	for _, h := range k.Holders {
		release := sdui.WithVariant(sdui.WithAction(sdui.Button(actSlotRelease+"/"+h.ID, "Release", ""), actSlotRelease), sdui.VariantSecondary)
		rows = append(rows, sdui.HStack(sdui.MutedText(slotHolds), sdui.Text(h.Title), sdui.MonoText(h.Command), sdui.Spacer(), release))
	}
	for _, w := range k.Waiters {
		rows = append(rows, sdui.HStack(sdui.MutedText(fmt.Sprintf(slotWaits, w.Place)), sdui.Text(w.Title), sdui.MonoText(w.Command), sdui.Spacer(), sdui.MutedText(w.Why)))
	}
	return rows
}

func isSlotAction(action string) bool {
	return strings.HasPrefix(action, slotActPrefix)
}

func (a *app) slotSettingsEvent(ctx context.Context, ev pluginsdk.UIEvent) error {
	b := &a.resources
	str := func() (string, error) {
		var s string
		if err := json.Unmarshal(ev.Value, &s); err != nil {
			return "", fmt.Errorf("%s value: %w", ev.Action, err)
		}
		return s, nil
	}
	num := func() (int, error) {
		s, err := str()
		if err != nil {
			return 0, err
		}
		return strconv.Atoi(s)
	}
	switch ev.Action {
	case actSlotsOn:
		var on bool
		if err := json.Unmarshal(ev.Value, &on); err != nil {
			return fmt.Errorf("%s value: %w", ev.Action, err)
		}
		cfg := a.slotConfig()
		cfg.Enabled = on
		return a.setSlotConfig(ctx, cfg)
	case actSlotsWait:
		m, err := num()
		if err != nil {
			return err
		}
		cfg := a.slotConfig()
		cfg.MaxWait = seconds(time.Duration(m) * time.Minute)
		return a.setSlotConfig(ctx, cfg)
	case actSlotEdit:
		id := strings.TrimPrefix(ev.NodeID, actSlotEdit+"/")
		k, ok := a.slotConfig().kind(id)
		if !ok {
			return fmt.Errorf("%w: %q", errSlotKind, id)
		}
		b.setDraft(&slotDraft{orig: id, kind: k, patterns: strings.Join(k.Patterns, "\n")})
	case actSlotAdd:
		b.setDraft(&slotDraft{kind: slotKind{Max: 1, CPU: slotCPU, Settle: seconds(slotSettle), Count: countProcesses}})
	case actSlotCancel:
		b.setDraft(nil)
	case actSlotRelease:
		return a.releaseSlot(ctx, strings.TrimPrefix(ev.NodeID, actSlotRelease+"/"))
	case actSlotSave:
		return a.saveSlotDraft(ctx)
	case actSlotRemove:
		d := b.getDraft()
		if d == nil {
			return nil
		}
		cfg := a.slotConfig()
		cfg.Kinds = slices.DeleteFunc(cfg.Kinds, func(k slotKind) bool { return k.ID == d.orig })
		if err := a.setSlotConfig(ctx, cfg); err != nil {
			return err
		}
		b.setDraft(nil)
	default:
		return a.editSlotDraft(ev, str, num)
	}
	a.refreshSettings(ctx)
	return nil
}

func (a *app) editSlotDraft(ev pluginsdk.UIEvent, str func() (string, error), num func() (int, error)) error {
	b := &a.resources
	d := b.getDraft()
	if d == nil {
		return nil
	}
	next := *d
	var err error
	var n int
	switch ev.Action {
	case actSlotID:
		next.kind.ID, err = str()
	case actSlotLabel:
		next.kind.Label, err = str()
	case actSlotCount:
		next.kind.Count, err = str()
	case actSlotPatterns:
		next.patterns, err = str()
	case actSlotMax:
		n, err = num()
		next.kind.Max = n
	case actSlotCPU:
		n, err = num()
		next.kind.CPU = float64(n)
	case actSlotMem:
		n, err = num()
		next.kind.MemFreeGB = float64(n)
	case actSlotSettle:
		n, err = num()
		next.kind.Settle = seconds(time.Duration(n) * time.Second)
	case actSlotReset:
		reset, rerr := resetKind(slotConfig{Kinds: []slotKind{next.kind}}, next.kind.ID)
		if rerr != nil {
			return rerr
		}
		next.kind = reset.Kinds[0]
		next.patterns = strings.Join(next.kind.Patterns, "\n")
	default:
		return fmt.Errorf("unknown action %q", ev.Action)
	}
	if err != nil {
		return err
	}
	next.err = ""
	b.setDraft(&next)
	if ev.Action != actSlotPatterns && ev.Action != actSlotID && ev.Action != actSlotLabel {
		a.refreshSettings(context.Background())
	}
	return nil
}

func (a *app) saveSlotDraft(ctx context.Context) error {
	b := &a.resources
	d := b.getDraft()
	if d == nil {
		return nil
	}
	k := d.kind
	k.Patterns = strings.Split(d.patterns, "\n")
	cfg := a.slotConfig()
	if i := slices.IndexFunc(cfg.Kinds, func(x slotKind) bool { return x.ID == d.orig }); d.orig != "" && i >= 0 {
		cfg.Kinds[i] = k
	} else {
		cfg.Kinds = append(cfg.Kinds, k)
	}
	if err := a.setSlotConfig(ctx, cfg); err != nil {
		next := *d
		next.err = err.Error()
		b.setDraft(&next)
		a.refreshSettings(ctx)
		return nil
	}
	b.setDraft(nil)
	a.refreshSettings(ctx)
	return nil
}

func (b *slotBoard) setDraft(d *slotDraft) {
	b.mu.Lock()
	b.draft = d
	b.mu.Unlock()
}

func (b *slotBoard) getDraft() *slotDraft {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.draft
}
