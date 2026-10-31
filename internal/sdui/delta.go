package sdui

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"hash/maphash"
	"strconv"
)

const minRefSpan = 16

var errNotNode = errors.New("tree node is not an object")

type TreeIndex struct {
	tree   []byte
	nodes  []treeNode
	byHash map[uint64]int32
}

type treeNode struct {
	start, end         int
	listStart, listEnd int
	parent, pos        int32
	firstChild, next   int32
	hash               uint64
}

type DeltaTracker struct {
	sent map[string]sentTree
}

type sentTree struct {
	rev   int64
	index *TreeIndex
}

func IndexTree(tree []byte) (*TreeIndex, error) {
	ix := &TreeIndex{tree: tree, byHash: map[uint64]int32{}}
	dec := jsontext.NewDecoder(bytes.NewReader(tree), jsontext.AllowDuplicateNames(true), jsontext.AllowInvalidUTF8(true))
	if _, err := ix.readNode(dec, -1, 0); err != nil {
		return nil, err
	}
	for i := range ix.nodes {
		n := &ix.nodes[i]
		if n.end-n.start < minRefSpan {
			continue
		}
		n.hash = maphash.Bytes(treeSeed, tree[n.start:n.end])
		if _, ok := ix.byHash[n.hash]; !ok {
			ix.byHash[n.hash] = int32(i)
		}
	}
	return ix, nil
}

func (ix *TreeIndex) DeltaFrom(base *TreeIndex) ([]byte, bool) {
	out := ix.appendDelta(make([]byte, 0, 4096), base, 0)
	if len(out) >= len(ix.tree) {
		return nil, false
	}
	return out, true
}

func NewDeltaTracker() *DeltaTracker {
	return &DeltaTracker{sent: map[string]sentTree{}}
}

func (d *DeltaTracker) Frame(view SlotView) SlotView {
	next, err := IndexTree(view.Tree)
	if err != nil {
		delete(d.sent, view.Slot)
		return view
	}
	prev, ok := d.sent[view.Slot]
	d.sent[view.Slot] = sentTree{rev: view.Rev, index: next}
	if !ok {
		return view
	}
	delta, ok := next.DeltaFrom(prev.index)
	if !ok {
		return view
	}
	view.Base, view.Delta, view.Tree = prev.rev, delta, nil
	return view
}

func (ix *TreeIndex) readNode(dec *jsontext.Decoder, parent, pos int32) (int32, error) {
	tok, err := dec.ReadToken()
	if err != nil {
		return 0, err
	}
	if tok.Kind() != '{' {
		return 0, errNotNode
	}
	id := int32(len(ix.nodes))
	ix.nodes = append(ix.nodes, treeNode{start: int(dec.InputOffset()) - 1, listStart: -1, parent: parent, pos: pos, firstChild: -1, next: -1})
	for dec.PeekKind() != '}' {
		name, err := dec.ReadValue()
		if err != nil {
			return 0, err
		}
		if string(name) != `"children"` || dec.PeekKind() != '[' {
			if err := dec.SkipValue(); err != nil {
				return 0, err
			}
			continue
		}
		if err := ix.readChildren(dec, id); err != nil {
			return 0, err
		}
	}
	if _, err := dec.ReadToken(); err != nil {
		return 0, err
	}
	ix.nodes[id].end = int(dec.InputOffset())
	return id, nil
}

func (ix *TreeIndex) readChildren(dec *jsontext.Decoder, id int32) error {
	if _, err := dec.ReadToken(); err != nil {
		return err
	}
	ix.nodes[id].listStart = int(dec.InputOffset()) - 1
	prev := int32(-1)
	for pos := int32(0); dec.PeekKind() != ']'; pos++ {
		child, err := ix.readNode(dec, id, pos)
		if err != nil {
			return err
		}
		if prev < 0 {
			ix.nodes[id].firstChild = child
		} else {
			ix.nodes[prev].next = child
		}
		prev = child
	}
	if _, err := dec.ReadToken(); err != nil {
		return err
	}
	ix.nodes[id].listEnd = int(dec.InputOffset())
	return nil
}

func (ix *TreeIndex) appendDelta(dst []byte, base *TreeIndex, i int32) []byte {
	n := ix.nodes[i]
	span := ix.tree[n.start:n.end]
	if j, ok := base.find(n.hash, span); ok {
		mark := len(dst)
		dst = base.appendRef(dst, j)
		if len(dst)-mark < len(span) {
			return dst
		}
		dst = dst[:mark]
	}
	if n.firstChild < 0 {
		return append(dst, span...)
	}
	dst = append(dst, ix.tree[n.start:n.listStart+1]...)
	for c := n.firstChild; c >= 0; c = ix.nodes[c].next {
		if c != n.firstChild {
			dst = append(dst, ',')
		}
		dst = ix.appendDelta(dst, base, c)
	}
	return append(dst, ix.tree[n.listEnd-1:n.end]...)
}

func (ix *TreeIndex) find(hash uint64, span []byte) (int32, bool) {
	if hash == 0 {
		return 0, false
	}
	j, ok := ix.byHash[hash]
	if !ok {
		return 0, false
	}
	n := ix.nodes[j]
	return j, bytes.Equal(ix.tree[n.start:n.end], span)
}

func (ix *TreeIndex) appendRef(dst []byte, j int32) []byte {
	var buf [32]int32
	path := buf[:0]
	for ; ix.nodes[j].parent >= 0; j = ix.nodes[j].parent {
		path = append(path, ix.nodes[j].pos)
	}
	dst = append(dst, `{"ref":[`...)
	for k := len(path) - 1; k >= 0; k-- {
		dst = strconv.AppendInt(dst, int64(path[k]), 10)
		if k > 0 {
			dst = append(dst, ',')
		}
	}
	return append(dst, "]}"...)
}

var treeSeed = maphash.MakeSeed()
