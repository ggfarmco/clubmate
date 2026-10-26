package main

import (
	"os"

	"clubmate/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
