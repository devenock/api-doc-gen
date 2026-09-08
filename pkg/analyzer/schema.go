package analyzer

import (
	"go/ast"
	"go/token"
	"reflect"
	"strings"
	"unicode"

	"github.com/devenock/api-doc-gen/pkg/models"
)

// This file builds models.Schema from Go struct declarations: parsing struct
// tags, resolving embedded fields, and mapping Go types (including locally
// defined ones) to OpenAPI schema shapes. Framework-agnostic — used for both
// request and response bodies regardless of which router package is in use.

// collectTypesInFile parses a Go file and adds struct type definitions to the type registry.
func (a *Analyzer) collectTypesInFile(filePath string) error {
	fset := token.NewFileSet()
	node, err := a.rootParseFile(fset, filePath, 0)
	if err != nil {
		a.recordParseFailure(filePath, err)
		return nil
	}
	for _, decl := range node.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		if genDecl.Tok == token.CONST || genDecl.Tok == token.VAR {
			a.collectStringConsts(genDecl)
			continue
		}
		if genDecl.Tok != token.TYPE {
			continue
		}
		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			structType, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				continue
			}
			name := typeSpec.Name.Name
			schema := a.buildSchemaFromStruct(structType)
			if schema.Type != "" || len(schema.Properties) > 0 {
				a.typeRegistry[name] = schema
				if a.typePackageName == nil {
					a.typePackageName = make(map[string]string)
				}
				a.typePackageName[name] = node.Name.Name
			}
		}
	}
	return nil
}

// buildSchemaFromStruct converts an ast.StructType to a models.Schema (object with properties).
func (a *Analyzer) buildSchemaFromStruct(st *ast.StructType) models.Schema {
	if st.Fields == nil {
		return models.Schema{Type: "object"}
	}
	props := make(map[string]models.Schema)
	var required []string
	var embeds []string
	for _, f := range st.Fields.List {
		if len(f.Names) == 0 {
			// Anonymous (embedded) field, e.g. `gorm.Model` or a local `BaseModel`.
			// With an explicit json tag it behaves like a normal nested property;
			// without one, encoding/json promotes its fields to the top level, so
			// record it for the field-promotion post-pass (resolveEmbeddedFields)
			// that runs once every type in the project has been collected —
			// the embedded type may be defined in a file not parsed yet.
			embeddedName := embeddedTypeName(f.Type)
			if embeddedName == "" {
				continue // qualified (cross-package) embed, e.g. gorm.Model — can't resolve locally
			}
			tags := parseFieldTags(f.Tag)
			if tags.skip {
				continue
			}
			if tags.name != "" {
				props[tags.name] = a.goTypeToSchema(f.Type)
			} else {
				embeds = append(embeds, embeddedName)
			}
			continue
		}
		// encoding/json never marshals unexported fields — a schema property
		// for one could never appear in a real request/response payload.
		if !f.Names[0].IsExported() {
			continue
		}
		tags := parseFieldTags(f.Tag)
		if tags.skip {
			// json:"-" — the idiomatic tag for secret-bearing fields
			// (password hashes, API tokens, internal IDs). Must not appear
			// in the generated public spec at all.
			continue
		}
		fieldName := f.Names[0].Name
		if tags.name != "" {
			fieldName = tags.name
		}
		fieldSchema := a.goTypeToSchema(f.Type)
		// A pointer field's absence of Go's zero value is nullability, not a
		// signal about whether the field is required — those are independent
		// (see the `required` derivation below). $ref siblings are forbidden
		// in OpenAPI 3.0, so a pointer-to-struct field can't carry `nullable`.
		if isPointerType(f.Type) && fieldSchema.Ref == "" {
			fieldSchema.Nullable = true
		}
		props[fieldName] = fieldSchema
		// required is a Go-side validation concern, not something JSON typing
		// implies: a plain non-pointer `int` with no validation tag is still
		// entirely optional on the wire (it just defaults to zero if absent).
		// Derive it instead from the tags Go handlers actually validate
		// against, and let omitempty stand as an explicit "not required" —
		// unless RequiredByDefault is set, in which case every field is
		// required unless omitempty says otherwise.
		if (tags.required || a.config.RequiredByDefault) && !tags.omitempty {
			required = append(required, fieldName)
		}
	}
	return models.Schema{
		Type:       "object",
		Properties: props,
		Required:   required,
		Embeds:     embeds,
	}
}

// fieldTags carries the parsed json/binding/validate tag information for a
// single struct field: the OpenAPI property name (name), whether the field
// is excluded from the schema entirely (skip, from json:"-"), whether it
// carries an explicit not-required signal (omitempty), and whether a
// binding/validate tag marks it required.
type fieldTags struct {
	name      string
	skip      bool
	omitempty bool
	required  bool
}

// parseFieldTags extracts json/binding/validate semantics from a struct
// field's tag using reflect.StructTag so quoting/escaping matches Go's own
// tag parsing exactly, rather than a hand-rolled space-split.
func parseFieldTags(tag *ast.BasicLit) fieldTags {
	var ft fieldTags
	if tag == nil {
		return ft
	}
	st := reflect.StructTag(strings.Trim(tag.Value, "`"))

	if jsonTag, ok := st.Lookup("json"); ok {
		parts := strings.Split(jsonTag, ",")
		name := parts[0]
		// `json:"-"` (exactly, no trailing comma) excludes the field.
		// `json:"-,"` is encoding/json's escape for a field literally named
		// "-" that should still be marshaled — parts would be ["-", ""] here,
		// which correctly falls through to the name-assignment below instead.
		if name == "-" && len(parts) == 1 {
			ft.skip = true
			return ft
		}
		ft.name = name
		for _, opt := range parts[1:] {
			if opt == "omitempty" {
				ft.omitempty = true
			}
		}
	}
	if v, ok := st.Lookup("binding"); ok && tagOptionPresent(v, "required") {
		ft.required = true
	}
	if v, ok := st.Lookup("validate"); ok && tagOptionPresent(v, "required") {
		ft.required = true
	}
	return ft
}

// tagOptionPresent reports whether opt appears as one of the comma-separated
// options in a binding/validate tag value (e.g. "required,min=1,max=100").
func tagOptionPresent(tagValue, opt string) bool {
	for _, part := range strings.Split(tagValue, ",") {
		if strings.TrimSpace(part) == opt {
			return true
		}
	}
	return false
}

// embeddedTypeName returns the local type name for an embedded field's type
// expression (`Foo` or `*Foo`), or "" for anything not locally resolvable
// (qualified selectors like `gorm.Model` live in another package/module).
func embeddedTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name
		}
	}
	return ""
}

// resolveEmbeddedFields flattens anonymous (embedded) struct fields recorded
// during collectTypesInFile into their parent schema's Properties, mirroring
// Go's field-promotion behavior for JSON marshaling. Runs after every file in
// the project has been scanned so embedding a type defined in another file
// resolves correctly regardless of file processing order.
func (a *Analyzer) resolveEmbeddedFields() {
	resolved := make(map[string]bool)
	var flatten func(name string) models.Schema
	flatten = func(name string) models.Schema {
		s, ok := a.typeRegistry[name]
		if !ok || resolved[name] || len(s.Embeds) == 0 {
			return s
		}
		resolved[name] = true // guard against embedding cycles
		if s.Properties == nil {
			s.Properties = make(map[string]models.Schema)
		}
		for _, embedded := range s.Embeds {
			parent := flatten(embedded)
			for propName, propSchema := range parent.Properties {
				if _, exists := s.Properties[propName]; !exists {
					s.Properties[propName] = propSchema
				}
			}
			s.Required = append(s.Required, parent.Required...)
		}
		a.typeRegistry[name] = s
		return s
	}
	for name := range a.typeRegistry {
		flatten(name)
	}
}

// externalTypeSchemas maps "package.Type", as written at the selector
// expression (e.g. "time.Time", "sql.NullString"), to the schema it should
// produce. Every entry here was checked against encoding/json's actual
// output before being added, not assumed from the Go-level shape — that
// distinction matters: sql.NullString looks like it should marshal as a
// plain string, but it has no custom MarshalJSON, so it actually marshals
// as its literal struct fields ({"String":...,"Valid":...}). Getting this
// wrong would be worse than the generic {"type":"object"} fallback below,
// since it would confidently show the wrong shape instead of an honestly
// incomplete one.
//
// Matched purely by identifier name (this package has no go/types import
// resolution), so it has the same known limitation the pre-existing
// time.Time case already had: an import alias or a same-named local type
// would be matched too. Accepted for the same reason it already was.
var externalTypeSchemas = map[string]models.Schema{
	"time.Time": {Type: "string", Format: "date-time"},
	// Duration has no custom MarshalJSON; it's `type Duration int64`
	// marshaling as a plain nanosecond count, not a formatted string.
	"time.Duration": {Type: "integer", Format: "int64"},
	// github.com/google/uuid: implements encoding.TextMarshaler, so
	// encoding/json renders it as the canonical hyphenated string form.
	"uuid.UUID": {Type: "string", Format: "uuid"},

	// database/sql's Null* types have no custom MarshalJSON either, so each
	// marshals as its two literal fields, not the plain underlying value.
	"sql.NullString": {Type: "object", Properties: map[string]models.Schema{
		"String": {Type: "string"}, "Valid": {Type: "boolean"},
	}},
	"sql.NullBool": {Type: "object", Properties: map[string]models.Schema{
		"Bool": {Type: "boolean"}, "Valid": {Type: "boolean"},
	}},
	"sql.NullFloat64": {Type: "object", Properties: map[string]models.Schema{
		"Float64": {Type: "number"}, "Valid": {Type: "boolean"},
	}},
	"sql.NullInt64": {Type: "object", Properties: map[string]models.Schema{
		"Int64": {Type: "integer", Format: "int64"}, "Valid": {Type: "boolean"},
	}},
	"sql.NullInt32": {Type: "object", Properties: map[string]models.Schema{
		"Int32": {Type: "integer", Format: "int32"}, "Valid": {Type: "boolean"},
	}},
	"sql.NullInt16": {Type: "object", Properties: map[string]models.Schema{
		"Int16": {Type: "integer"}, "Valid": {Type: "boolean"},
	}},
	"sql.NullByte": {Type: "object", Properties: map[string]models.Schema{
		"Byte": {Type: "integer"}, "Valid": {Type: "boolean"},
	}},
	"sql.NullTime": {Type: "object", Properties: map[string]models.Schema{
		"Time": {Type: "string", Format: "date-time"}, "Valid": {Type: "boolean"},
	}},
}

// goTypeToSchema maps a Go ast.Expr type to an OpenAPI-style Schema.
func (a *Analyzer) goTypeToSchema(expr ast.Expr) models.Schema {
	switch t := expr.(type) {
	case *ast.Ident:
		return a.identToSchema(t.Name)
	case *ast.StarExpr:
		return a.goTypeToSchema(t.X)
	case *ast.ArrayType:
		item := a.goTypeToSchema(t.Elt)
		return models.Schema{Type: "array", Items: &item}
	case *ast.MapType:
		return models.Schema{Type: "object", AdditionalProperties: map[string]interface{}{}}
	case *ast.SelectorExpr:
		// e.g. time.Time — a type from outside the scanned project, which we
		// have no way to inspect the fields of (no go/types, and it isn't
		// necessarily even downloaded). externalTypeSchemas special-cases
		// the handful of common ones with a well-established JSON shape;
		// anything else falls back to a bare object below.
		if ident, ok := t.X.(*ast.Ident); ok {
			if schema, ok := externalTypeSchemas[ident.Name+"."+t.Sel.Name]; ok {
				return schema
			}
		}
		return models.Schema{Type: "object"}
	case *ast.InterfaceType:
		return models.Schema{Type: "object"}
	default:
		return models.Schema{Type: "object"}
	}
}

// addSchemaAndRefsToModels adds the schema and any referenced types to a.models so OpenAPI components/schemas can resolve $ref.
func (a *Analyzer) addSchemaAndRefsToModels(name string, s models.Schema) {
	a.models[name] = s
	if s.Ref != "" {
		refName := strings.TrimPrefix(s.Ref, "#/components/schemas/")
		if refName != "" && refName != name {
			if nested, ok := a.typeRegistry[refName]; ok {
				a.addSchemaAndRefsToModels(refName, nested)
			}
		}
	}
	for _, prop := range s.Properties {
		if prop.Ref != "" {
			refName := strings.TrimPrefix(prop.Ref, "#/components/schemas/")
			if refName != "" {
				if nested, ok := a.typeRegistry[refName]; ok {
					a.addSchemaAndRefsToModels(refName, nested)
				}
			}
		}
	}
	if s.Items != nil {
		if s.Items.Ref != "" {
			refName := strings.TrimPrefix(s.Items.Ref, "#/components/schemas/")
			if refName != "" {
				if nested, ok := a.typeRegistry[refName]; ok {
					a.addSchemaAndRefsToModels(refName, nested)
				}
			}
		}
	}
}

func (a *Analyzer) identToSchema(name string) models.Schema {
	switch name {
	case "string":
		return models.Schema{Type: "string"}
	case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64":
		return models.Schema{Type: "integer", Format: "int64"}
	case "float32", "float64":
		return models.Schema{Type: "number", Format: "double"}
	case "bool":
		return models.Schema{Type: "boolean"}
	case "interface{}":
		return models.Schema{Type: "object"}
	default:
		// Named struct: use ref if in registry, else object
		if _, ok := a.typeRegistry[name]; ok {
			return models.Schema{Ref: "#/components/schemas/" + name}
		}
		return models.Schema{Type: "object"}
	}
}

// getHandlerRequestAndResponseTypes returns the type names for the handler's second param (request body) and first return (response).
func getHandlerRequestAndResponseTypes(file *ast.File, handlerName string) (reqTypeName, respTypeName string) {
	for _, decl := range file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Name.Name != handlerName || fd.Type.Params == nil {
			continue
		}
		params := fd.Type.Params.List
		if len(params) >= 2 {
			reqTypeName = typeExprToName(params[1].Type)
		}
		if fd.Type.Results != nil && len(fd.Type.Results.List) >= 1 {
			respTypeName = typeExprToName(fd.Type.Results.List[0].Type)
		}
		return reqTypeName, respTypeName
	}
	return "", ""
}

// localTypeName returns the unqualified type name (e.g. "pkg.CreateRequest" -> "CreateRequest").
func localTypeName(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return name[i+1:]
	}
	return name
}

func typeExprToName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return typeExprToName(t.X)
	case *ast.SelectorExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name + "." + t.Sel.Name
		}
		return ""
	default:
		return ""
	}
}

func isPointerType(expr ast.Expr) bool {
	_, ok := expr.(*ast.StarExpr)
	return ok
}

// looksLikeHandlerName returns true if s looks like a Go handler name (CamelCase, no spaces, no slash).
func looksLikeHandlerName(s string) bool {
	if s == "" || strings.Contains(s, " ") || strings.Contains(s, "/") {
		return false
	}
	// At least one lower and one upper for CamelCase, or single word
	hasUpper := false
	hasLower := false
	for _, r := range s {
		if unicode.IsUpper(r) {
			hasUpper = true
		}
		if unicode.IsLower(r) {
			hasLower = true
		}
	}
	return hasUpper && (hasLower || len(s) <= 2)
}

// humanizeHandlerName turns a handler name like "CreateProduct" into "Create product".
func humanizeHandlerName(name string) string {
	if name == "" {
		return ""
	}
	var b strings.Builder
	for i, r := range name {
		if i > 0 && unicode.IsUpper(r) {
			b.WriteByte(' ')
			b.WriteRune(unicode.ToLower(r))
		} else {
			b.WriteRune(r)
		}
	}
	s := b.String()
	if s == "" {
		return s
	}
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}
