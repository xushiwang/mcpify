package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/xushiwang/mcpify/pkg/converter"
	"github.com/xushiwang/mcpify/pkg/openapi"
)

// MCPifyServer wraps an MCP stdio server that exposes OpenAPI operations as tools.
type MCPifyServer struct {
	spec       *openapi.Spec
	operations []converter.Operation
	client     *http.Client
}

// New creates a new MCPifyServer from a loaded spec.
func New(spec *openapi.Spec) *MCPifyServer {
	return &MCPifyServer{
		spec:   spec,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// Serve starts the MCP stdio server. Blocks until stdin closes.
func (s *MCPifyServer) Serve() error {
	s.operations = converter.Convert(s.spec.Doc)

	if len(s.operations) == 0 {
		return fmt.Errorf("no operations found in spec")
	}

	mcpServer := mcpserver.NewMCPServer("mcpify", "1.0.0")

	for _, op := range s.operations {
		tool := converter.ToMCPTool(op)
		op := op // capture for closure
		mcpServer.AddTool(tool, s.makeHandler(op))
	}

	return mcpserver.ServeStdio(mcpServer)
}

// ServeIO is like Serve but allows injecting custom reader/writer for testing.
func (s *MCPifyServer) ServeIO(in io.Reader, out io.Writer) error {
	s.operations = converter.Convert(s.spec.Doc)

	if len(s.operations) == 0 {
		return fmt.Errorf("no operations found in spec")
	}

	mcpServer := mcpserver.NewMCPServer("mcpify", "1.0.0")

	for _, op := range s.operations {
		tool := converter.ToMCPTool(op)
		op := op // capture for closure
		mcpServer.AddTool(tool, s.makeHandler(op))
	}

	stdioServer := mcpserver.NewStdioServer(mcpServer)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return stdioServer.Listen(ctx, in, out)
}

// makeHandler returns a tool handler that calls the real API.
func (s *MCPifyServer) makeHandler(op converter.Operation) func(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return s.callAPI(ctx, op, req)
	}
}

// callAPI builds and executes the HTTP request, returning the response as text.
func (s *MCPifyServer) callAPI(
	ctx context.Context, op converter.Operation, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	// Build URL: resolve path parameters
	urlPath := op.PathTemplate
	var queryParts []string
	var bodyJSON []byte

	args := req.GetArguments()
	for _, p := range op.Parameters {
		val, ok := args[p.Name]
		if !ok || val == nil {
			if p.Required {
				return mcp.NewToolResultError(
					fmt.Sprintf("missing required parameter: %s", p.Name),
				), nil
			}
			continue
		}

		strVal := fmt.Sprintf("%v", val)

		switch p.In {
		case "path":
			urlPath = strings.ReplaceAll(urlPath, "{"+p.Name+"}", strVal)
		case "query":
			queryParts = append(queryParts, fmt.Sprintf("%s=%s", p.Name, strVal))
		case "body":
			bodyJSON = []byte(strVal)
		}
	}

	fullURL := s.spec.BaseURL + urlPath
	if len(queryParts) > 0 {
		fullURL += "?" + strings.Join(queryParts, "&")
	}

	// Build HTTP request
	var bodyReader io.Reader
	if bodyJSON != nil {
		bodyReader = bytes.NewReader(bodyJSON)
	}

	httpReq, err := http.NewRequestWithContext(ctx, op.Method, fullURL, bodyReader)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("build request: %v", err)), nil
	}

	if bodyJSON != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	httpReq.Header.Set("Accept", "application/json")

	if s.spec.AuthHeaderOK {
		parts := strings.SplitN(s.spec.AuthHeader, " ", 2)
		if len(parts) == 2 {
			httpReq.Header.Set("Authorization", s.spec.AuthHeader)
		} else {
			// Assume it's a raw header like "X-API-Key: value"
			kv := strings.SplitN(s.spec.AuthHeader, ":", 2)
			if len(kv) == 2 {
				httpReq.Header.Set(strings.TrimSpace(kv[0]), strings.TrimSpace(kv[1]))
			}
		}
	}

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("API call failed: %v", err)), nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("read response: %v", err)), nil
	}

	// Pretty-print if JSON
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, body, "", "  "); err == nil {
		body = pretty.Bytes()
	}

	result := fmt.Sprintf("HTTP %d %s\n\n%s", resp.StatusCode, http.StatusText(resp.StatusCode), string(body))

	return mcp.NewToolResultText(result), nil
}
