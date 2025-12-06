package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"gh-runners-tool/internal/config"
	"gh-runners-tool/internal/logging"
	"gh-runners-tool/internal/provider/github"
	"github.com/spf13/cobra"
)

func purgeCmd() *cobra.Command {
	var timeout time.Duration
	var waitInterval time.Duration

	cmd := &cobra.Command{
		Use:   "purge",
		Short: "Delete all self-hosted runners for the configured scope (waits for busy runners to go idle)",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := logging.New()

			cfg, err := config.Load(configPath)
			if err != nil {
				return err
			}
			token := os.Getenv("GITHUB_TOKEN")
			if token == "" {
				token = os.Getenv("GITHUB_PAT")
			}
			if token == "" {
				return fmt.Errorf("GITHUB_TOKEN (or GITHUB_PAT) is required in environment")
			}

			gh := github.New(token)

			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			logger.Printf("purge: starting (timeout=%s, interval=%s)", timeout, waitInterval)
			for {
				runners, err := gh.ListRunners(ctx, cfg.GitHub)
				if err != nil {
					return fmt.Errorf("list runners: %w", err)
				}
				if len(runners) == 0 {
					logger.Printf("purge: nothing to delete")
					return nil
				}

				deleted := 0
				busy := 0
				for _, rn := range runners {
					if rn.Busy || rn.Status == "busy" {
						busy++
						continue
					}
					if err := gh.DeleteRunner(ctx, cfg.GitHub, rn.ID); err != nil {
						return fmt.Errorf("delete runner %s (%d): %w", rn.Name, rn.ID, err)
					}
					deleted++
					logger.Printf("purge: deleted %s (%d)", rn.Name, rn.ID)
				}

				remaining := len(runners) - deleted
				if remaining == 0 {
					logger.Printf("purge: completed")
					return nil
				}
				logger.Printf("purge: remaining=%d busy=%d, waiting %s", remaining, busy, waitInterval)

				select {
				case <-ctx.Done():
					return fmt.Errorf("purge timeout: %w", ctx.Err())
				case <-time.After(waitInterval):
				}
			}
		},
	}

	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "Overall timeout for purge")
	cmd.Flags().DurationVar(&waitInterval, "interval", 5*time.Second, "Wait interval when runners are busy")

	return cmd
}
