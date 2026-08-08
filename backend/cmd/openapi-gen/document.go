// Command openapi-gen builds docs/openapi.json from two real sources: the
// route table cmd/api/*.go actually registers (parsed from source, not
// hand-copied — see routes.go) and the DTO structs in internal/apidoc
// (reflected into JSON Schema — see schema.go). See routes.go for exactly
// what "generated" does and doesn't guarantee.
package main

// This file is a minimal OpenAPI 3.0.3 document model — just the fields
// this generator actually populates, not the full spec. A dependency the
// size of kin-openapi wasn't worth it for what's otherwise a ~500-line tool
// with zero other external dependencies (matching this backend's existing
// "no framework, and it doesn't need one at this route count" bar for
// cmd/api itself).

type Document struct {
	OpenAPI    string              `json:"openapi"`
	Info       Info                `json:"info"`
	Servers    []Server            `json:"servers"`
	Paths      map[string]PathItem `json:"paths"`
	Components Components          `json:"components"`
}

type Info struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Version     string `json:"version"`
}

type Server struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

// PathItem maps HTTP method (lowercase) -> Operation for one path. A plain
// map here (rather than named Get/Post/... fields) keeps routes.go simple
// since it discovers methods dynamically from source.
type PathItem map[string]Operation

type Operation struct {
	Summary     string                `json:"summary,omitempty"`
	Tags        []string              `json:"tags,omitempty"`
	Parameters  []Parameter           `json:"parameters,omitempty"`
	RequestBody *RequestBody          `json:"requestBody,omitempty"`
	Responses   map[string]Response   `json:"responses"`
	Security    []map[string][]string `json:"security,omitempty"`
}

type Parameter struct {
	Name     string  `json:"name"`
	In       string  `json:"in"` // "path" or "query"
	Required bool    `json:"required"`
	Schema   *Schema `json:"schema"`
}

type RequestBody struct {
	Required bool                 `json:"required"`
	Content  map[string]MediaType `json:"content"`
}

type Response struct {
	Description string               `json:"description"`
	Content     map[string]MediaType `json:"content,omitempty"`
}

type MediaType struct {
	Schema *Schema `json:"schema,omitempty"`
}

type Components struct {
	Schemas         map[string]*Schema        `json:"schemas"`
	SecuritySchemes map[string]SecurityScheme `json:"securitySchemes"`
}

type SecurityScheme struct {
	Type         string `json:"type"`
	Scheme       string `json:"scheme"`
	BearerFormat string `json:"bearerFormat,omitempty"`
}

// Schema is a JSON Schema subset — enough for the flat/nested DTOs in
// internal/apidoc (string/int/bool/array/object/map, $ref, oneOf, nullable).
type Schema struct {
	Ref                  string             `json:"$ref,omitempty"`
	Type                 string             `json:"type,omitempty"`
	Format               string             `json:"format,omitempty"`
	Nullable             bool               `json:"nullable,omitempty"`
	Properties           map[string]*Schema `json:"properties,omitempty"`
	Required             []string           `json:"required,omitempty"`
	Items                *Schema            `json:"items,omitempty"`
	AdditionalProperties *Schema            `json:"additionalProperties,omitempty"`
	OneOf                []*Schema          `json:"oneOf,omitempty"`
	Description          string             `json:"description,omitempty"`
}
