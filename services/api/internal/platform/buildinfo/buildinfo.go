// Package buildinfo carries the release identity stamped in at build time:
//
//	go build -ldflags "-X mise.tt/api/internal/platform/buildinfo.Version=1.4.0 -X mise.tt/api/internal/platform/buildinfo.Commit=abc123"
//
// A binary built without them reports "dev", so an unstamped build is never
// mistaken for a release.
package buildinfo

var (
	Version = "dev"
	Commit  = ""
)
