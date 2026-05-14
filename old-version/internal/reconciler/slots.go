package reconciler

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"gh-runners-tool/internal/config"
	"gh-runners-tool/internal/domain"
	"gh-runners-tool/internal/provider/github"
	"gh-runners-tool/internal/runner"
)

type slotPool struct {
	logger  Logger
	gh      *github.Client
	runners *runner.Manager
	ghCfg   config.GitHubConfig

	mu       sync.Mutex
	group    domain.Group
	slots    map[int]context.CancelFunc
	wg       sync.WaitGroup
	stopping bool
}

func newSlotPool(logger Logger, gh *github.Client, runners *runner.Manager, group domain.Group, ghCfg config.GitHubConfig) *slotPool {
	return &slotPool{
		logger:  logger,
		gh:      gh,
		runners: runners,
		ghCfg:   ghCfg,
		group:   group,
		slots:   make(map[int]context.CancelFunc),
	}
}

func (p *slotPool) update(group domain.Group) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.group = group
	target := group.Count
	current := len(p.slots)

	if target > current {
		for i := current; i < target; i++ {
			p.startSlotLocked(i)
		}
	}
	if target < current {
		diff := current - target
		i := 0
		for id, cancel := range p.slots {
			if i >= diff {
				break
			}
			cancel()
			delete(p.slots, id)
			i++
		}
	}
}

func (p *slotPool) startSlotLocked(id int) {
	ctx, cancel := context.WithCancel(context.Background())
	p.slots[id] = cancel
	p.wg.Add(1)
	go p.runSlot(ctx, id)
}

func (p *slotPool) runSlot(ctx context.Context, slotID int) {
	defer p.wg.Done()

	const (
		minBackoff = 2 * time.Second
		maxBackoff = 30 * time.Second
	)
	backoff := minBackoff

	for {
		group := p.currentGroup()

		select {
		case <-ctx.Done():
			return
		default:
		}

		token, err := p.gh.RegistrationToken(ctx, p.ghCfg)
		if err != nil {
			p.logger.Printf("slot %d group=%s: registration token: %v", slotID, group.Name, err)
			if !sleepOrDone(ctx, jitter(backoff)) {
				return
			}
			if backoff < maxBackoff {
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
			}
			continue
		}

		inst := runner.NewRunnerInstance(group)
		handle, err := p.runners.Start(ctx, inst, p.ghCfg, token)
		if err != nil {
			p.logger.Printf("slot %d group=%s: start runner: %v", slotID, group.Name, err)
			if !sleepOrDone(ctx, jitter(backoff)) {
				return
			}
			if backoff < maxBackoff {
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
			}
			continue
		}

		backoff = minBackoff

		err = handle.Wait()
		if err != nil {
			p.logger.Printf("slot %d group=%s: runner %s exited with error: %v", slotID, group.Name, handle.ID, err)
		} else {
			p.logger.Printf("slot %d group=%s: runner %s exited normally", slotID, group.Name, handle.ID)
		}

		go func(h *runner.Handle) {
			ctxUnreg, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			if err := p.unregister(ctxUnreg, h); err != nil {
				p.logger.Printf("slot %d group=%s: unregister %s: %v", slotID, group.Name, h.ID, err)
			}
		}(handle)

		if !sleepOrDone(ctx, jitter(minBackoff)) {
			return
		}
	}
}

func (p *slotPool) unregister(ctx context.Context, h *runner.Handle) error {
	name := runnerName(h.Group, h.ID)
	runners, err := p.gh.ListRunners(ctx, p.ghCfg)
	if err != nil {
		return err
	}
	for _, rn := range runners {
		if rn.Name == name {
			return p.gh.DeleteRunner(ctx, p.ghCfg, rn.ID)
		}
	}
	return nil
}

func (p *slotPool) stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopping {
		return
	}
	p.stopping = true
	for _, cancel := range p.slots {
		cancel()
	}
}

func (p *slotPool) wait(ctx context.Context) {
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		p.logger.Printf("slot pool group=%s wait timeout", p.group.Name)
	}
}

func sleepOrDone(ctx context.Context, d time.Duration) bool {
	select {
	case <-time.After(d):
		return true
	case <-ctx.Done():
		return false
	}
}

func (p *slotPool) size() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.slots)
}

func (p *slotPool) currentGroup() domain.Group {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.group
}

func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return time.Second
	}
	// * Apply ±20% jitter to avoid thundering herd on retries.
	delta := d / 5
	if delta <= 0 {
		delta = time.Millisecond
	}
	offset := rand.Int63n(int64(delta)*2+1) - int64(delta)
	out := d + time.Duration(offset)
	if out < time.Millisecond {
		return time.Millisecond
	}
	return out
}

func init() {
	rand.Seed(time.Now().UnixNano())
}

func runnerName(group, id string) string {
	return fmt.Sprintf("%s-%s", group, id)
}
