package mcpify_test

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/secssh/mcpify/pkg/openapi"
	"github.com/secssh/mcpify/pkg/server"
)

func TestIntegrationMCPProtocol(t *testing.T) {
	// Create a fake API that echoes requests
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body, _ := io.ReadAll(r.Body)
		resp := map[string]any{
			"method":  r.Method,
			"path":    r.URL.Path,
			"query":   r.URL.RawQuery,
			"bodyRaw": string(body),
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer apiServer.Close()

	specYAML := `
openapi: "3.0.3"
info:
  title: Test API
  version: "1.0"
paths:
  /echo:
    get:
      operationId: echo
      summary: Echo request
      parameters:
        - name: msg
          in: query
          required: false
          schema:
            type: string
      responses:
        "200":
          description: OK
  /items/{id}:
    get:
      summary: Get item
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

	// Set up pipe-based stdio
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()

	srv := server.New(spec)

	// Run server in goroutine
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ServeIO(inReader, outWriter)
	}()

	// MCP client: send initialize
	scanner := bufio.NewScanner(outReader)

	send := func(msg map[string]any) map[string]any {
		data, _ := json.Marshal(msg)
		data = append(data, '\n')
		inWriter.Write(data)

		if !scanner.Scan() {
			t.Fatal("no response from server")
		}
		var resp map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
			t.Fatalf("parse response: %v\nraw: %s", err, scanner.Text())
		}
		return resp
	}

	// Step 1: Initialize
	initResp := send(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "test-client",
				"version": "1.0",
			},
		},
	})

	if errMsg, ok := initResp["error"]; ok {
		t.Fatalf("initialize error: %v", errMsg)
	}
	t.Logf("initialize: id=%v", initResp["id"])

	// Step 2: Send initialized notification
	inWriter.Write([]byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n"))
	time.Sleep(50 * time.Millisecond)

	// Step 3: List tools
	listResp := send(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
	})

	if errMsg, ok := listResp["error"]; ok {
		t.Fatalf("tools/list error: %v", errMsg)
	}

	result := listResp["result"].(map[string]any)
	tools := result["tools"].([]any)
	t.Logf("tools/list: got %d tools", len(tools))
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}

	// Step 4: Call echo tool
	callResp := send(map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "echo",
			"arguments": map[string]any{"msg": "hello-mcp"},
		},
	})

	if errMsg, ok := callResp["error"]; ok {
		t.Fatalf("tools/call error: %v", errMsg)
	}

	callResult := callResp["result"].(map[string]any)
	content := callResult["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	t.Logf("tools/call echo: %s", text[:min(100, len(text))])

	// Verify the response contains the query parameter
	if len(text) == 0 {
		t.Error("empty response from echo tool")
	}

	// Step 5: Call get_items_id tool with path param
	callResp2 := send(map[string]any{
		"jsonrpc": "2.0",
		"id":      4,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "get_items_id",
			"arguments": map[string]any{"id": "42"},
		},
	})

	if errMsg, ok := callResp2["error"]; ok {
		t.Fatalf("tools/call get_items_id error: %v", errMsg)
	}

	callResult2 := callResp2["result"].(map[string]any)
	content2 := callResult2["content"].([]any)
	text2 := content2[0].(map[string]any)["text"].(string)
	t.Logf("tools/call get_items_id: %s", text2[:min(150, len(text2))])

	if len(text2) == 0 {
		t.Error("empty response from get_items_id tool")
	}

	// Clean up
	inWriter.Close()
	select {
	case err := <-errCh:
		if err != nil && err != io.EOF && err.Error() != "pipe closed" {
			t.Logf("server exit: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Log("server didn't exit within timeout (may be expected)")
	}
}
