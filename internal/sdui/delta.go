package sdui

import (
	"clubmate/pkg/sdui"
)

type DeltaOptions = sdui.DeltaOptions

type DeltaTracker struct {
	opts DeltaOptions
	sent map[string]sentTree
}

type sentTree struct {
	rev   int64
	index *sdui.TreeIndex
}

func NewDeltaTracker(opts DeltaOptions) *DeltaTracker {
	return &DeltaTracker{opts: opts, sent: map[string]sentTree{}}
}

func (d *DeltaTracker) Frame(view SlotView) SlotView {
	next := view.index
	if next == nil {
		ix, err := sdui.IndexTree(view.Tree)
		if err != nil {
			delete(d.sent, view.Slot)
			return view
		}
		next = ix
	}
	prev, ok := d.sent[view.Slot]
	d.sent[view.Slot] = sentTree{rev: view.Rev, index: next}
	if !ok {
		return view
	}
	delta, ok := next.DeltaFrom(prev.index, d.opts)
	if !ok {
		return view
	}
	view.Base, view.Delta, view.Tree = prev.rev, delta, nil
	return view
}
