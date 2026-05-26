package main

import (
	"fmt"
	"io"
	"os"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/cli"
)

var execute = cli.Execute

func main() {
	os.Exit(run(os.Stderr))
}

func run(stderr io.Writer) int {
	if err := execute(); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
