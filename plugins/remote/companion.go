package main

import (
	"context"
	"slices"
	"sync"
)

const (
	companionKey = "companion"

	companionUnpaired  = "unpaired"
	companionOffer     = "offer"
	companionDismissed = "dismissed"
	companionPaired    = "paired"

	viaTailscale = "tailscale"
	viaRelay     = "relay"
)

type companionView struct {
	State string `json:"state"`
	Via   string `json:"via"`
	Host  string `json:"host"`
}

type companionStore struct {
	kv kvStore

	mu    sync.Mutex
	state string
}

func newCompanionStore(kv kvStore) *companionStore {
	return &companionStore{kv: kv, state: companionUnpaired}
}

func (c *companionStore) load(ctx context.Context) error {
	var state string
	if _, err := c.kv.StorageGet(ctx, companionKey, &state); err != nil {
		return err
	}
	if slices.Contains([]string{companionOffer, companionDismissed}, state) {
		c.mu.Lock()
		c.state = state
		c.mu.Unlock()
	}
	return nil
}

func (c *companionStore) get() string {
	if c == nil {
		return companionUnpaired
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

func (c *companionStore) move(ctx context.Context, to string, from ...string) (bool, error) {
	if c == nil {
		return false, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !slices.Contains(from, c.state) {
		return false, nil
	}
	if err := c.kv.StorageSet(ctx, companionKey, to); err != nil {
		return false, err
	}
	c.state = to
	return true, nil
}

func (c *companionStore) dismiss(ctx context.Context) (bool, error) {
	return c.move(ctx, companionDismissed, companionUnpaired, companionOffer)
}

func (c *companionStore) paired(ctx context.Context) (bool, error) {
	return c.move(ctx, companionDismissed, companionUnpaired, companionOffer)
}

func companionState(stored string, devices int, st status) companionView {
	v := companionView{State: stored}
	if devices > 0 {
		v.State = companionPaired
	}
	switch {
	case len(st.Hosts) > 0:
		v.Via, v.Host = viaTailscale, st.Hosts[0]
	case st.Tunnel.State == tunnelConnected:
		v.Via = viaRelay
	}
	return v
}
