package logging

import (
	"log"
	"os"
)

// * Provides a basic logger configured for stdout.
func New() *log.Logger {
	logger := log.New(os.Stdout, "[ghr] ", log.LstdFlags|log.Lmicroseconds)
	return logger
}
