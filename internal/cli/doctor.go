package cli

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/auth"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/config"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/doctor"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/launchd"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/state"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose ghr installation, configuration, and connectivity",
		RunE:  runDoctor,
	}
	cmd.Flags().Bool("json", false, "output in JSON format")
	cmd.Flags().Duration("timeout", 8*time.Second, "per-check timeout")
	cmd.Flags().String("only", "", "comma-separated list of check names to run")
	return cmd
}

func runDoctor(cmd *cobra.Command, _ []string) error {
	jsonOutput, err := cmd.Flags().GetBool("json")
	if err != nil {
		return fmt.Errorf("get json flag: %w", err)
	}
	timeout, err := cmd.Flags().GetDuration("timeout")
	if err != nil {
		return fmt.Errorf("get timeout flag: %w", err)
	}
	only, err := cmd.Flags().GetString("only")
	if err != nil {
		return fmt.Errorf("get only flag: %w", err)
	}

	checks := buildChecks()
	if only != "" {
		checks = filterChecks(checks, only)
	}

	report := doctor.Run(cmd.Context(), checks, timeout)
	if jsonOutput {
		if err := doctor.FormatJSON(cmd.OutOrStdout(), report); err != nil {
			return fmt.Errorf("write json: %w", err)
		}
	} else {
		doctor.FormatText(cmd.OutOrStdout(), report)
	}

	if code := report.ExitCode(); code != 0 {
		os.Exit(code)
	}
	return nil
}

func buildChecks() []doctor.Check {
	cfg := loadDoctorConfig()
	stateDir := cfg.Daemon.StateDir
	if stateDir == "" {
		stateDir = resolveStateDir()
	}

	creds, _, _ := auth.Load(auth.LoadOpts{TokenFlag: tokenFlag})

	credsPath := auth.FilePath()
	credsMethod := ""
	keyPath := ""
	if creds != nil {
		credsMethod = creds.Method
		if creds.GitHubApp != nil {
			keyPath = creds.GitHubApp.PrivateKeyPath
		}
	}

	token := ""
	if creds != nil {
		token = creds.PAT
	}

	label := launchd.DefaultLabel()

	return []doctor.Check{
		doctor.SocketCheck{Path: state.New(stateDir).Socket()},
		doctor.LaunchdCheck{Label: label, PlistPath: launchd.PlistPath(label)},
		doctor.CredentialsCheck{Path: credsPath, Method: credsMethod, PrivateKeyPath: keyPath},
		doctor.GitHubAPICheck{BaseURL: cfg.GitHub.URL, Token: token},
		doctor.DiskCheck{Paths: []string{stateDir, cfg.Runner.CacheDir}, MinFree: 1 << 30},
		doctor.RunnerCheck{CacheDir: cfg.Runner.CacheDir},
		doctor.CacheCheck{Path: cfg.Runner.CacheDir},
	}
}

func loadDoctorConfig() *config.Config {
	if cfgFile == "" {
		return &config.Config{}
	}
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return &config.Config{}
	}
	return cfg
}

func filterChecks(in []doctor.Check, only string) []doctor.Check {
	want := map[string]bool{}
	for _, n := range strings.Split(only, ",") {
		want[strings.TrimSpace(n)] = true
	}
	var out []doctor.Check
	for _, c := range in {
		if want[c.Name()] {
			out = append(out, c)
		}
	}
	return out
}
