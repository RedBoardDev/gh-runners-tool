package config

import (
	"testing"
)

func TestParseByteSize(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int64
		wantErr bool
	}{
		// Raw byte values.
		{name: "numeric only", input: "1024", want: 1024},
		{name: "zero", input: "0", want: 0},
		{name: "explicit B suffix", input: "1B", want: 1},
		{name: "large bytes", input: "999999", want: 999999},

		// KB (1000-based).
		{name: "1KB", input: "1KB", want: 1000},
		{name: "lowercase kb", input: "1kb", want: 1000},
		{name: "mixed case Kb", input: "1Kb", want: 1000},
		{name: "500KB", input: "500KB", want: 500_000},

		// MB.
		{name: "1MB", input: "1MB", want: 1_000_000},
		{name: "500MB", input: "500MB", want: 500_000_000},

		// GB.
		{name: "1GB", input: "1GB", want: 1_000_000_000},
		{name: "10GB", input: "10GB", want: 10_000_000_000},

		// TB.
		{name: "1TB", input: "1TB", want: 1_000_000_000_000},
		{name: "2TB", input: "2TB", want: 2_000_000_000_000},

		// Fractional values (supported for suffixed inputs via ParseFloat).
		{name: "1.5GB", input: "1.5GB", want: 1_500_000_000},
		{name: "0.5MB", input: "0.5MB", want: 500_000},

		// Binary suffixes (1024-based), matching systemd and Docker.
		{name: "1K", input: "1K", want: 1024},
		{name: "1M", input: "1M", want: 1 << 20},
		{name: "12G", input: "12G", want: 12 << 30},
		{name: "lowercase g", input: "48g", want: 48 << 30},
		{name: "1T", input: "1T", want: 1 << 40},
		{name: "1KiB", input: "1KiB", want: 1024},
		{name: "512MiB", input: "512MiB", want: 512 << 20},
		{name: "2GiB", input: "2GiB", want: 2 << 30},
		{name: "1TiB", input: "1TiB", want: 1 << 40},
		{name: "fractional G", input: "1.5G", want: 3 << 29},

		// Whitespace handling.
		{name: "leading/trailing spaces", input: "  100MB  ", want: 100_000_000},

		// Error cases.
		{name: "empty string", input: "", wantErr: true},
		{name: "pure alpha", input: "abc", wantErr: true},
		{name: "negative GB", input: "-1GB", wantErr: true},
		{name: "negative raw", input: "-100", wantErr: true},
		{name: "suffix only KB", input: "KB", wantErr: true},
		{name: "suffix only B", input: "B", wantErr: true},
		{name: "suffix only G", input: "G", wantErr: true},
		{name: "suffix only GiB", input: "GiB", wantErr: true},
		{name: "negative G", input: "-1G", wantErr: true},
		{name: "overflowing value", input: "99999999999TB", wantErr: true},
		{name: "infinite value", input: "infG", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseByteSize(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseByteSize(%q) = %d, want error", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseByteSize(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("ParseByteSize(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}
