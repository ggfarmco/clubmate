package main

import (
	"context"
	"fmt"
	"os"

	"clubmate/pkg/pluginsdk"
)

type store struct {
	h *pluginsdk.Host
}

func (st *store) load(ctx context.Context) ([]*Run, error) {
	keys, err := st.h.StorageList(ctx, runPrefix)
	if err != nil {
		return nil, fmt.Errorf("list graph runs: %w", err)
	}
	runs := make([]*Run, 0, len(keys))
	for _, key := range keys {
		var r Run
		found, err := st.h.StorageGet(ctx, key, &r)
		if err != nil {
			fmt.Fprintln(os.Stderr, "graphs: skip stored run", key+":", err)
			continue
		}
		if found {
			runs = append(runs, &r)
		}
	}
	return runs, nil
}

func (st *store) save(ctx context.Context, r *Run) error {
	if err := st.h.StorageSet(ctx, runKey(r), r); err != nil {
		return fmt.Errorf("save graph run %s: %w", r.ID, err)
	}
	return nil
}

func (st *store) remove(ctx context.Context, r *Run) error {
	if err := st.h.StorageDelete(ctx, runKey(r)); err != nil {
		return fmt.Errorf("delete graph run %s: %w", r.ID, err)
	}
	return nil
}
