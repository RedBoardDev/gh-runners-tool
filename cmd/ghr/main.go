package main

import (
	"log"

	"gh-runners-tool/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		log.Fatalf("ghr: %v", err)
	}
}
