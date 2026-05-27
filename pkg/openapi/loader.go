package openapi

import (
	"fmt"
	"net/url"
	"os"

	"github.com/getkin/kin-openapi/openapi3"
)

// Spec holds a loaded and validated OpenAPI spec plus derived config.
type Spec struct {
	Doc          *openapi3.T
	BaseURL      string   // resolved base URL for API calls
	AuthHeader   string   // e.g. "Bearer xxx" or "ApiKey yyy"
	AuthHeaderOK bool     // true if auth header is set
	Servers      []string // all server URLs from spec
}

// Load reads an OpenAPI 3.x spec from path (JSON or YAML), resolves the
// base URL, and returns a validated Spec.
func Load(path, baseURL, authHeader string) (*Spec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read spec: %w", err)
	}

	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(data)
	if err != nil {
		return nil, fmt.Errorf("parse spec: %w", err)
	}

	if err := doc.Validate(loader.Context); err != nil {
		return nil, fmt.Errorf("validate spec: %w", err)
	}

	// Collect server URLs
	servers := make([]string, 0, len(doc.Servers))
	for _, s := range doc.Servers {
		servers = append(servers, s.URL)
	}

	// Resolve base URL: CLI flag > spec servers[0] > empty
	resolved := baseURL
	if resolved == "" && len(servers) > 0 {
		resolved = servers[0]
	}

	// Normalize: strip trailing slash
	if resolved != "" {
		u, err := url.Parse(resolved)
		if err != nil {
			return nil, fmt.Errorf("invalid base URL %q: %w", resolved, err)
		}
		resolved = u.String()
		if resolved[len(resolved)-1] == '/' {
			resolved = resolved[:len(resolved)-1]
		}
	}

	return &Spec{
		Doc:          doc,
		BaseURL:      resolved,
		AuthHeader:   authHeader,
		AuthHeaderOK: authHeader != "",
		Servers:      servers,
	}, nil
}
