package config

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	bytesPerKB int64 = 1000
	bytesPerMB int64 = 1000 * 1000
	bytesPerGB int64 = 1000 * 1000 * 1000
	bytesPerTB int64 = 1000 * 1000 * 1000 * 1000
)

func ParseByteSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("parse byte size: empty string")
	}

	upper := strings.ToUpper(s)

	suffixes := []struct {
		suffix     string
		multiplier int64
	}{
		{"TB", bytesPerTB},
		{"GB", bytesPerGB},
		{"MB", bytesPerMB},
		{"KB", bytesPerKB},
		{"B", 1},
	}

	for _, entry := range suffixes {
		if !strings.HasSuffix(upper, entry.suffix) {
			continue
		}
		numStr := strings.TrimSpace(s[:len(s)-len(entry.suffix)])
		if numStr == "" {
			return 0, fmt.Errorf("parse byte size %q: missing numeric value", s)
		}
		n, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			return 0, fmt.Errorf("parse byte size %q: %w", s, err)
		}
		if n < 0 {
			return 0, fmt.Errorf("parse byte size %q: negative value", s)
		}
		return int64(n * float64(entry.multiplier)), nil
	}

	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse byte size %q: %w", s, err)
	}
	if n < 0 {
		return 0, fmt.Errorf("parse byte size %q: negative value", s)
	}
	return n, nil
}
