package auth

import (
	"fmt"
	"os"
)

func Load(opts LoadOpts) (*Credentials, string, error) {
	if opts.TokenFlag != "" {
		return &Credentials{
			Method: "pat",
			PAT:    opts.TokenFlag,
		}, "flag (--token)", nil
	}

	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return &Credentials{
			Method: "pat",
			PAT:    token,
		}, "env (GITHUB_TOKEN)", nil
	}

	creds, err := loadFromFile()
	if err == nil {
		return creds, fmt.Sprintf("file (%s)", FilePath()), nil
	}
	if !os.IsNotExist(err) {
		return nil, "", fmt.Errorf("load credentials file: %w", err)
	}

	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return &Credentials{
			Method: "pat",
			PAT:    token,
		}, "env (.env GITHUB_TOKEN)", nil
	}

	return nil, "", fmt.Errorf("not authenticated. Run 'ghr login' to set up authentication, or set GITHUB_TOKEN")
}
