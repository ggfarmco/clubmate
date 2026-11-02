package main

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	dictionaryKey     = "dictionary"
	maxDictionarySize = 1000
)

type dictionary struct {
	Entries []dictionaryEntry `json:"entries"`
}

type dictionaryEntry struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func (v *voice) dictionary(ctx context.Context) (dictionary, error) {
	var d dictionary
	found, err := v.h.StorageGet(ctx, dictionaryKey, &d)
	if err != nil {
		return dictionary{}, err
	}
	if !found {
		return dictionary{Entries: slices.Clone(seedDictionary.Entries)}, nil
	}
	return d.wire(), nil
}

func (v *voice) setDictionary(ctx context.Context, d dictionary) (dictionary, error) {
	if len(d.Entries) > maxDictionarySize {
		return dictionary{}, fmt.Errorf("dictionary has %d entries, at most %d", len(d.Entries), maxDictionarySize)
	}
	entries := make([]dictionaryEntry, 0, len(d.Entries))
	for i, e := range d.Entries {
		e.From, e.To = strings.TrimSpace(e.From), strings.TrimSpace(e.To)
		if e.From == "" {
			return dictionary{}, fmt.Errorf("dictionary entry %d: empty from", i)
		}
		entries = append(entries, e)
	}
	d = dictionary{Entries: entries}
	if err := v.h.StorageSet(ctx, dictionaryKey, d); err != nil {
		return dictionary{}, err
	}
	return d.wire(), nil
}

func (v *voice) loadDictionary(ctx context.Context) dictionary {
	d, err := v.dictionary(ctx)
	logError("load dictionary", err)
	return d
}

func (d dictionary) apply(text string) string {
	if len(d.Entries) == 0 {
		return text
	}
	entries := slices.SortedStableFunc(slices.Values(d.Entries), func(a, b dictionaryEntry) int {
		return cmp.Compare(utf8.RuneCountInString(b.From), utf8.RuneCountInString(a.From))
	})
	runes := []rune(text)
	var b strings.Builder
	for i := 0; i < len(runes); {
		if i == 0 || !wordRune(runes[i-1]) {
			if e, n := match(entries, runes[i:]); n > 0 {
				b.WriteString(e.To)
				i += n
				continue
			}
		}
		b.WriteRune(runes[i])
		i++
	}
	return b.String()
}

func (d dictionary) wire() dictionary {
	if d.Entries == nil {
		d.Entries = []dictionaryEntry{}
	}
	return d
}

func match(entries []dictionaryEntry, rest []rune) (dictionaryEntry, int) {
	for _, e := range entries {
		from := []rune(e.From)
		n := len(from)
		if n > len(rest) || !strings.EqualFold(string(rest[:n]), e.From) {
			continue
		}
		if n < len(rest) && wordRune(rest[n]) {
			continue
		}
		return e, n
	}
	return dictionaryEntry{}, 0
}

func wordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

var seedDictionary = dictionary{Entries: []dictionaryEntry{
	{From: "ворктри", To: "worktree"},
	{From: "ворк три", To: "worktree"},
	{From: "мсп", To: "MCP"},
	{From: "эмсипи", To: "MCP"},
	{From: "эм си пи", To: "MCP"},
	{From: "бэклог", To: "backlog"},
	{From: "баклог", To: "backlog"},
	{From: "мерж", To: "merge"},
	{From: "мёрж", To: "merge"},
	{From: "мердж", To: "merge"},
	{From: "сквош", To: "squash"},
	{From: "пиар", To: "PR"},
	{From: "пи ар", To: "PR"},
	{From: "си ай", To: "CI"},
	{From: "гит", To: "git"},
	{From: "клод", To: "Claude"},
	{From: "клабмейт", To: "Clubmate"},
	{From: "клаб мейт", To: "Clubmate"},
	{From: "компаньон", To: "Companion"},
	{From: "нвим", To: "nvim"},
	{From: "энвим", To: "nvim"},
}}
