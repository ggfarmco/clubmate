package main

import (
	"bytes"
	"cmp"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"
)

const (
	size     = 32 << 10
	maxValue = 48
)

var protocol = []string{
	"id: ",
	"event: bus\ndata: ",
	"event: reset\ndata: ",
	"\n\n",
	`{"m":"POST","p":"/v1/rpc","h":{"Authorization":"Bearer cmt_`,
	`{"m":"GET","p":"/v1/events","h":{"Authorization":"Bearer cmt_`,
	`","Last-Event-ID":"`,
	`{"s":200,"h":{"Content-Type":"application/json"}}`,
	`{"s":200,"h":{"Content-Type":"text/event-stream"}}`,
	`{"method":"command","params":{"id":"`,
	`","workspace":"prj_`,
	`","args":{`,
	`data: {"seq":`,
	`,"topic":"`,
	`","payload":`,
	`"error":{"code":`,
}

func main() {
	out := flag.String("o", "dict-v1.bin", "output file")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: mkdict -o dict-v1.bin fixture.json…")
		os.Exit(2)
	}
	d, err := build(flag.Args())
	if err != nil {
		fmt.Fprintln(os.Stderr, "mkdict:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, d, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "mkdict:", err)
		os.Exit(1)
	}
}

func build(files []string) ([]byte, error) {
	files = slices.Sorted(slices.Values(files))
	counts := map[string]int{}
	var corpus bytes.Buffer
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		var v any
		if err := json.Unmarshal(b, &v); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		walk(v, counts)
		if err := json.Compact(&corpus, b); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
	}
	type frag struct {
		s     string
		score int
	}
	frags := make([]frag, 0, len(counts)+len(protocol))
	for s, n := range counts {
		if n > 1 {
			frags = append(frags, frag{s, n * len(s)})
		}
	}
	for _, s := range protocol {
		frags = append(frags, frag{s, 1 << 30})
	}
	slices.SortFunc(frags, func(a, b frag) int {
		return cmp.Or(cmp.Compare(a.score, b.score), strings.Compare(a.s, b.s))
	})
	var tail []byte
	for i := len(frags) - 1; i >= 0; i-- {
		if len(tail)+len(frags[i].s) > size/2 {
			break
		}
		tail = append([]byte(frags[i].s), tail...)
	}
	head := corpus.Bytes()
	need := size - len(tail)
	if len(head) < need {
		return nil, fmt.Errorf("fixtures give %d bytes, need %d", len(head), need)
	}
	return append(slices.Clone(head[len(head)-need:]), tail...), nil
}

func walk(v any, counts map[string]int) {
	switch v := v.(type) {
	case map[string]any:
		for k, x := range v {
			counts[`"`+k+`":`]++
			walk(x, counts)
		}
	case []any:
		for _, x := range v {
			walk(x, counts)
		}
	case string:
		if len(v) <= maxValue {
			counts[`"`+v+`"`]++
		}
	}
}
