package doctor

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
)

type SocketCheck struct {
	Path string
}

func (c SocketCheck) Name() string { return "socket" }

func (c SocketCheck) Run(ctx context.Context) Result {
	res := Result{Name: c.Name()}

	if _, err := os.Stat(c.Path); err != nil {
		if os.IsNotExist(err) {
			res.Status = StatusFail
			res.Summary = "daemon socket missing"
			res.Details = []string{c.Path}
			res.Hint = "start the daemon with 'ghr start'"
			return res
		}
		res.Status = StatusFail
		res.Summary = fmt.Sprintf("stat socket: %v", err)
		res.Details = []string{c.Path}
		return res
	}

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", c.Path)
			},
		},
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/health", nil)
	resp, err := client.Do(req)
	if err != nil {
		res.Status = StatusFail
		res.Summary = "socket present but unresponsive"
		res.Details = []string{c.Path, err.Error()}
		res.Hint = "daemon may be wedged; try 'ghr restart'"
		return res
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		res.Status = StatusWarn
		res.Summary = fmt.Sprintf("daemon /health returned %d", resp.StatusCode)
		res.Details = []string{c.Path}
		return res
	}

	res.Status = StatusOK
	res.Summary = "daemon reachable"
	res.Details = []string{c.Path}
	return res
}
