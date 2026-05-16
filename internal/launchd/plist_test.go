package launchd

import (
	"strings"
	"testing"
)

func TestGeneratePlist_ValidConfig(t *testing.T) {
	cfg := ServiceConfig{
		Label:      "com.ghr.daemon",
		BinaryPath: "/usr/local/bin/ghr",
		ConfigPath: "/etc/ghr/config.yaml",
		LogDir:     "/var/log/ghr",
		StateDir:   "/var/lib/ghr/state",
	}

	data, err := generatePlist(&cfg)
	if err != nil {
		t.Fatalf("generatePlist() error = %v", err)
	}

	result := string(data)

	checks := []struct {
		name     string
		expected string
	}{
		{"xml header", `<?xml version="1.0" encoding="UTF-8"?>`},
		{"label", `<string>com.ghr.daemon</string>`},
		{"binary path", `<string>/usr/local/bin/ghr</string>`},
		{"run command", `<string>run</string>`},
		{"config flag", `<string>--config</string>`},
		{"config path", `<string>/etc/ghr/config.yaml</string>`},
		{"stdout path", `<string>/var/log/ghr/daemon.log</string>`},
		{"stderr path", `<string>/var/log/ghr/daemon.err</string>`},
		{"workdir", `<string>/var/lib/ghr/state</string>`},
		{"run at load", `<true/>`},
		{"keep alive", `<key>SuccessfulExit</key>`},
	}

	for _, tc := range checks {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(result, tc.expected) {
				t.Errorf("plist missing %q", tc.expected)
			}
		})
	}
}

func TestGeneratePlist_SpecialChars(t *testing.T) {
	cfg := ServiceConfig{
		Label:      "com.ghr.test",
		BinaryPath: "/path/with spaces/ghr",
		ConfigPath: "/config/test.yaml",
		LogDir:     "/tmp/logs",
		StateDir:   "/tmp/state",
	}

	data, err := generatePlist(&cfg)
	if err != nil {
		t.Fatalf("generatePlist() error = %v", err)
	}

	if !strings.Contains(string(data), "/path/with spaces/ghr") {
		t.Error("plist should preserve paths with spaces")
	}
}

func TestGeneratePlist_EscapesXMLMetacharacters(t *testing.T) {
	cfg := ServiceConfig{
		Label:      "com.ghr.injected",
		BinaryPath: `/tmp/x</string><key>InjectedKey</key><string>yes`,
		ConfigPath: "/config/<test>&yaml",
		LogDir:     "/tmp/log\"dir",
		StateDir:   "/tmp/state",
	}

	data, err := generatePlist(&cfg)
	if err != nil {
		t.Fatalf("generatePlist() error = %v", err)
	}
	out := string(data)

	if strings.Contains(out, "<key>InjectedKey</key>") {
		t.Errorf("plist must escape XML payload, got: %s", out)
	}
	for _, needle := range []string{
		"&lt;/string&gt;",
		"&lt;key&gt;InjectedKey&lt;/key&gt;",
		"/config/&lt;test&gt;&amp;yaml",
	} {
		if !strings.Contains(out, needle) {
			t.Errorf("expected escaped %q in plist, got: %s", needle, out)
		}
	}
}
