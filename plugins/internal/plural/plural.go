package plural

import "fmt"

func Word(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func N(n int, word string) string {
	return Count(n, "1 "+word, "%d "+word+"s")
}

func Count(n int, one, manyFmt string) string {
	if n == 1 {
		return one
	}
	return fmt.Sprintf(manyFmt, n)
}
