// Package version holds the build version as a single mutable leaf variable.
// main sets it from the -ldflags-stamped value before the cli runs, so any
// package (the hooks env, --version) can read it without an import cycle or
// threading it through every constructor.
package version

// Value is the treepi build version. main overrides it at startup; "dev" is the
// unstamped default.
var Value = "dev"
