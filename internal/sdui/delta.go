package sdui

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"hash/maphash"
	"strconv"
)

const (
	minRefSpan   = 16
	appendOpOpen = `{"append":"`
	appendOpSize = len(appendOpOpen) + 2
)

var errNotNode = errors.New("tree node is not an object")

type TreeIndex struct {
	tree   []byte
	nodes  []treeNode
	byHash map[uint64]int32
}

type treeNode struct {
	start, end           int
	listStart, listEnd   int
	propsStart, propsEnd int
	parent, pos          int32
	firstChild, next     int32
	hash                 uint64
}

type DeltaOptions struct {
	Appends bool
}

type DeltaTracker struct {
	opts DeltaOptions
	sent map[string]sentTree
}

type sentTree struct {
	rev   int64
	index *TreeIndex
}

type field struct {
	name, value []byte
}

type deltaWriter struct {
	ix, base *TreeIndex
	appends  bool
	dst      []byte
	clash    bool
}

func IndexTree(tree []byte) (*TreeIndex, error) {
	ix := &TreeIndex{tree: tree, byHash: map[uint64]int32{}}
	dec := newTreeDecoder(tree)
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

func (ix *TreeIndex) DeltaFrom(base *TreeIndex, opts DeltaOptions) ([]byte, bool) {
	w := deltaWriter{ix: ix, base: base, appends: opts.Appends, dst: make([]byte, 0, 4096)}
	w.node(0, 0)
	if w.clash || len(w.dst) >= len(ix.tree) {
		return nil, false
	}
	return w.dst, true
}

func NewDeltaTracker(opts DeltaOptions) *DeltaTracker {
	return &DeltaTracker{opts: opts, sent: map[string]sentTree{}}
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
	delta, ok := next.DeltaFrom(prev.index, d.opts)
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
	ix.nodes = append(ix.nodes, treeNode{start: int(dec.InputOffset()) - 1, listStart: -1, propsStart: -1, parent: parent, pos: pos, firstChild: -1, next: -1})
	for dec.PeekKind() != '}' {
		name, err := dec.ReadValue()
		if err != nil {
			return 0, err
		}
		switch {
		case string(name) == `"children"` && dec.PeekKind() == '[':
			err = ix.readChildren(dec, id)
		case string(name) == `"props"`:
			err = ix.readProps(dec, id)
		default:
			err = dec.SkipValue()
		}
		if err != nil {
			return 0, err
		}
	}
	if _, err := dec.ReadToken(); err != nil {
		return 0, err
	}
	ix.nodes[id].end = int(dec.InputOffset())
	return id, nil
}

func (ix *TreeIndex) readProps(dec *jsontext.Decoder, id int32) error {
	v, err := dec.ReadValue()
	if err != nil {
		return err
	}
	end := int(dec.InputOffset())
	ix.nodes[id].propsStart, ix.nodes[id].propsEnd = end-len(v), end
	return nil
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

func (w *deltaWriter) node(i, at int32) {
	n := w.ix.nodes[i]
	span := w.ix.tree[n.start:n.end]
	if j, ok := w.base.find(n.hash, span); ok {
		mark := len(w.dst)
		w.dst = w.base.appendRef(w.dst, j)
		if len(w.dst)-mark < len(span) {
			return
		}
		w.dst = w.dst[:mark]
	}
	patch := w.appends && at >= 0 && n.propsStart >= 0 && w.base.nodes[at].propsStart >= 0
	if n.firstChild < 0 && !patch {
		w.dst = append(w.dst, span...)
		return
	}
	propsFirst := n.firstChild < 0 || n.propsStart < n.listStart
	cur := n.start
	if patch && propsFirst {
		cur = w.props(cur, n, at)
	}
	if n.firstChild >= 0 {
		cur = w.children(cur, n, at)
	}
	if patch && !propsFirst {
		cur = w.props(cur, n, at)
	}
	w.dst = append(w.dst, w.ix.tree[cur:n.end]...)
}

func (w *deltaWriter) children(cur int, n treeNode, at int32) int {
	w.dst = append(w.dst, w.ix.tree[cur:n.listStart+1]...)
	bc := int32(-1)
	if at >= 0 {
		bc = w.base.nodes[at].firstChild
	}
	for c := n.firstChild; c >= 0; c = w.ix.nodes[c].next {
		if c != n.firstChild {
			w.dst = append(w.dst, ',')
		}
		w.node(c, bc)
		if bc >= 0 {
			bc = w.base.nodes[bc].next
		}
	}
	return n.listEnd - 1
}

func (w *deltaWriter) props(cur int, n treeNode, at int32) int {
	w.dst = append(w.dst, w.ix.tree[cur:n.propsStart]...)
	b := w.base.nodes[at]
	w.value(w.ix.tree[n.propsStart:n.propsEnd], w.base.tree[b.propsStart:b.propsEnd])
	return n.propsEnd
}

func (w *deltaWriter) value(v, base []byte) {
	switch {
	case bytes.Equal(v, base):
		w.dst = append(w.dst, v...)
	case v[0] == '"' && base[0] == '"':
		w.text(v, base)
	case v[0] == '{' && base[0] == '"':
		w.clash = w.clash || isAppendOp(v)
		w.dst = append(w.dst, v...)
	case v[0] == '{' && base[0] == '{':
		w.object(v, base)
	default:
		w.dst = append(w.dst, v...)
	}
}

func (w *deltaWriter) text(v, base []byte) {
	if len(v) > len(base) && bytes.HasPrefix(v, base[:len(base)-1]) {
		tail := v[len(base)-1 : len(v)-1]
		if !startsWithLowSurrogate(tail) && len(tail)+appendOpSize < len(v) {
			w.dst = append(append(append(w.dst, appendOpOpen...), tail...), `"}`...)
			return
		}
	}
	w.dst = append(w.dst, v...)
}

func (w *deltaWriter) object(v, base []byte) {
	fields, ok := objectFields(v)
	baseFields, baseOK := objectFields(base)
	if !ok || !baseOK {
		w.dst = append(w.dst, v...)
		return
	}
	w.dst = append(w.dst, '{')
	for i, f := range fields {
		if i > 0 {
			w.dst = append(w.dst, ',')
		}
		w.dst = append(append(w.dst, f.name...), ':')
		if bv, ok := fieldValue(baseFields, f.name); ok {
			w.value(f.value, bv)
		} else {
			w.dst = append(w.dst, f.value...)
		}
	}
	w.dst = append(w.dst, '}')
}

func objectFields(v []byte) ([]field, bool) {
	dec := newTreeDecoder(v)
	if _, err := dec.ReadToken(); err != nil {
		return nil, false
	}
	var fields []field
	for dec.PeekKind() != '}' {
		name, err := dec.ReadValue()
		if err != nil {
			return nil, false
		}
		nameEnd := int(dec.InputOffset())
		val, err := dec.ReadValue()
		if err != nil {
			return nil, false
		}
		end := int(dec.InputOffset())
		fields = append(fields, field{name: v[nameEnd-len(name) : nameEnd], value: v[end-len(val) : end]})
	}
	return fields, true
}

func fieldValue(fields []field, name []byte) ([]byte, bool) {
	for _, f := range fields {
		if bytes.Equal(f.name, name) {
			return f.value, true
		}
	}
	return nil, false
}

func isAppendOp(v []byte) bool {
	dec := newTreeDecoder(v)
	if _, err := dec.ReadToken(); err != nil {
		return false
	}
	name, err := dec.ReadToken()
	if err != nil || name.Kind() != '"' || name.String() != "append" || dec.PeekKind() != '"' {
		return false
	}
	if err := dec.SkipValue(); err != nil {
		return false
	}
	return dec.PeekKind() == '}'
}

func startsWithLowSurrogate(s []byte) bool {
	if len(s) < 6 || s[0] != '\\' || s[1] != 'u' || s[2]|0x20 != 'd' {
		return false
	}
	c := s[3] | 0x20
	return c >= 'c' && c <= 'f'
}

func newTreeDecoder(b []byte) *jsontext.Decoder {
	return jsontext.NewDecoder(bytes.NewReader(b), jsontext.AllowDuplicateNames(true), jsontext.AllowInvalidUTF8(true))
}

var treeSeed = maphash.MakeSeed()
