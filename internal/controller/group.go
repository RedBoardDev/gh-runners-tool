package controller

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"time"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/config"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/logging"
	"github.com/actions/scaleset"
)

const (
	backoffMin = 2 * time.Second
	backoffMax = 30 * time.Second
)

func (c *GroupController) runGroup(ctx context.Context, group *config.GroupConfig) error {
	version := group.Version
	if version == "" {
		version = c.globalCfg.RunnerVersion
	}

	cachedDir, err := c.binary.EnsureBits(ctx, version)
	if err != nil {
		return fmt.Errorf("ensure runner bits for group %q: %w", group.Name, err)
	}

	groupLogger, err := c.logMgr.GroupLogger(group.Name)
	if err != nil {
		return fmt.Errorf("create group logger for %q: %w", group.Name, err)
	}

	labels := deduplicateLabels(group.Name, group.Labels)

	backoff := backoffMin
	for {
		err := c.runGroupOnce(ctx, group, cachedDir, labels, groupLogger)
		if err == nil || errors.Is(err, context.Canceled) {
			return nil
		}

		groupLogger.ErrorContext(ctx, "group listener failed, retrying",
			logging.KeyGroup, group.Name,
			logging.KeyError, err,
			"backoff", backoff,
		)

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}

		backoff = nextBackoff(backoff)
	}
}

func (c *GroupController) runGroupOnce(
	ctx context.Context,
	group *config.GroupConfig,
	cachedDir string,
	labels []string,
	groupLogger *slog.Logger,
) error {
	ss, err := c.resolveScaleSet(ctx, group.Name, labels)
	if err != nil {
		return fmt.Errorf("resolve scale set %q: %w", group.Name, err)
	}

	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}

	session, err := c.client.OpenSession(ctx, ss.ID, hostname)
	if err != nil {
		return fmt.Errorf("open session for %q: %w", group.Name, err)
	}
	defer func() {
		closeCtx := context.WithoutCancel(ctx)
		if closeErr := session.Close(closeCtx); closeErr != nil {
			groupLogger.DebugContext(ctx, "session close",
				logging.KeyGroup, group.Name,
				logging.KeyError, closeErr,
			)
		}
	}()

	scaler := NewMacOSScaler(
		c.client, c.process, c.logMgr, c.notifier,
		ss.ID, group.Name, group.MaxRunners, group.MinRunners,
		cachedDir, groupLogger,
	)
	c.registerScaler(group.Name, scaler)

	l, err := c.client.NewListener(session, ss.ID, group.MaxRunners)
	if err != nil {
		c.unregisterScaler(group.Name)
		return fmt.Errorf("create listener for %q: %w", group.Name, err)
	}

	groupLogger.InfoContext(ctx, "group listener started",
		logging.KeyGroup, group.Name,
		"scale_set_id", ss.ID,
	)

	listenerErr := l.Run(ctx, scaler)

	c.unregisterScaler(group.Name)

	if errors.Is(listenerErr, context.Canceled) {
		scaler.Shutdown(ctx)
		cleanupCtx := context.WithoutCancel(ctx)
		deleteErr := c.client.DeleteScaleSet(cleanupCtx, ss.ID)
		if deleteErr != nil {
			groupLogger.WarnContext(ctx, "failed to delete scale set on shutdown",
				logging.KeyGroup, group.Name,
				"scale_set_id", ss.ID,
				logging.KeyError, deleteErr,
			)
		}
		return context.Canceled
	}

	return listenerErr
}

func (c *GroupController) resolveScaleSet(ctx context.Context, name string, labels []string) (*resolvedScaleSet, error) {
	ss, err := c.client.GetScaleSet(ctx, c.globalCfg.RunnerGroupID, name)
	if err == nil && ss != nil {
		if labelsChanged(ss.Labels, labels) {
			c.logger.WarnContext(ctx, "scale set label mismatch detected, delete and recreate to update",
				logging.KeyGroup, name,
				"scale_set_id", ss.ID,
			)
		}
		return &resolvedScaleSet{ID: ss.ID, Name: ss.Name}, nil
	}

	ss, err = c.client.CreateScaleSet(ctx, name, c.globalCfg.RunnerGroupID, labels)
	if err != nil {
		return nil, fmt.Errorf("create scale set %q: %w", name, err)
	}
	return &resolvedScaleSet{ID: ss.ID, Name: ss.Name}, nil
}

func labelsChanged(existing []scaleset.Label, desired []string) bool {
	if len(existing) != len(desired) {
		return true
	}
	have := make(map[string]struct{}, len(existing))
	for _, l := range existing {
		have[l.Name] = struct{}{}
	}
	for _, d := range desired {
		if _, ok := have[d]; !ok {
			return true
		}
	}
	return false
}

type resolvedScaleSet struct {
	ID   int
	Name string
}

func deduplicateLabels(groupName string, extra []string) []string {
	seen := make(map[string]struct{}, len(extra)+1)
	result := make([]string, 0, len(extra)+1)

	for _, label := range append([]string{groupName}, extra...) {
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		result = append(result, label)
	}
	return result
}

func nextBackoff(current time.Duration) time.Duration {
	next := current * 2
	if next > backoffMax {
		next = backoffMax
	}
	// ±20% jitter to spread retries across groups that all failed at the same tick.
	jitter := time.Duration((rand.Float64()*0.4 - 0.2) * float64(next))
	return next + jitter
}
