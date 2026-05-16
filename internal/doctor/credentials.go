package doctor

import (
	"context"
	"fmt"
	"os"
)

type CredentialsCheck struct {
	Path           string
	Method         string
	PrivateKeyPath string
}

func (c CredentialsCheck) Name() string { return "credentials" }

func (c CredentialsCheck) Run(_ context.Context) Result {
	res := Result{Name: c.Name(), Details: []string{c.Path}}

	info, err := os.Stat(c.Path)
	if err != nil {
		if os.IsNotExist(err) {
			res.Status = StatusFail
			res.Summary = "credentials file missing"
			res.Hint = "run 'ghr login' to set up authentication"
			return res
		}
		res.Status = StatusFail
		res.Summary = fmt.Sprintf("stat credentials: %v", err)
		return res
	}

	if perm := info.Mode().Perm(); perm != 0o600 {
		res.Status = StatusWarn
		res.Summary = fmt.Sprintf("credentials file has loose perms %#o", perm)
		res.Hint = fmt.Sprintf("chmod 600 %s", c.Path)
		return res
	}

	if c.Method == "github_app" && c.PrivateKeyPath != "" {
		keyInfo, kerr := os.Stat(c.PrivateKeyPath)
		if kerr != nil {
			res.Status = StatusFail
			res.Summary = fmt.Sprintf("github app private key unreadable: %v", kerr)
			res.Details = append(res.Details, c.PrivateKeyPath)
			res.Hint = "verify the path in the credentials file or rerun 'ghr login'"
			return res
		}
		if perm := keyInfo.Mode().Perm(); perm != 0o600 {
			res.Status = StatusWarn
			res.Summary = fmt.Sprintf("private key has loose perms %#o", perm)
			res.Details = append(res.Details, c.PrivateKeyPath)
			res.Hint = fmt.Sprintf("chmod 600 %s", c.PrivateKeyPath)
			return res
		}
		res.Details = append(res.Details, "method: github_app", "key: "+c.PrivateKeyPath)
	} else {
		res.Details = append(res.Details, "method: "+c.Method)
	}

	res.Status = StatusOK
	res.Summary = "credentials file ok (0600)"
	return res
}
