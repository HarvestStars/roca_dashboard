// Package web embeds the static store-manager UI into the binary.
package web

import (
	"embed"
	"io/fs"
)

//go:embed store
var files embed.FS

// Store returns the store-manager UI rooted at web/store.
func Store() fs.FS {
	sub, _ := fs.Sub(files, "store")
	return sub
}
