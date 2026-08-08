package main

import (
	"reflect"
	"strings"
	"time"
)

// schemaBuilder converts Go types (from internal/apidoc) into OpenAPI
// component schemas via reflection, registering named struct types once
// into components.schemas and referencing them by $ref everywhere else —
// the same dedup an OpenAPI spec is expected to have.
type schemaBuilder struct {
	components map[string]*Schema
}

func newSchemaBuilder() *schemaBuilder {
	return &schemaBuilder{components: map[string]*Schema{}}
}

var timeType = reflect.TypeOf(time.Time{})

// schemaFor returns a $ref schema for named struct types (registering the
// real schema into components as a side effect) and an inline schema for
// everything else (primitives, slices, maps).
func (b *schemaBuilder) schemaFor(t reflect.Type) *Schema {
	if t.Kind() == reflect.Pointer {
		inner := b.schemaFor(t.Elem())
		inner.Nullable = true
		return inner
	}

	if t == timeType {
		return &Schema{Type: "string", Format: "date-time"}
	}

	switch t.Kind() {
	case reflect.String:
		return &Schema{Type: "string"}
	case reflect.Bool:
		return &Schema{Type: "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return &Schema{Type: "integer"}
	case reflect.Float32, reflect.Float64:
		return &Schema{Type: "number"}
	case reflect.Slice, reflect.Array:
		return &Schema{Type: "array", Items: b.schemaFor(t.Elem())}
	case reflect.Map:
		return &Schema{Type: "object", AdditionalProperties: b.schemaFor(t.Elem())}
	case reflect.Interface:
		// any / interface{} — used for the DLQ payload's arbitrary JSON blob.
		// No "type" keyword at all (not "object") — an empty schema is JSON
		// Schema's own way of saying "unconstrained", and openapi-typescript
		// maps that to `unknown`. Typing it "object" instead collapses to
		// Record<string, never> once nested under a map's additionalProperties
		// (an object schema with no declared properties reads as "no
		// properties allowed", not "any properties, unknown shape").
		return &Schema{}
	case reflect.Struct:
		return b.refFor(t)
	default:
		return &Schema{Type: "object"}
	}
}

// refFor registers a named struct's schema (once) and returns a $ref to it.
// Anonymous structs (OrgUsage's inline "storage" field) get inlined instead
// — they have no name to register under.
func (b *schemaBuilder) refFor(t reflect.Type) *Schema {
	if t.Name() == "" {
		return b.structSchema(t)
	}
	name := t.Name()
	if _, ok := b.components[name]; !ok {
		b.components[name] = &Schema{} // placeholder breaks recursive-type infinite loops
		b.components[name] = b.structSchema(t)
	}
	return &Schema{Ref: "#/components/schemas/" + name}
}

func (b *schemaBuilder) structSchema(t reflect.Type) *Schema {
	props := map[string]*Schema{}
	var required []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if name == "" {
			name = f.Name
		}
		props[name] = b.schemaFor(f.Type)
		if !strings.Contains(opts, "omitempty") && f.Type.Kind() != reflect.Pointer {
			required = append(required, name)
		}
	}
	return &Schema{Type: "object", Properties: props, Required: required}
}

// refOf is the exported entry point routeSchemas uses: pass a zero value of
// a DTO type (or a pointer to one, for request bodies where absence is
// meaningful), get back a $ref schema with the real schema registered.
func (b *schemaBuilder) refOf(v any) *Schema {
	return b.schemaFor(reflect.TypeOf(v))
}

// oneOfRefs builds a oneOf schema across several DTOs — used for the two
// routes with a genuinely discriminated-union response (login, share
// resolve).
func (b *schemaBuilder) oneOfRefs(values ...any) *Schema {
	refs := make([]*Schema, len(values))
	for i, v := range values {
		refs[i] = b.refOf(v)
	}
	return &Schema{OneOf: refs}
}
