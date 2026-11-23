package examples

import "embed"

// FS holds the example packs embedded in the CLI
//
//go:embed jig.yaml app-image commands ecosystem helm-chart library manifests stack
var FS embed.FS
