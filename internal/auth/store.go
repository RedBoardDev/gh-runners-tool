package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func FilePath() string {
	if p := os.Getenv("GHR_CREDENTIALS_FILE"); p != "" {
		return p
	}
	if os.Getuid() == 0 {
		return "/etc/ghr/credentials.json"
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".config", "ghr", "credentials.json")
	}
	return filepath.Join(home, ".config", "ghr", "credentials.json")
}

func loadFromFile() (*Credentials, error) {
	data, err := os.ReadFile(FilePath())
	if err != nil {
		return nil, err
	}
	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("parse credentials file: %w", err)
	}
	return &creds, nil
}

func Save(creds *Credentials) error {
	if creds.CreatedAt.IsZero() {
		creds.CreatedAt = time.Now()
	}

	p := FilePath()
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create credentials directory %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}

	if err := os.WriteFile(p, data, 0o600); err != nil {
		return fmt.Errorf("write credentials file %s: %w", p, err)
	}
	return nil
}

func Remove() error {
	err := os.Remove(FilePath())
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove credentials file: %w", err)
	}
	return nil
}
