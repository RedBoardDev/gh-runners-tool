package github

import (
	"context"
	"fmt"

	"github.com/actions/scaleset"
)

func (c *Client) ResolveScaleSet(ctx context.Context, name string, runnerGroupID int, labels []string) (*scaleset.RunnerScaleSet, error) {
	existing, err := c.GetScaleSet(ctx, runnerGroupID, name)
	if err != nil {
		return nil, fmt.Errorf("resolve scale set %q: %w", name, err)
	}
	if existing != nil {
		return existing, nil
	}

	created, err := c.CreateScaleSet(ctx, name, runnerGroupID, labels)
	if err != nil {
		return nil, fmt.Errorf("resolve scale set %q: %w", name, err)
	}
	return created, nil
}
