package doctor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

type CacheCheck struct {
	Path string
}

func (c CacheCheck) Name() string { return "cache" }

func (c CacheCheck) Run(ctx context.Context) Result {
	res := Result{Name: c.Name(), Details: []string{c.Path}}

	if c.Path == "" {
		res.Status = StatusSkip
		res.Summary = "no cache path configured"
		return res
	}

	if _, err := os.Stat(c.Path); err != nil {
		if os.IsNotExist(err) {
			res.Status = StatusSkip
			res.Summary = "cache directory does not exist yet"
			return res
		}
		res.Status = StatusFail
		res.Summary = fmt.Sprintf("stat cache: %v", err)
		return res
	}

	size, err := dirSize(ctx, c.Path)
	if err != nil {
		res.Status = StatusWarn
		res.Summary = fmt.Sprintf("could not measure cache: %v", err)
		return res
	}

	res.Status = StatusOK
	res.Summary = "cache size " + humanBytes(size)
	return res
}

func dirSize(ctx context.Context, root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		total += info.Size()
		return nil
	})
	return total, err
}
