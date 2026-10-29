package main

import "context"

func CopyAll(ctx context.Context, pairs [][2]string, progress func(copied, total int64)) error {
	ps := make([]copyPair, 0, len(pairs))
	for _, p := range pairs {
		ps = append(ps, copyPair{src: p[0], dst: p[1]})
	}
	return copyAll(ctx, ps, progress)
}

func ConfigPairs(data bool) ([][2]string, error) {
	dirs, err := resolveDirs()
	if err != nil {
		return nil, err
	}
	var out [][2]string
	for _, p := range copyPairs(dirs, data) {
		out = append(out, [2]string{p.src, p.dst})
	}
	return out, nil
}

func OnCommitted(fn func(string)) func() {
	prev := committed
	committed = fn
	return func() { committed = prev }
}
