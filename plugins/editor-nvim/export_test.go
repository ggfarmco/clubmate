package main

import "context"

func CopyAll(ctx context.Context, src, dst string, progress func(copied, total int64)) error {
	return copyAll(ctx, []copyPair{{src: src, dst: dst}}, progress)
}
