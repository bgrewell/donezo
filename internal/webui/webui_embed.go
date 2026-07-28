//go:build embedui

// Package webui carries the production web bundle in release builds.
//
// This file compiles only under the embedui build tag. The dist/
// directory it embeds is BUILD-TIME ONLY: the Makefile's release-build
// target copies web/dist to internal/webui/dist immediately before
// compiling with -tags embedui and removes it again afterwards. The
// directory is gitignored and absent from normal checkouts, which is
// exactly why the go:embed directive lives behind the tag — an untagged
// build must compile without dist existing (see webui_stub.go).
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var embedded embed.FS

// dist is the bundle re-rooted at its top level, so index.html sits at
// ".". Computed once at init; immutable afterwards.
var dist = mustSub(embedded, "dist")

// mustSub re-roots fsys at dir. The error branch is unreachable here —
// "dist" is a valid fs path and go:embed guarantees the directory
// exists in tagged builds — so a failure is a build-system bug worth
// crashing on at startup rather than serving a broken UI.
func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic("webui: re-root embedded bundle: " + err.Error())
	}
	return sub
}

// Available reports whether this build embeds the web UI. Always true
// under the embedui build tag.
func Available() bool { return true }

// FS returns the embedded production web bundle with index.html at its
// root.
func FS() fs.FS { return dist }
