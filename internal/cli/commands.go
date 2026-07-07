// Package cli implements the InfraMap command-line interface using Cobra.
package cli

import (
	"context"
	"embed"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/inframap/inframap/internal/api"
	"github.com/inframap/inframap/internal/export"
	"github.com/inframap/inframap/internal/provider"
	awsprovider "github.com/inframap/inframap/providers/aws"
	azureprovider "github.com/inframap/inframap/providers/azure"
	dockerprovider "github.com/inframap/inframap/providers/docker"
	gcpprovider "github.com/inframap/inframap/providers/gcp"
	mockprovider "github.com/inframap/inframap/providers/mock"
)

// Version is set at build time via ldflags.
var Version = "dev"

// Execute runs the root command. staticFS is the embedded frontend.
func Execute(staticFS embed.FS) {
	registry := provider.NewRegistry()

	rootCmd := &cobra.Command{
		Use:   "inframap",
		Short: "InfraMap — Live Infrastructure Digital Twin",
		Long: `InfraMap discovers your infrastructure and renders it as an
interactive, navigable graph. Think "Google Maps for your infrastructure."

Supports: AWS (mock), Docker, Kubernetes, Terraform.`,
		Version: Version,
	}

	// ─── serve ──────────────────────────────────────────
	var servePort int
	var serveProviders string

	serveCmd := &cobra.Command{
		Use:   "serve",
		Short: "Discover infrastructure and launch the web dashboard",
		Example: `  inframap serve
  inframap serve --providers=mock,docker
  inframap serve --port=9090 --providers=mock`,
		RunE: func(cmd *cobra.Command, args []string) error {
			registerProviders(registry, parseProviders(serveProviders))

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Handle Ctrl+C
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sigCh
				fmt.Println("\n👋 Shutting down...")
				cancel()
			}()

			// "auto" expands to whichever providers registerProviders actually
			// registered — the registry's own names, not the literal filter string.
			providerNames := registry.Names()

			srv := api.NewServer(registry, staticFS, servePort)
			return srv.Run(ctx, providerNames)
		},
	}
	serveCmd.Flags().IntVar(&servePort, "port", 8080, "Port for the web dashboard")
	serveCmd.Flags().StringVar(&serveProviders, "providers", "auto", "Comma-separated providers (auto, mock, aws, azure, gcp, docker)")

	// ─── discover ───────────────────────────────────────
	var discoverProviders string
	var discoverOutput string

	discoverCmd := &cobra.Command{
		Use:   "discover",
		Short: "Discover infrastructure and export to a file",
		Example: `  inframap discover --output=graph.json
  inframap discover --providers=docker --output=infra.json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			registerProviders(registry, parseProviders(discoverProviders))

			ctx := context.Background()
			providerNames := parseProviders(discoverProviders)
			if len(providerNames) == 0 {
				providerNames = registry.Names()
			}

			log.Println("🔍 Discovering infrastructure...")
			g, err := registry.DiscoverAll(ctx, providerNames)
			if err != nil {
				return err
			}

			stats := g.Stats()
			log.Printf("✅ Discovered %d nodes and %d edges", stats.TotalNodes, stats.TotalEdges)

			data, err := export.Export(g, export.FormatJSON)
			if err != nil {
				return err
			}

			if discoverOutput == "" {
				fmt.Println(data)
			} else {
				if err := os.WriteFile(discoverOutput, []byte(data), 0644); err != nil {
					return err
				}
				log.Printf("📄 Exported to %s", discoverOutput)
			}

			return nil
		},
	}
	discoverCmd.Flags().StringVar(&discoverProviders, "providers", "mock", "Comma-separated list of providers")
	discoverCmd.Flags().StringVar(&discoverOutput, "output", "", "Output file path (stdout if empty)")

	// ─── export ─────────────────────────────────────────
	var exportFormat string
	var exportProviders string
	var exportOutput string

	exportCmd := &cobra.Command{
		Use:   "export",
		Short: "Export infrastructure graph to various formats",
		Example: `  inframap export --format=dot
  inframap export --format=mermaid --output=infra.md
  inframap export --format=json --providers=docker`,
		RunE: func(cmd *cobra.Command, args []string) error {
			registerProviders(registry, parseProviders(exportProviders))

			ctx := context.Background()
			providerNames := parseProviders(exportProviders)
			if len(providerNames) == 0 {
				providerNames = registry.Names()
			}

			g, err := registry.DiscoverAll(ctx, providerNames)
			if err != nil {
				return err
			}

			data, err := export.Export(g, export.Format(exportFormat))
			if err != nil {
				return err
			}

			if exportOutput == "" {
				fmt.Println(data)
			} else {
				if err := os.WriteFile(exportOutput, []byte(data), 0644); err != nil {
					return err
				}
				log.Printf("📄 Exported to %s (%s format)", exportOutput, exportFormat)
			}

			return nil
		},
	}
	exportCmd.Flags().StringVar(&exportFormat, "format", "json", "Output format: json, dot, mermaid, terraform")
	exportCmd.Flags().StringVar(&exportProviders, "providers", "mock", "Comma-separated list of providers")
	exportCmd.Flags().StringVar(&exportOutput, "output", "", "Output file path (stdout if empty)")

	// ─── providers ──────────────────────────────────────
	providersCmd := &cobra.Command{
		Use:   "providers",
		Short: "List available infrastructure providers",
		Run: func(cmd *cobra.Command, args []string) {
			// Register all known providers to list them
			registerProviders(registry, []string{"mock", "docker", "aws"})

			fmt.Println("Available providers:")
			fmt.Println()
			for _, info := range registry.List() {
				status := "✅"
				if !info.Enabled {
					status = "❌"
				}
				fmt.Printf("  %s  %-12s  %s\n", status, info.Name, info.Description)
			}
			fmt.Println()
		},
	}

	rootCmd.AddCommand(serveCmd, discoverCmd, exportCmd, providersCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// ─── Helpers ────────────────────────────────────────────

func parseProviders(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func registerProviders(registry *provider.Registry, names []string) {
	for _, name := range names {
		switch name {
		case "auto":
			autoDetect(registry)
		case "mock":
			registry.Register(mockprovider.New())
		case "docker":
			d, err := dockerprovider.New()
			if err != nil {
				log.Printf("⚠️  Docker provider unavailable: %v", err)
				continue
			}
			registry.Register(d)
		case "aws":
			a, err := awsprovider.New(context.Background())
			if err != nil {
				log.Printf("⚠️  AWS provider unavailable: %v", err)
				continue
			}
			registry.Register(a)
		case "azure":
			a, err := azureprovider.New()
			if err != nil {
				log.Printf("⚠️  Azure provider unavailable: %v", err)
				continue
			}
			registry.Register(a)
		case "gcp":
			p, err := gcpprovider.New()
			if err != nil {
				log.Printf("⚠️  GCP provider unavailable: %v", err)
				continue
			}
			registry.Register(p)
		default:
			log.Printf("⚠️  Unknown provider: %s", name)
		}
	}
}

// autoDetect registers every provider whose local environment is usable
// (AWS creds, docker socket, az/gcloud logins). Falls back to mock so the
// dashboard is never empty on first run.
func autoDetect(registry *provider.Registry) {
	found := 0
	if a, err := awsprovider.New(context.Background()); err == nil {
		registry.Register(a)
		log.Println("🔎 auto: AWS credentials found")
		found++
	}
	if d, err := dockerprovider.New(); err == nil {
		registry.Register(d)
		log.Println("🔎 auto: Docker engine found")
		found++
	}
	if a, err := azureprovider.New(); err == nil {
		registry.Register(a)
		log.Println("🔎 auto: Azure CLI found")
		found++
	}
	if p, err := gcpprovider.New(); err == nil {
		registry.Register(p)
		log.Println("🔎 auto: gcloud CLI found")
		found++
	}
	if found == 0 {
		log.Println("🔎 auto: no live environments detected — using mock demo data")
		registry.Register(mockprovider.New())
	}
}
