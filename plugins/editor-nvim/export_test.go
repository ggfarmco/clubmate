package main

import "context"

func CopyAll(ctx context.Context, pairs [][2]string, progress func(copied, total int64)) error {
	ps := make([]copyPair, 0, len(pairs))
	for _, p := range pairs {
		ps = append(ps, copyPair{src: p[0], dst: p[1]})
	}
	return copyAll(ctx, ps, progress)
}
