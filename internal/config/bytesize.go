package config

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

const (
	bytesPerKB int64 = 1000
	bytesPerMB int64 = 1000 * 1000
	bytesPerGB int64 = 1000 * 1000 * 1000
	bytesPerTB int64 = 1000 * 1000 * 1000 * 1000

	bytesPerKiB int64 = 1 << 10
	bytesPerMiB int64 = 1 << 20
	bytesPerGiB int64 = 1 << 30
	bytesPerTiB int64 = 1 << 40
)

var byteSizeSuffixes = []struct {
	suffix     string
	multiplier int64
}{
	{"TIB", bytesPerTiB},
	{"GIB", bytesPerGiB},
	{"MIB", bytesPerMiB},
	{"KIB", bytesPerKiB},
	{"TB", bytesPerTB},
	{"GB", bytesPerGB},
	{"MB", bytesPerMB},
	{"KB", bytesPerKB},
	{"T", bytesPerTiB},
	{"G", bytesPerGiB},
	{"M", bytesPerMiB},
	{"K", bytesPerKiB},
	{"B", 1},
}

func ParseByteSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("parse byte size: empty string")
	}

	upper := strings.ToUpper(s)

	for _, entry := range byteSizeSuffixes {
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
		total := n * float64(entry.multiplier)
		if math.IsNaN(total) || total >= math.MaxInt64 {
			return 0, fmt.Errorf("parse byte size %q: value out of range", s)
		}
		return int64(total), nil
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
