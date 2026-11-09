package parallel

import (
	"sync"
	"sync/atomic"
)

func ForEach(n, limit int, fn func(int)) {
	var next atomic.Int64
	var wg sync.WaitGroup
	for range min(n, limit) {
		wg.Go(func() {
			for {
				i := int(next.Add(1)) - 1
				if i >= n {
					return
				}
				fn(i)
			}
		})
	}
	wg.Wait()
}
