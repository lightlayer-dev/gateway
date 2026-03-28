package gateway

import "embed"

// UIAssets contains the built dashboard UI files.
// When the ui/dist directory doesn't exist (e.g., during development without
// a frontend build), the embed will be empty and the admin server will skip
// serving the UI.
//
//go:embed all:ui/dist
var UIAssets embed.FS
