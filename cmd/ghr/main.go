package main

import (
	"os"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
