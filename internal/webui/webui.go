// Package webui embeds the compiled dashboard (the dashboard/ directory, built
// by `make build`) so the hub can serve it. It contains no logic.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS returns the dashboard files rooted at the build output directory.
// It contains only a .gitkeep file until the dashboard has been built.
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // dist is embedded at compile time, so this cannot fail
	}
	return sub
}
