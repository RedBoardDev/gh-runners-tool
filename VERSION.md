# Versioning & Releases

ghr uses git tags as the single source of truth for versioning. No version file to maintain.

## Creating a release

```bash
git tag v1.0.0
git push --tags
```

This triggers the release workflow which builds binaries for darwin/linux (amd64 + arm64) and publishes a GitHub Release with archives and checksums.

## Version format

Follow [semver](https://semver.org):

| Tag | When |
|---|---|
| `v1.0.0` | First stable release |
| `v1.1.0` | New feature, backward compatible |
| `v1.0.1` | Bug fix |
| `v2.0.0` | Breaking change |
| `v1.0.0-rc.1` | Pre-release (marked automatically) |

## How it works

The version is injected at build time via Go ldflags. The Makefile and GoReleaser both inject `version`, `commit`, and `date` into the binary. Running `ghr version` prints these values.

When building manually without ldflags (`go build ./cmd/ghr`), the version defaults to `dev`.

## Fixing a bad tag

```bash
git tag -d v1.0.0                   # delete locally
git push --delete origin v1.0.0     # delete on remote
git tag v1.0.1                      # create correct tag
git push --tags
```

Delete the corresponding GitHub Release manually if it was already published.
