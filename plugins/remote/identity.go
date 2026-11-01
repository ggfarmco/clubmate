package main

import (
	"context"
	"errors"
	"slices"
	"sync"
)

const identityTopic = "remote.identity"

var (
	colourOrder = []string{"blue", "magenta", "sky", "purple", "indigo", "graphite"}

	errBadColour = errors.New("colour must be one of sky, blue, indigo, purple, magenta, graphite")
)

type identity struct {
	kv      kvStore
	publish func(topic string, payload any)

	mu   sync.Mutex
	comp computer
}

type macInfo struct {
	Name   string `json:"name"`
	Colour string `json:"colour"`
}

type identityEvent struct {
	Computer computer `json:"computer"`
}

func (i *identity) get() computer {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.comp
}

func (i *identity) setColour(ctx context.Context, colour string) (computer, error) {
	if !slices.Contains(colourOrder, colour) {
		return computer{}, errBadColour
	}
	return i.update(ctx, func(c computer) (string, bool) { return colour, c.Colour != colour })
}

func (i *identity) ensureColour(ctx context.Context, used []string) (computer, error) {
	return i.update(ctx, func(c computer) (string, bool) { return defaultColour(used), c.Colour == "" })
}

func (i *identity) update(ctx context.Context, next func(computer) (string, bool)) (computer, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	colour, ok := next(i.comp)
	if !ok {
		return i.comp, nil
	}
	c := i.comp
	c.Colour = colour
	if err := i.kv.StorageSet(ctx, computerKey, c); err != nil {
		return computer{}, err
	}
	i.comp = c
	i.publish(identityTopic, identityEvent{Computer: c})
	i.publish(changedTopic, struct{}{})
	return c, nil
}

func defaultColour(used []string) string {
	for _, c := range colourOrder {
		if !slices.Contains(used, c) {
			return c
		}
	}
	return colourOrder[0]
}
