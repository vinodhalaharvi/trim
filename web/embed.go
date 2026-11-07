// Package web holds the browser editor served by `trim serve`.
package web

import "embed"

//go:embed index.html
var FS embed.FS
