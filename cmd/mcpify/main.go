package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/secssh/mcpify/pkg/openapi"
	"github.com/secssh/mcpify/pkg/server"
)

func main() {
	specPath := flag.String("spec", "", "Path to OpenAPI 3.x spec (YAML or JSON)")
	baseURL := flag.String("base-url", "", "Override base URL (default: from spec servers[0])")
	auth := flag.String("auth", "", "Auth header: 'Bearer <token>' or 'ApiKey <key>' or 'X-Custom: val'")
	flag.Parse()

	if *specPath == "" {
		fmt.Fprintln(os.Stderr, "Usage: mcpify --spec <openapi.yaml> [--base-url <url>] [--auth <header>]")
		fmt.Fprintln(os.Stderr, "\nExamples:")
		fmt.Fprintln(os.Stderr, "  mcpify --spec petstore.yaml")
		fmt.Fprintln(os.Stderr, "  mcpify --spec api.yaml --auth 'Bearer sk-xxx'")
		fmt.Fprintln(os.Stderr, "  mcpify --spec api.yaml --base-url https://staging.example.com --auth 'X-API-Key: abc123'")
		os.Exit(1)
	}

	spec, err := openapi.Load(*specPath, *baseURL, *auth)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading spec: %v\n", err)
		os.Exit(1)
	}

	srv := server.New(spec)
	if err := srv.Serve(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
