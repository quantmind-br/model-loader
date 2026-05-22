// Package assets embeds the config web GUI templates and static files.
package assets

import "embed"

//go:embed templates/*.gohtml static/*
var FS embed.FS
