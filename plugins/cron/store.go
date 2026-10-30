package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"

	"clubmate/pkg/pluginsdk"
)

const (
	jobPrefix = "job/"
	idPrefix  = "cj_"
)

type store struct {
	h *pluginsdk.Host
}

func (st *store) load(ctx context.Context) ([]*Job, error) {
	keys, err := st.h.StorageList(ctx, jobPrefix)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	jobs := make([]*Job, 0, len(keys))
	for _, key := range keys {
		var j Job
		found, err := st.h.StorageGet(ctx, key, &j)
		if err != nil {
			fmt.Fprintln(os.Stderr, "cron: skip stored job", key+":", err)
			continue
		}
		if found {
			jobs = append(jobs, &j)
		}
	}
	return jobs, nil
}

func (st *store) save(ctx context.Context, j *Job) error {
	if err := st.h.StorageSet(ctx, jobKey(j), j); err != nil {
		return fmt.Errorf("save job %s: %w", j.ID, err)
	}
	return nil
}

func (st *store) remove(ctx context.Context, j *Job) error {
	if err := st.h.StorageDelete(ctx, jobKey(j)); err != nil {
		return fmt.Errorf("delete job %s: %w", j.ID, err)
	}
	return nil
}

func jobKey(j *Job) string {
	return jobPrefix + j.Workspace + "/" + j.ID
}

func newJobID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return idPrefix + hex.EncodeToString(b), nil
}
