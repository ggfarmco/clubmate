package bashcmd

import (
	"strings"
	"unicode"
)

const separators = ";&|\n`()$"

type segment struct {
	Words []string
	At    int
}

func segments(command string) []segment {
	var out []segment
	start := -1
	flush := func(end int) {
		if start < 0 {
			return
		}
		text := command[start:end]
		if words := strings.Fields(text); len(words) > 0 {
			out = append(out, segment{Words: words, At: start + strings.IndexFunc(text, func(r rune) bool { return !unicode.IsSpace(r) })})
		}
		start = -1
	}
	for i, r := range command {
		if strings.ContainsRune(separators, r) {
			flush(i)
			continue
		}
		if start < 0 {
			start = i
		}
	}
	flush(len(command))
	return out
}

func Parts(command string) []string {
	out := []string{command}
	for _, s := range segments(command) {
		words := s.Words
		for len(words) > 0 && strings.Contains(words[0], "=") {
			words = words[1:]
		}
		if len(words) > 0 {
			out = append(out, strings.Join(words, " "))
		}
	}
	return out
}
