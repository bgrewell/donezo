//go:build !embedui

// Package webui carries the production web bundle in release builds.
//
// This stub compiles when the embedui build tag is absent (every dev
// build): nothing is embedded, Available reports false, and FS returns
// nil. It must keep compiling without internal/webui/dist existing —
// that directory is created and removed by the Makefile's release-build
// target and never committed.
package webui

import "io/fs"

// Available reports whether this build embeds the web UI. Always false
// without the embedui build tag.
func Available() bool { return false }

// FS returns nil: no web bundle is embedded in this build.
func FS() fs.FS { return nil }
