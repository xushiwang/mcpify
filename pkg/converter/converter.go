package converter

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/mark3labs/mcp-go/mcp"
)

// Operation represents a single API operation extracted from the spec.
type Operation struct {
	Method      string // GET, POST, PUT, DELETE, PATCH
	Path        string // e.g. /users/{id}
	ToolName    string // MCP tool name
	Description string
	// Parameters extracted from path, query, header, and requestBody.
	Parameters []ParamDef
	// The resolved path template for actual HTTP calls (base URL already applied).
	PathTemplate string
}

// ParamDef describes a single input parameter for a tool.
type ParamDef struct {
	Name        string
	In          string // path, query, header
	Required    bool
	Type        string // string, number, integer, boolean
	Description string
}

// Convert extracts all operations from an OpenAPI spec and converts them
// to a flat list of Operation structs.
func Convert(doc *openapi3.T) []Operation {
	var ops []Operation

	for path, pathItem := range doc.Paths.Map() {
		methods := map[string]*openapi3.Operation{
			"GET":    pathItem.Get,
			"POST":   pathItem.Post,
			"PUT":    pathItem.Put,
			"DELETE": pathItem.Delete,
			"PATCH":  pathItem.Patch,
		}

		for method, op := range methods {
			if op == nil {
				continue
			}
			ops = append(ops, buildOperation(method, path, op))
		}
	}

	return ops
}

func buildOperation(method, path string, op *openapi3.Operation) Operation {
	name := op.OperationID
	if name == "" {
		name = methodToToolName(method, path)
	}

	desc := op.Summary
	if desc == "" {
		desc = op.Description
	}
	if desc == "" {
		desc = fmt.Sprintf("%s %s", method, path)
	}

	var params []ParamDef

	// Path parameters
	for _, p := range op.Parameters {
		if p.Value == nil {
			continue
		}
		params = append(params, ParamDef{
			Name:        p.Value.Name,
			In:          p.Value.In,
			Required:    p.Value.Required,
			Type:        schemaType(p.Value.Schema),
			Description: p.Value.Description,
		})
	}

	// Request body (if JSON)
	if op.RequestBody != nil && op.RequestBody.Value != nil {
		mt := op.RequestBody.Value.Content
		if jsonMT, ok := mt["application/json"]; ok && jsonMT.Schema != nil {
			params = append(params, ParamDef{
				Name:        "body",
				In:          "body",
				Required:    op.RequestBody.Value.Required,
				Type:        "object",
				Description: "Request body as JSON",
			})
		}
	}

	return Operation{
		Method:       method,
		Path:         path,
		ToolName:     name,
		Description:  desc,
		Parameters:   params,
		PathTemplate: path,
	}
}

// ToMCPTool converts an Operation into an mcp.Tool with the matching
// input schema (JSON Schema for the parameters).
func ToMCPTool(op Operation) mcp.Tool {
	opts := []mcp.ToolOption{
		mcp.WithDescription(op.Description),
	}

	for _, p := range op.Parameters {
		desc := mcp.Description(p.Description)
		var opt mcp.ToolOption

		switch p.In {
		case "path", "query", "header":
			opt = typedParam(p.Name, p.Type, p.Required, desc)
		case "body":
			// Body is always sent as JSON string
			if p.Required {
				opt = mcp.WithString(p.Name, mcp.Required(), desc)
			} else {
				opt = mcp.WithString(p.Name, desc)
			}
		}
		opts = append(opts, opt)
	}

	return mcp.NewTool(op.ToolName, opts...)
}

// schemaType maps an OpenAPI schema to a simple type string.
func schemaType(s *openapi3.SchemaRef) string {
	if s == nil || s.Value == nil || s.Value.Type == nil || len(*s.Value.Type) == 0 {
		return "string"
	}
	return string((*s.Value.Type)[0])
}

// typedParam returns a ToolOption with the correct MCP type for the OpenAPI type.
func typedParam(name, oapiType string, required bool, desc mcp.PropertyOption) mcp.ToolOption {
	base := []mcp.PropertyOption{desc}
	if required {
		base = append([]mcp.PropertyOption{mcp.Required()}, base...)
	}
	switch oapiType {
	case "integer", "number":
		return mcp.WithNumber(name, base...)
	case "boolean":
		return mcp.WithBoolean(name, base...)
	default:
		return mcp.WithString(name, base...)
	}
}

var nonAlpha = regexp.MustCompile(`[^a-zA-Z0-9]+`)

// methodToToolName generates a tool name like "get_users" from method and path.
func methodToToolName(method, path string) string {
	slug := strings.ToLower(method) + "_" + path
	slug = strings.ReplaceAll(slug, "/", "_")
	slug = strings.ReplaceAll(slug, "{", "")
	slug = strings.ReplaceAll(slug, "}", "")
	slug = nonAlpha.ReplaceAllString(slug, "_")
	slug = strings.Trim(slug, "_")
	if len(slug) > 64 {
		slug = slug[:64]
	}
	return slug
}
