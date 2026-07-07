// InfraMap — Live Infrastructure Digital Twin
//
// Entry point. Embeds the static frontend and launches the CLI.
package main

import (
	"embed"

	"github.com/inframap/inframap/internal/cli"
)

//go:embed web/static/*
var staticFS embed.FS

func main() {
	cli.Execute(staticFS)
}
