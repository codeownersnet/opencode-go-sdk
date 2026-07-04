// Command pull-and-normalize fetches the upstream opencode OpenAPI spec and/or
// normalizes OpenAPI 3.1 constructs to 3.0-compatible JSON so oapi-codegen
// (which targets 3.0) can parse it.
//
// Usage:
//
//	go run scripts/pull-and-normalize.go                     # pull + normalize
//	go run scripts/pull-and-normalize.go --pull               # fetch only
//	go run scripts/pull-and-normalize.go --normalize          # normalize only
//
// Environment:
//
//	OPENCODE_SERVER  if set, fetch from this server instead of upstream GitHub
//
// Files:
//
//	opencode-spec.json             raw upstream spec (committed; diff shows upstream changes)
//	opencode-spec.normalized.json  normalized 3.0-compatible spec (gitignored; used by oapi-codegen)
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
)

const (
	defaultURL     = "https://raw.githubusercontent.com/anomalyco/opencode/refs/heads/dev/packages/sdk/openapi.json"
	rawFile        = "opencode-spec.json"
	normalizedFile = "opencode-spec.normalized.json"
	localPath      = "/openapi.json"
)

func main() {
	pull := flag.Bool("pull", false, "fetch the upstream spec")
	normalize := flag.Bool("normalize", false, "normalize opencode-spec.json to 3.0-compatible JSON")
	flag.Parse()

	// Default: do both.
	if !*pull && !*normalize {
		*pull = true
		*normalize = true
	}

	if *pull {
		url := defaultURL
		if v := os.Getenv("OPENCODE_SERVER"); v != "" {
			url = v + localPath
		}
		if err := fetch(url, rawFile); err != nil {
			fmt.Fprintf(os.Stderr, "pull: %v\n", err)
			os.Exit(1)
		}
	}

	if *normalize {
		if err := normalizeFile(rawFile, normalizedFile); err != nil {
			fmt.Fprintf(os.Stderr, "normalize: %v\n", err)
			os.Exit(1)
		}
	}
}

func fetch(url, out string) error {
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("get %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("get %s: status %s", url, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}

	if err := os.WriteFile(out, body, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", out, err)
	}

	fmt.Printf("Pulled %s (%d bytes) from %s\n", out, len(body), url)
	return nil
}

// normalizeFile reads a 3.1 OpenAPI JSON file, converts 3.1-only constructs to
// their 3.0 equivalents, and writes the result. The transformations are:
//
//  1. numeric `exclusiveMinimum: N` → `minimum: N` + `exclusiveMinimum: true`
//  2. numeric `exclusiveMaximum: N` → `maximum: N` + `exclusiveMaximum: true`
//  3. `type: ["foo", "null"]` → `type: "foo"` + `nullable: true`
//  4. `anyOf: [{...}, {type: "null"}]` → inline the non-null variant + `nullable: true`
//  5. GlobalEvent.payload.anyOf[] inline schemas → extracted to named schemas
//     (prevents naming collisions with the Event union's Event* schemas)
//
// oapi-codegen v2 targets OpenAPI 3.0; these constructs are the most common
// 3.1 incompatibilities. If upstream adopts more 3.1 features (const,
// prefixItems, etc.) this function should be extended.
func normalizeFile(in, out string) error {
	data, err := os.ReadFile(in)
	if err != nil {
		return fmt.Errorf("read %s: %w", in, err)
	}

	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}

	normalizeValue(doc)
	extractGlobalEventInlineSchemas(doc)
	fixDottedSchemaNames(doc)

	result, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	result = append(result, '\n')

	if err := os.WriteFile(out, result, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", out, err)
	}

	fmt.Printf("Normalized %s → %s (%d bytes)\n", in, out, len(result))
	return nil
}

// normalizeValue recursively walks a JSON value (map, slice, or scalar) and
// applies 3.1→3.0 transformations in place.
func normalizeValue(v any) {
	switch t := v.(type) {
	case map[string]any:
		normalizeObject(t)
		for _, child := range t {
			normalizeValue(child)
		}
	case []any:
		for _, child := range t {
			normalizeValue(child)
		}
	}
}

// normalizeObject applies 3.1→3.0 fixes to a single JSON object.
func normalizeObject(m map[string]any) {
	// 1 & 2: numeric exclusiveMinimum / exclusiveMaximum
	if em, ok := m["exclusiveMinimum"]; ok {
		if num, isNum := em.(float64); isNum {
			m["minimum"] = num
			m["exclusiveMinimum"] = true
		}
	}
	if em, ok := m["exclusiveMaximum"]; ok {
		if num, isNum := em.(float64); isNum {
			m["maximum"] = num
			m["exclusiveMaximum"] = true
		}
	}

	// 3: type as array (e.g. ["string", "null"])
	if types, ok := m["type"].([]any); ok && len(types) > 0 {
		var nonNull []any
		nullable := false
		for _, tp := range types {
			if s, ok := tp.(string); ok && s == "null" {
				nullable = true
			} else {
				nonNull = append(nonNull, tp)
			}
		}
		if len(nonNull) == 1 {
			m["type"] = nonNull[0]
		} else if len(nonNull) > 1 {
			m["type"] = nonNull // 3.0 doesn't support multi-type, but keep for debugging
		} else {
			// Only null was in the array; remove type, set nullable.
			delete(m, "type")
		}
		if nullable {
			m["nullable"] = true
		}
	}

	// 4: anyOf/oneOf with {type: "null"} variants — the 3.1 nullable pattern.
	//   anyOf: [{type: "string"}, {type: "null"}] → type: "string" + nullable: true
	//   anyOf: [{$ref: "..."}, {type: "null"}]     → $ref: "..." + nullable: true
	//   anyOf: [{}, {type: "null"}]                → nullable: true (any type)
	for _, key := range []string{"anyOf", "oneOf"} {
		arr, ok := m[key].([]any)
		if !ok || len(arr) == 0 {
			continue
		}
		var nonNull []any
		hadNull := false
		for _, elem := range arr {
			if schema, ok := elem.(map[string]any); ok && isNullSchema(schema) {
				hadNull = true
				continue
			}
			nonNull = append(nonNull, elem)
		}
		if !hadNull {
			continue
		}
		switch len(nonNull) {
		case 0:
			// Only null was in the array; remove the union, mark nullable.
			delete(m, key)
			m["nullable"] = true
		case 1:
			// Single non-null variant: inline its keys, drop the union.
			if schema, ok := nonNull[0].(map[string]any); ok {
				for k, v := range schema {
					m[k] = v
				}
			}
			delete(m, key)
			m["nullable"] = true
		default:
			// Multiple non-null variants: keep the union without the null element.
			m[key] = nonNull
			m["nullable"] = true
		}
	}
}

// isNullSchema returns true if the schema is {type: "null"}.
func isNullSchema(m map[string]any) bool {
	t, ok := m["type"]
	if !ok {
		return false
	}
	s, ok := t.(string)
	return ok && s == "null"
}

// extractGlobalEventInlineSchemas pulls inline schemas out of
// GlobalEvent.payload.anyOf[] and registers them as named schemas in
// components/schemas, replacing the inline entries with $ref pointers.
//
// This prevents oapi-codegen from auto-naming the inline variants in a way
// that collides with the Event union's named Event* schemas. The extracted
// schemas are named GlobalEvent<PascalCase(enum value)>.
func extractGlobalEventInlineSchemas(doc map[string]any) {
	schemas, ok := doc["components"].(map[string]any)
	if !ok {
		return
	}
	compSchemas, ok := schemas["schemas"].(map[string]any)
	if !ok {
		return
	}
	globalEvent, ok := compSchemas["GlobalEvent"].(map[string]any)
	if !ok {
		return
	}
	props, ok := globalEvent["properties"].(map[string]any)
	if !ok {
		return
	}
	payload, ok := props["payload"].(map[string]any)
	if !ok {
		return
	}
	anyOf, ok := payload["anyOf"].([]any)
	if !ok {
		return
	}

	for i, variant := range anyOf {
		schema, ok := variant.(map[string]any)
		if !ok {
			continue
		}
		// Skip $ref variants — they're already named.
		if _, hasRef := schema["$ref"]; hasRef {
			continue
		}
		// Extract the type enum to build a name.
		typeEnum := extractTypeEnum(schema)
		if typeEnum == "" {
			continue
		}
		name := "GlobalEvent" + pascalCase(typeEnum)
		// If the name already exists (shouldn't, but be safe), append an index.
		if _, exists := compSchemas[name]; exists {
			name = fmt.Sprintf("GlobalEvent%s%d", pascalCase(typeEnum), i)
		}
		compSchemas[name] = schema
		anyOf[i] = map[string]any{"$ref": "#/components/schemas/" + name}
	}
}

// extractTypeEnum reads the `type.enum[0]` value from a schema's properties.
func extractTypeEnum(schema map[string]any) string {
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		return ""
	}
	typeField, ok := props["type"].(map[string]any)
	if !ok {
		return ""
	}
	enum, ok := typeField["enum"].([]any)
	if !ok || len(enum) == 0 {
		return ""
	}
	s, ok := enum[0].(string)
	if !ok {
		return ""
	}
	return s
}

// pascalCase converts a dotted/hyphenated event type string to PascalCase.
// Examples:
//
//	"tui.command.execute" → "TuiCommandExecute"
//	"models-dev.refreshed" → "ModelsDevRefreshed"
//	"permission.v2.asked"  → "PermissionV2Asked"
func pascalCase(s string) string {
	var result []byte
	capitalize := true
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '.' || c == '-' || c == '_' {
			capitalize = true
			continue
		}
		if capitalize {
			if c >= 'a' && c <= 'z' {
				c -= 'a' - 'A'
			}
			capitalize = false
		}
		result = append(result, c)
	}
	return string(result)
}

// fixDottedSchemaNames resolves Go naming collisions caused by OpenAPI schema
// names that contain dots. oapi-codegen converts both `Event.tui.command.execute`
// and `EventTuiCommandExecute` to the same Go type name `EventTuiCommandExecute`.
//
// For each dotted schema name, if its PascalCase form collides with an existing
// non-dotted schema, we rename the schema key to PascalCase+"Durable" and update
// all $ref pointers. Renaming the key (rather than just x-go-name) ensures the
// inner generated types (e.g. EventTuiCommandExecute_Properties_Command) also
// get the unique prefix.
func fixDottedSchemaNames(doc map[string]any) {
	schemas, ok := doc["components"].(map[string]any)
	if !ok {
		return
	}
	compSchemas, ok := schemas["schemas"].(map[string]any)
	if !ok {
		return
	}

	// Collect all non-dotted schema names to check for collisions.
	nonDotted := make(map[string]bool)
	for name := range compSchemas {
		if !containsDot(name) {
			nonDotted[name] = true
		}
	}

	// Find dotted schemas that collide and build a rename map.
	rename := make(map[string]string) // old name → new name
	for name := range compSchemas {
		if !containsDot(name) {
			continue
		}
		pascal := pascalCase(name)
		if !nonDotted[pascal] {
			continue // No collision; skip.
		}
		newName := pascal + "Durable"
		rename[name] = newName
	}

	if len(rename) == 0 {
		return
	}

	// Rename schema keys.
	for oldName, newName := range rename {
		compSchemas[newName] = compSchemas[oldName]
		delete(compSchemas, oldName)
	}

	// Update all $ref pointers throughout the document.
	updateRefs(doc, rename)
}

// updateRefs recursively walks the document and updates $ref values that match
// the rename map (old → new).
func updateRefs(v any, rename map[string]string) {
	switch t := v.(type) {
	case map[string]any:
		if ref, ok := t["$ref"].(string); ok {
			for old, new := range rename {
				if ref == "#/components/schemas/"+old {
					t["$ref"] = "#/components/schemas/" + new
					break
				}
			}
		}
		for _, child := range t {
			updateRefs(child, rename)
		}
	case []any:
		for _, child := range t {
			updateRefs(child, rename)
		}
	}
}

// containsDot returns true if the string contains a '.'.
func containsDot(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			return true
		}
	}
	return false
}
