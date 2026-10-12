// Package web embeds the static UIs into the binary:
// store/ (store manager), engineer/ (field engineer PWA), backoffice/.
package web

import (
	"embed"
	"io/fs"
)

//go:embed store engineer backoffice
var files embed.FS

// FS returns the root containing one directory per UI.
func FS() fs.FS { return files }
