# mcpify

Convert any REST API to an MCP server — one command, no code generation.

Feed it an OpenAPI 3.x spec, get an MCP stdio server that exposes every endpoint as a tool.

## Quick Start

```bash
# Install
go install github.com/xushiwang/mcpify/cmd/mcpify@latest

# Run: convert petstore.yaml to MCP tools
mcpify --spec petstore.yaml

# Try the included example (no API key needed, uses public httpbin.org)
mcpify --spec examples/httpbin.yaml

# With auth
mcpify --spec api.yaml --auth "Bearer sk-xxx"

# Override base URL
mcpify --spec api.yaml --base-url https://staging.example.com
```

Then configure in your MCP client (Claude Desktop, etc.):

```json
{
  "mcpServers": {
    "my-api": {
      "command": "mcpify",
      "args": ["--spec", "/path/to/openapi.yaml", "--auth", "Bearer sk-xxx"]
    }
  }
}
```

## How It Works

```
openapi.yaml  →  [Loader]  →  [Converter]  →  [MCP Server (stdio)]
                     │              │                  │
                 kin-openapi    operation→tool      mcp-go
                                                  ┌──↓──┐
               REST API  ←───  HTTP Client  ←───  │Tool │
                                                  └─────┘
```

1. **Loader** parses OpenAPI 3.x spec (YAML/JSON)
2. **Converter** maps each operation to an MCP tool:
   - `operationId` → tool name (or `{method}_{path}`)
   - summary/description → tool description
   - path/query/body params → tool input schema
3. **Server** runs a stdio MCP server
4. When a tool is called → builds HTTP request → calls API → returns response

## Library Usage

```go
import (
    "github.com/xushiwang/mcpify/pkg/openapi"
    "github.com/xushiwang/mcpify/pkg/server"
)

func main() {
    spec, _ := openapi.Load("api.yaml", "https://api.example.com", "Bearer sk-xxx")
    srv := server.New(spec)
    srv.Serve() // blocking, stdio transport
}
```

## Auth

Supports any auth header:

```bash
--auth "Bearer sk-abc123"           # Bearer token
--auth "ApiKey my-api-key"          # API key  
--auth "X-Custom-Header: value"     # Custom header
```

## Project Structure

```
mcpify/
├── cmd/mcpify/main.go         # CLI entry
├── pkg/
│   ├── openapi/loader.go      # Spec loading + validation
│   ├── converter/converter.go # OpenAPI → MCP tool mapping
│   └── server/server.go       # MCP server + HTTP client
├── mcpify_test.go             # Unit tests
├── integration_test.go        # MCP protocol integration test
└── go.mod
```

## Dependencies

| Package | Purpose |
|---------|---------|
| `mark3labs/mcp-go` | MCP protocol server (stdio transport) |
| `getkin/kin-openapi` | OpenAPI 3.x spec parser |
| stdlib `net/http` | HTTP client for API calls |

## License

MIT
