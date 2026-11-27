package main

import (
	"cmp"
	"context"
	"crypto/rand"
	"errors"
	"slices"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

const (
	identityTopic = "remote.identity"
	nameKey       = "computerName"
	maxNameRunes  = 40
	zwnj          = '\u200C'
	zwj           = '\u200D'
)

var (
	colourOrder = []string{"blue", "magenta", "sky", "purple", "indigo", "graphite"}

	errBadColour = errors.New("colour must be one of sky, blue, indigo, purple, magenta, graphite")
	errBadName   = errors.New("name must be at most 40 characters and 128 bytes, on one line, without control or format characters (zero-width joiners are allowed)")
)

type identity struct {
	kv      kvStore
	publish func(topic string, payload any)
	system  string

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
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.save(ctx, func(c computer) computer { c.Colour = colour; return c })
}

func (i *identity) ensureColour(ctx context.Context, used []string) (computer, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.save(ctx, func(c computer) computer {
		if c.Colour == "" {
			c.Colour = defaultColour(used)
		}
		return c
	})
}

func (i *identity) setName(ctx context.Context, name string) (computer, error) {
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) > maxNameRunes || len(name) > maxDeviceField || strings.ContainsFunc(name, hiddenRune) {
		return computer{}, errBadName
	}
	if name == i.system {
		name = ""
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.kv.StorageSet(ctx, nameKey, name); err != nil {
		return computer{}, err
	}
	return i.save(ctx, func(c computer) computer { c.Name = cmp.Or(name, i.system); return c })
}

func hiddenRune(r rune) bool {
	return r != zwnj && r != zwj && unicode.In(r, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp)
}

func (i *identity) save(ctx context.Context, next func(computer) computer) (computer, error) {
	c := next(i.comp)
	if c == i.comp {
		return c, nil
	}
	if err := i.kv.StorageSet(ctx, computerKey, c); err != nil {
		return computer{}, err
	}
	i.comp = c
	i.publish(identityTopic, identityEvent{Computer: c})
	i.publish(changedTopic, struct{}{})
	return c, nil
}

func loadComputer(ctx context.Context, kv kvStore, system, model string) (computer, error) {
	var c computer
	if _, err := kv.StorageGet(ctx, computerKey, &c); err != nil {
		return computer{}, err
	}
	var name string
	if _, err := kv.StorageGet(ctx, nameKey, &name); err != nil {
		return computer{}, err
	}
	if c.ID == "" {
		c.ID = rand.Text()
	}
	c.Name = cmp.Or(name, system)
	c.Model = model
	return c, kv.StorageSet(ctx, computerKey, c)
}

func defaultColour(used []string) string {
	for _, c := range colourOrder {
		if !slices.Contains(used, c) {
			return c
		}
	}
	return colourOrder[0]
}
