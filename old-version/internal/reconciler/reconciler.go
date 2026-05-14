package reconciler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"gh-runners-tool/internal/config"
	"gh-runners-tool/internal/domain"
	"gh-runners-tool/internal/provider/github"
	"gh-runners-tool/internal/runner"
)

type Logger interface {
	Printf(string, ...any)
}

type Reconciler struct {
	logger  Logger
	gh      *github.Client
	runners *runner.Manager

	mu         sync.Mutex
	desired    *config.Config
	groupPools map[string]*slotPool
	ghCfg      config.GitHubConfig

	stopOnce sync.Once
}

func New(logger Logger, gh *github.Client, runners *runner.Manager) *Reconciler {
	return &Reconciler{
		logger:     logger,
		gh:         gh,
		runners:    runners,
		groupPools: make(map[string]*slotPool),
	}
}

func (r *Reconciler) SetDesired(cfg *config.Config) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.desired = cfg
	r.ghCfg = cfg.GitHub
}

func (r *Reconciler) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = 15 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	if err := r.reconcile(ctx); err != nil {
		r.logger.Printf("reconcile error: %v", err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := r.reconcile(ctx); err != nil {
				r.logger.Printf("reconcile error: %v", err)
			}
		}
	}
}

func (r *Reconciler) reconcile(ctx context.Context) error {
	r.mu.Lock()
	cfg := r.desired
	r.mu.Unlock()

	if cfg == nil {
		return fmt.Errorf("no desired config set")
	}

	desired := make(map[string]domain.Group, len(cfg.Groups))
	for _, g := range cfg.Groups {
		desired[g.Name] = domain.Group{
			Name:      g.Name,
			Count:     g.Count,
			Ephemeral: g.Ephemeral,
			Labels:    g.Labels,
			Workdir:   g.WorkdirBase,
			Version:   g.Version,
		}
	}

	// Remove pools for groups that are no longer desired.
	for name := range r.groupPools {
		if _, ok := desired[name]; !ok {
			r.groupPools[name].stop()
			delete(r.groupPools, name)
			r.logger.Printf("group %s stopped", name)
		}
	}

	// Ensure pools exist and match desired count.
	for name, grp := range desired {
		pool, ok := r.groupPools[name]
		if !ok {
			pool = newSlotPool(r.logger, r.gh, r.runners, grp, r.ghCfg)
			r.groupPools[name] = pool
			r.logger.Printf("group %s started with %d slots", name, grp.Count)
		}
		pool.update(grp)
	}

	return nil
}

// Shutdown stops all slots and runners when the daemon exits.
func (r *Reconciler) Shutdown(ctx context.Context) {
	r.stopOnce.Do(func() {
		r.mu.Lock()
		pools := r.snapshotPools()
		r.mu.Unlock()

		stopPools(pools)

		waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		waitPools(waitCtx, pools, r.logger)
	})
}

func (r *Reconciler) snapshotPools() []*slotPool {
	out := make([]*slotPool, 0, len(r.groupPools))
	for _, p := range r.groupPools {
		out = append(out, p)
	}
	return out
}

func stopPools(pools []*slotPool) {
	for _, p := range pools {
		p.stop()
	}
}

func waitPools(ctx context.Context, pools []*slotPool, logger Logger) {
	for _, p := range pools {
		p.wait(ctx)
	}
}

// Status returns a snapshot of current slots by group.
func (r *Reconciler) Status() map[string]int {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]int)
	for name, pool := range r.groupPools {
		out[name] = pool.size()
	}
	return out
}
