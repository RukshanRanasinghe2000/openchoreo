// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package parallel

import "sync"

// Run distributes the work of calling analyze over items using a fixed pool of
// workers goroutines. Results are returned in the same order as the input
// regardless of which worker finishes first.
func Run[T any](items []string, workers int, analyze func(string) T) []T {
	if len(items) == 0 {
		return nil
	}
	if workers < 1 {
		workers = 1
	}
	if workers > len(items) {
		workers = len(items)
	}

	results := make([]T, len(items))
	if workers == 1 {
		for i, f := range items {
			results[i] = analyze(f)
		}
		return results
	}

	jobs := make(chan int)
	type indexed struct {
		idx int
		val T
	}
	out := make(chan indexed, len(items))

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				out <- indexed{idx: idx, val: analyze(items[idx])}
			}
		}()
	}
	for i := range items {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	close(out)

	for r := range out {
		results[r.idx] = r.val
	}
	return results
}
