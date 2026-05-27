package mcpify_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/secssh/mcpify/pkg/converter"
	"github.com/secssh/mcpify/pkg/openapi"
)

func TestConverter(t *testing.T) {
	specYAML := `
openapi: "3.0.3"
info:
  title: Test API
  version: "1.0"
servers:
  - url: https://example.com
paths:
  /users:
    get:
      operationId: listUsers
      summary: List all users
      parameters:
        - name: limit
          in: query
          required: false
          schema:
            type: integer
          description: Max results
      responses:
        "200":
          description: OK
  /users/{id}:
    get:
      summary: Get user by ID
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: OK
    post:
      summary: Create a user
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
      responses:
        "201":
          description: Created
`

	tmpFile := t.TempDir() + "/test.yaml"
	if err := os.WriteFile(tmpFile, []byte(specYAML), 0644); err != nil {
		t.Fatal(err)
	}

	spec, err := openapi.Load(tmpFile, "", "")
	if err != nil {
		t.Fatal(err)
	}

	ops := converter.Convert(spec.Doc)
	if len(ops) != 3 {
		t.Fatalf("expected 3 operations, got %d", len(ops))
	}

	names := make(map[string]bool)
	for _, op := range ops {
		names[op.ToolName] = true
	}

	if !names["listUsers"] {
		t.Error("missing listUsers")
	}
	if !names["get_users_id"] {
		t.Error("missing get_users_id (generated name)")
	}
	if !names["post_users_id"] {
		t.Error("missing post_users_id (generated name)")
	}

	// Verify listUsers has correct parameters
	for _, op := range ops {
		if op.ToolName == "listUsers" {
			if len(op.Parameters) != 1 {
				t.Errorf("listUsers: expected 1 param, got %d", len(op.Parameters))
			}
			if op.Parameters[0].Name != "limit" || op.Parameters[0].In != "query" {
				t.Errorf("listUsers param mismatch: %+v", op.Parameters[0])
			}
		}
		if op.ToolName == "get_users_id" {
			if len(op.Parameters) != 1 || op.Parameters[0].Name != "id" || !op.Parameters[0].Required {
				t.Errorf("get_users_id param mismatch: %+v", op.Parameters)
			}
		}
		if op.ToolName == "post_users_id" {
			hasBody := false
			for _, p := range op.Parameters {
				if p.In == "body" && p.Required {
					hasBody = true
				}
			}
			if !hasBody {
				t.Error("post_users_id missing required body param")
			}
		}
	}

	// Verify MCP tool generation doesn't panic
	for _, op := range ops {
		tool := converter.ToMCPTool(op)
		if tool.Name != op.ToolName {
			t.Errorf("tool name mismatch: %q vs %q", tool.Name, op.ToolName)
		}
	}
}

func TestURLCalling(t *testing.T) {
	// Create a fake API server
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/users" && r.Method == "GET" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"users": []map[string]any{{"id": 1, "name": "Alice"}},
			})
			return
		}
		if r.URL.Path == "/users/42" && r.Method == "GET" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"id": 42, "name": "Bob"})
			return
		}
		w.WriteHeader(404)
	}))
	defer apiServer.Close()

	// Test that the URL path resolution works
	specYAML := `
openapi: "3.0.3"
info:
  title: Test API
  version: "1.0"
paths:
  /users:
    get:
      operationId: listUsers
      summary: List users
      responses:
        "200":
          description: OK
  /users/{id}:
    get:
      operationId: getUser
      summary: Get user
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: OK
`

	tmpFile := t.TempDir() + "/test.yaml"
	if err := os.WriteFile(tmpFile, []byte(specYAML), 0644); err != nil {
		t.Fatal(err)
	}

	spec, err := openapi.Load(tmpFile, apiServer.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if spec.BaseURL != apiServer.URL {
		t.Fatalf("base URL mismatch: %q vs %q", spec.BaseURL, apiServer.URL)
	}

	ops := converter.Convert(spec.Doc)
	if len(ops) != 2 {
		t.Fatalf("expected 2 operations, got %d", len(ops))
	}

	// Simulate URL building (what server.go does)
	for _, op := range ops {
		url := spec.BaseURL + op.PathTemplate

		// Resolve path params
		if op.ToolName == "getUser" {
			url = strings.ReplaceAll(url, "{id}", "42")
		}

		resp, err := http.Get(url)
		if err != nil {
			t.Fatalf("%s %s: %v", op.Method, url, err)
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if resp.StatusCode != 200 {
			t.Errorf("%s %s: status %d, body: %s", op.Method, url, resp.StatusCode, body)
			continue
		}

		var pretty bytes.Buffer
		if err := json.Indent(&pretty, body, "", "  "); err == nil {
			body = pretty.Bytes()
		}

		t.Logf("%s %s → %d\n%s", op.Method, url, resp.StatusCode, body)

		// Verify we got valid JSON with expected data
		if op.ToolName == "getUser" {
			if !strings.Contains(string(body), "Bob") {
				t.Errorf("getUser response doesn't contain 'Bob': %s", body)
			}
		}
		if op.ToolName == "listUsers" {
			if !strings.Contains(string(body), "Alice") {
				t.Errorf("listUsers response doesn't contain 'Alice': %s", body)
			}
		}
	}

	fmt.Println("\n✓ All URL-calling tests passed")
}
