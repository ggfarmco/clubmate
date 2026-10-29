package gitrepo

import (
	"strings"
	"unicode"
)

const globPunct = "*?[]{},._-/!"

func ValidGlob(g string) bool {
	if g == "" {
		return false
	}
	for _, r := range g {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune(globPunct, r) {
			return false
		}
	}
	return true
}
