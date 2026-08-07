// Package version holds YACMO's build metadata. The values are overridden at
// build time via -ldflags, e.g.:
//
//	go build -ldflags "-X yacmo/pkg/version.Version=1.2.3 \
//	    -X yacmo/pkg/version.Commit=$(git rev-parse --short HEAD) \
//	    -X yacmo/pkg/version.Date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" .
//
// For local/un-versioned builds the defaults below are used.
package version

// Build metadata, injected via -ldflags at build time.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// String returns a human-readable one-line version string.
func String() string {
	return "YACMO " + Version + " — Yet Another Chaos Monkey (commit " + Commit + ", built " + Date + ")"
}
