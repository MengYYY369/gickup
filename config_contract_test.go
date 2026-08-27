package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/cooperspencer/gickup/types"
	"github.com/goccy/go-yaml"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestConfigContract(t *testing.T) {
	schemaBytes, err := os.ReadFile("gickup_spec.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]interface{}
	if err := json.Unmarshal(schemaBytes, &schema); err != nil {
		t.Fatal(err)
	}

	checkStructSchema(t, reflect.TypeOf(types.Conf{}), schema, schema)
}

func TestConfigContractRecursesThroughPointersMapsAliasesAndEmbeddedFields(t *testing.T) {
	type contractAlias string
	type embeddedContract struct {
		Enabled bool `yaml:"enabled"`
	}
	type nestedContract struct {
		Name string `yaml:"name"`
	}
	type recursiveContract struct {
		embeddedContract `yaml:",inline"`
		Pointer          *nestedContract            `yaml:"pointer"`
		Mapped           map[string]*nestedContract `yaml:"mapped"`
		Alias            contractAlias              `yaml:"alias"`
	}

	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"enabled": map[string]interface{}{"type": "boolean"},
			"pointer": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{"type": "string"},
				},
			},
			"mapped": map[string]interface{}{
				"type": "object",
				"additionalProperties": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"name": map[string]interface{}{"type": "string"},
					},
				},
			},
			"alias": map[string]interface{}{"type": "string"},
		},
	}

	checkStructSchema(t, reflect.TypeOf(recursiveContract{}), schema, schema)
}

func TestConfigContractRejectsNestedGoFieldMissingFromSchema(t *testing.T) {
	if os.Getenv("GICKUP_CONTRACT_MISSING_NESTED_SCHEMA") == "1" {
		type nestedContract struct {
			Existing string `yaml:"existing"`
			Added    string `yaml:"added"`
		}
		type rootContract struct {
			Nested *nestedContract `yaml:"nested"`
		}
		schema := map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"nested": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"existing": map[string]interface{}{"type": "string"},
					},
				},
			},
		}
		checkStructSchema(t, reflect.TypeOf(rootContract{}), schema, schema)
		return
	}

	command := exec.Command(os.Args[0], "-test.run=^TestConfigContractRejectsNestedGoFieldMissingFromSchema$")
	command.Env = append(os.Environ(), "GICKUP_CONTRACT_MISSING_NESTED_SCHEMA=1")
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("contract check accepted a nested Go YAML field missing from schema:\n%s", output)
	}
}

func TestConfigContractRejectsNestedSchemaPropertyMissingFromGoModel(t *testing.T) {
	if os.Getenv("GICKUP_CONTRACT_MISSING_NESTED_MODEL") == "1" {
		type nestedContract struct {
			Existing string `yaml:"existing"`
		}
		type rootContract struct {
			Nested map[string]*nestedContract `yaml:"nested"`
		}
		schema := map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"nested": map[string]interface{}{
					"type": "object",
					"additionalProperties": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"existing": map[string]interface{}{"type": "string"},
							"added":    map[string]interface{}{"type": "string"},
						},
					},
				},
			},
		}
		checkStructSchema(t, reflect.TypeOf(rootContract{}), schema, schema)
		return
	}

	command := exec.Command(os.Args[0], "-test.run=^TestConfigContractRejectsNestedSchemaPropertyMissingFromGoModel$")
	command.Env = append(os.Environ(), "GICKUP_CONTRACT_MISSING_NESTED_MODEL=1")
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("contract check accepted a nested schema property missing from Go model:\n%s", output)
	}
}

func TestProviderSpecificSchemaSubsetRejectsUnauditedModelTypes(t *testing.T) {
	type GenRepo struct {
		Existing string `yaml:"existing"`
		Added    string `yaml:"added"`
	}

	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"existing": map[string]interface{}{"type": "string"},
		},
	}

	if isProviderSpecificSchemaSubset(reflect.TypeOf(GenRepo{}), schema) {
		t.Fatal("an unrelated model with an audited type name was allowed to use a provider-specific trimmed schema")
	}
}

func TestExampleConfigMatchesSchemaProperties(t *testing.T) {
	data, err := webUIAssets.ReadFile("conf.example.yml")
	if err != nil {
		t.Fatal(err)
	}
	documents, err := decodeExampleDocuments(data)
	if err != nil {
		t.Fatalf("example config is invalid YAML: %v", err)
	}
	if len(documents) == 0 {
		t.Fatal("example config contains no YAML documents")
	}

	schema := loadContractSchema(t)
	for _, document := range documents {
		checkExampleSchemaProperties(t, document, schema, schema, "document")
	}
}

func TestEmbeddedExampleDocumentsValidateAgainstJSONSchema(t *testing.T) {
	schemaData, err := webUIAssets.ReadFile("gickup_spec.json")
	if err != nil {
		t.Fatal(err)
	}
	var schemaDocument interface{}
	if err := json.Unmarshal(schemaData, &schemaDocument); err != nil {
		t.Fatalf("decode embedded JSON Schema: %v", err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("gickup_spec.json", schemaDocument); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile("gickup_spec.json")
	if err != nil {
		t.Fatalf("compile embedded JSON Schema: %v", err)
	}

	exampleData, err := webUIAssets.ReadFile("conf.example.yml")
	if err != nil {
		t.Fatal(err)
	}
	documents, err := decodeExampleDocuments(exampleData)
	if err != nil {
		t.Fatal(err)
	}
	for index, document := range documents {
		if err := schema.Validate(document); err != nil {
			t.Errorf("embedded example document %d violates JSON Schema: %v", index, err)
		}
	}
}

func decodeExampleDocuments(data []byte) ([]map[string]interface{}, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var documents []map[string]interface{}
	for {
		var document map[string]interface{}
		err := decoder.Decode(&document)
		if err == io.EOF {
			return documents, nil
		}
		if err != nil {
			return nil, err
		}
		if len(document) != 0 {
			documents = append(documents, document)
		}
	}
}

func TestDecodeExampleDocumentsHandlesCRLFDocumentSeparators(t *testing.T) {
	data := []byte("cron: '@daily'\r\n---\r\ncron: '@hourly'\r\n")
	documents, err := decodeExampleDocuments(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(documents) != 2 {
		t.Fatalf("decoded %d documents, want 2", len(documents))
	}
	if documents[0]["cron"] != "@daily" || documents[1]["cron"] != "@hourly" {
		t.Fatalf("decoded documents = %#v", documents)
	}
}

func TestEmbeddedConfigContractMatchesWorkingFiles(t *testing.T) {
	for _, name := range []string{"gickup_spec.json", "conf.example.yml"} {
		embedded, err := webUIAssets.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		working, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(embedded, working) {
			t.Errorf("embedded %s differs from working file", name)
		}
	}
}

func TestConfigEditorPreservesUnknownYAML(t *testing.T) {
	raw := []byte("source:\n  future_hoster:\n    custom: keep-me\ndestination:\n  local:\n    - path: backup\n")
	edited, err := webUIEditYAML(raw, []webUIEditOperation{{
		Op:       "delete-field",
		Document: 0,
		Path:     "destination.local.0.path",
	}})
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]interface{}
	if err := yaml.Unmarshal(edited, &value); err != nil {
		t.Fatal(err)
	}
	if got := value["source"].(map[string]interface{})["future_hoster"].(map[string]interface{})["custom"]; got != "keep-me" {
		t.Fatalf("unknown YAML value was not preserved by editor: got %v", got)
	}
}

func checkExampleSchemaProperties(t *testing.T, value interface{}, node, root map[string]interface{}, path string) {
	t.Helper()
	node = resolveSchemaRef(t, node, root)
	want, ok := node["type"].(string)
	if !ok || want == "" {
		t.Errorf("%s: schema type is missing", path)
	} else if got := exampleJSONType(value); got != want {
		t.Errorf("%s: example YAML type %q, schema type %q", path, got, want)
	}
	if enum, ok := node["enum"].([]interface{}); ok {
		matched := false
		for _, candidate := range enum {
			if reflect.DeepEqual(value, candidate) {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("%s: example YAML value %v is not in schema enum %v", path, value, enum)
		}
	}
	switch typed := value.(type) {
	case map[string]interface{}:
		properties, _ := node["properties"].(map[string]interface{})
		for name, child := range typed {
			property, ok := properties[name].(map[string]interface{})
			if !ok {
				t.Errorf("%s.%s: example YAML property is missing from schema", path, name)
				continue
			}
			checkExampleSchemaProperties(t, child, property, root, path+"."+name)
		}
	case []interface{}:
		items, ok := node["items"].(map[string]interface{})
		if !ok {
			return
		}
		for index, child := range typed {
			checkExampleSchemaProperties(t, child, items, root, fmt.Sprintf("%s.%d", path, index))
		}
	}
}

func exampleJSONType(value interface{}) string {
	switch value.(type) {
	case map[string]interface{}:
		return "object"
	case []interface{}:
		return "array"
	case bool:
		return "boolean"
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return "integer"
	case float32, float64:
		return "number"
	case nil:
		return "null"
	default:
		return "string"
	}
}

func TestConfigContractCoversRequiredEnumsAndDefaults(t *testing.T) {
	schema := loadContractSchema(t)

	checkSchemaContracts(t, schema, schema, "#")
	checkModelDefaults(t, reflect.TypeOf(types.Conf{}), schema, schema, "Conf")
	for _, contractError := range validateModelContractAudit(modelContractAudit, schema) {
		t.Error(contractError)
	}
}

func TestModelContractAuditRejectsMissingDeclaration(t *testing.T) {
	audit := cloneModelContractAudit(modelContractAudit)
	delete(audit.Required, "#/properties/destination/properties/local/items")
	errors := validateModelContractAudit(audit, loadContractSchema(t))
	if !slices.ContainsFunc(errors, func(err string) bool {
		return strings.Contains(err, "missing required declaration")
	}) {
		t.Fatalf("missing declaration errors = %#v, want missing required declaration", errors)
	}
}

func TestModelContractAuditRejectsUnexpectedDeclaration(t *testing.T) {
	audit := cloneModelContractAudit(modelContractAudit)
	audit.Required["#/properties/destination/properties/local/items"] = append(
		audit.Required["#/properties/destination/properties/local/items"],
		"not_a_model_field",
	)
	errors := validateModelContractAudit(audit, loadContractSchema(t))
	if !slices.ContainsFunc(errors, func(err string) bool {
		return strings.Contains(err, "unexpected required declaration")
	}) {
		t.Fatalf("unexpected declaration errors = %#v, want unexpected required declaration", errors)
	}
}

func TestModelContractAuditRejectsEnumAndDefaultValueDrift(t *testing.T) {
	audit := cloneModelContractAudit(modelContractAudit)
	for path, values := range audit.Enums {
		audit.Enums[path] = append([]interface{}{"drifted-value"}, values...)
		break
	}
	for path := range audit.Defaults {
		audit.Defaults[path] = "drifted-value"
		break
	}
	errors := validateModelContractAudit(audit, loadContractSchema(t))
	if !slices.ContainsFunc(errors, func(err string) bool {
		return strings.Contains(err, "enum value drift")
	}) {
		t.Fatalf("value drift errors = %#v, want enum value drift", errors)
	}
	if !slices.ContainsFunc(errors, func(err string) bool {
		return strings.Contains(err, "default value drift")
	}) {
		t.Fatalf("value drift errors = %#v, want default value drift", errors)
	}
}

type modelContractAuditMetadata struct {
	Required map[string][]string
	Enums    map[string][]interface{}
	Defaults map[string]interface{}
}

var modelContractAudit = modelContractAuditMetadata{
	Required: map[string][]string{
		"#/properties/source/properties/any/items":        {"url"},
		"#/properties/destination/properties/local/items": {"path"},
	},
	Enums: map[string][]interface{}{
		"#/definitions/visibility/properties/repositories":                        {"public", "private"},
		"#/definitions/visibility/properties/organizations":                       {"public", "private", "limited"},
		"#/properties/destination/properties/radicle/items/properties/visibility": {"public", "private", "source"},
	},
	Defaults: map[string]interface{}{
		"#/properties/destination/properties/local/items/properties/mirror":        false,
		"#/properties/destination/properties/s3/items/properties/use_static_creds": true,
	},
}

func cloneModelContractAudit(source modelContractAuditMetadata) modelContractAuditMetadata {
	clone := modelContractAuditMetadata{
		Required: make(map[string][]string, len(source.Required)),
		Enums:    make(map[string][]interface{}, len(source.Enums)),
		Defaults: make(map[string]interface{}, len(source.Defaults)),
	}
	for path, fields := range source.Required {
		clone.Required[path] = append([]string(nil), fields...)
	}
	for path, values := range source.Enums {
		clone.Enums[path] = append([]interface{}(nil), values...)
	}
	for path, value := range source.Defaults {
		clone.Defaults[path] = value
	}
	return clone
}

func validateModelContractAudit(audit modelContractAuditMetadata, schema map[string]interface{}) []string {
	var errors []string
	actualRequired := map[string][]string{}
	actualEnums := map[string][]interface{}{}
	actualDefaults := map[string]interface{}{}
	collectModelContractValues(schema, "#", actualRequired, actualEnums, actualDefaults)

	for path, actual := range actualRequired {
		expected, exists := audit.Required[path]
		if !exists {
			errors = append(errors, fmt.Sprintf("%s: missing required declaration", path))
			continue
		}
		if !reflect.DeepEqual(actual, expected) {
			errors = append(errors, fmt.Sprintf("%s: required value drift: schema=%#v audit=%#v", path, actual, expected))
		}
	}
	for path := range audit.Required {
		if _, exists := actualRequired[path]; !exists {
			errors = append(errors, fmt.Sprintf("%s: unexpected required declaration", path))
		}
	}
	for path, expected := range audit.Required {
		properties, _ := schemaNodeFromPath(schema, path)["properties"].(map[string]interface{})
		for _, field := range expected {
			if _, exists := properties[field]; !exists {
				errors = append(errors, fmt.Sprintf("%s: unexpected required declaration %q", path, field))
			}
		}
	}

	for path, actual := range actualEnums {
		expected, exists := audit.Enums[path]
		if !exists {
			errors = append(errors, fmt.Sprintf("%s: missing enum declaration", path))
		} else if !reflect.DeepEqual(actual, expected) {
			errors = append(errors, fmt.Sprintf("%s: enum value drift", path))
		}
	}
	for path := range audit.Enums {
		if _, exists := actualEnums[path]; !exists {
			errors = append(errors, fmt.Sprintf("%s: unexpected enum declaration", path))
		}
	}

	for path, actual := range actualDefaults {
		expected, exists := audit.Defaults[path]
		if !exists {
			errors = append(errors, fmt.Sprintf("%s: missing default declaration", path))
		} else if !reflect.DeepEqual(actual, expected) {
			errors = append(errors, fmt.Sprintf("%s: default value drift", path))
		}
	}
	for path := range audit.Defaults {
		if _, exists := actualDefaults[path]; !exists {
			errors = append(errors, fmt.Sprintf("%s: unexpected default declaration", path))
		}
	}
	return errors
}

func collectModelContractValues(node map[string]interface{}, path string, required map[string][]string, enums map[string][]interface{}, defaults map[string]interface{}) {
	if values, ok := node["required"].([]interface{}); ok {
		fields := make([]string, 0, len(values))
		for _, value := range values {
			fields = append(fields, value.(string))
		}
		required[path] = fields
	}
	if values, ok := node["enum"].([]interface{}); ok {
		enums[path] = append([]interface{}(nil), values...)
	}
	if value, ok := node["default"]; ok {
		defaults[path] = value
	}
	for name, value := range node {
		child, ok := value.(map[string]interface{})
		if ok {
			collectModelContractValues(child, path+"/"+name, required, enums, defaults)
		}
	}
}

func schemaNodeFromPath(root map[string]interface{}, path string) map[string]interface{} {
	var current interface{} = root
	for _, part := range strings.Split(strings.TrimPrefix(path, "#/"), "/") {
		current = current.(map[string]interface{})[part]
	}
	return current.(map[string]interface{})
}

func TestProviderSpecificSchemaSubsetAuditIsExplicitAndComplete(t *testing.T) {
	wantTypes := []reflect.Type{
		reflect.TypeOf(types.GenRepo{}),
		reflect.TypeOf(types.Filter{}),
		reflect.TypeOf(types.Visibility{}),
		reflect.TypeOf(types.PushConfig{}),
	}
	for _, typ := range wantTypes {
		audit, ok := providerSpecificSchemaSubsetAudit[typ]
		if !ok {
			t.Errorf("%s has no explicit provider-specific schema subset audit", typ.Name())
			continue
		}
		if audit.Reason == "" {
			t.Errorf("%s subset audit has no pruning reason", typ.Name())
		}
		if len(audit.AllowedFields) == 0 {
			t.Errorf("%s subset audit has no allowed YAML field set", typ.Name())
		}
		for field := range audit.AllowedFields {
			if _, ok := yamlField(typ, field); !ok {
				t.Errorf("%s subset audit allows unknown YAML field %q", typ.Name(), field)
			}
		}
	}
	if len(providerSpecificSchemaSubsetAudit) != len(wantTypes) {
		t.Errorf("provider-specific subset audit contains %d types, want exactly %d", len(providerSpecificSchemaSubsetAudit), len(wantTypes))
	}
}

type schemaSubsetAudit struct {
	AllowedFields map[string]struct{}
	Reason        string
}

func fieldSet(fields ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		result[field] = struct{}{}
	}
	return result
}

var providerSpecificSchemaSubsetAudit = map[reflect.Type]schemaSubsetAudit{
	reflect.TypeOf(types.GenRepo{}): {
		AllowedFields: fieldSet("token", "token_file", "user", "email", "organization", "ssh", "sshkey", "username", "password", "url", "exclude", "excludeorgs", "include", "includeorgs", "issues", "wiki", "starred", "createorg", "visibility", "filter", "force", "contributed", "mirrorinterval", "lfs", "mirror", "gists", "app_id", "app_installation_id", "app_private_key_file"),
		Reason:        "Provider schemas intentionally expose only the GenRepo fields supported by that source or destination provider.",
	},
	reflect.TypeOf(types.Filter{}): {
		AllowedFields: fieldSet("lastactivity", "stars", "languages", "excludearchived", "excludeforks"),
		Reason:        "Provider schemas intentionally expose only filtering capabilities implemented by that provider.",
	},
	reflect.TypeOf(types.Visibility{}): {
		AllowedFields: fieldSet("repositories", "organizations"),
		Reason:        "Provider schemas intentionally expose only visibility controls implemented by that destination provider.",
	},
	reflect.TypeOf(types.PushConfig{}): {
		AllowedFields: fieldSet("user", "password", "token", "email", "url"),
		Reason:        "Notification provider schemas intentionally expose only credentials and endpoints consumed by that provider.",
	},
}

func checkSchemaContracts(t *testing.T, node, root map[string]interface{}, path string) {
	t.Helper()
	node = resolveSchemaRef(t, node, root)
	properties, _ := node["properties"].(map[string]interface{})
	if rawRequired, exists := node["required"]; exists {
		required, ok := rawRequired.([]interface{})
		if !ok {
			t.Errorf("%s.required is not an array", path)
		} else {
			seen := map[string]bool{}
			for _, rawName := range required {
				name, ok := rawName.(string)
				if !ok || name == "" {
					t.Errorf("%s.required contains invalid entry %#v", path, rawName)
					continue
				}
				if seen[name] {
					t.Errorf("%s.required contains duplicate %q", path, name)
				}
				seen[name] = true
				if _, ok := properties[name]; !ok {
					t.Errorf("%s.required names unknown property %q", path, name)
				}
			}
		}
	}
	if rawEnum, exists := node["enum"]; exists {
		enum, ok := rawEnum.([]interface{})
		if !ok || len(enum) == 0 {
			t.Errorf("%s.enum must be a non-empty array", path)
		} else {
			seen := map[string]bool{}
			for _, value := range enum {
				encoded, err := json.Marshal(value)
				if err != nil {
					t.Errorf("%s.enum contains unencodable value %#v: %v", path, value, err)
					continue
				}
				key := string(encoded)
				if seen[key] {
					t.Errorf("%s.enum contains duplicate value %s", path, key)
				}
				seen[key] = true
			}
		}
	}
	if value, exists := node["default"]; exists {
		if want, _ := node["type"].(string); want != "" && exampleJSONType(value) != want {
			t.Errorf("%s.default has type %q, schema type %q", path, exampleJSONType(value), want)
		}
		if enum, ok := node["enum"].([]interface{}); ok && !slices.ContainsFunc(enum, func(candidate interface{}) bool {
			return reflect.DeepEqual(candidate, value)
		}) {
			t.Errorf("%s.default %#v is not present in enum %#v", path, value, enum)
		}
	}
	for name, raw := range properties {
		property, ok := raw.(map[string]interface{})
		if !ok {
			t.Errorf("%s.properties.%s is not an object", path, name)
			continue
		}
		checkSchemaContracts(t, property, root, path+"/properties/"+name)
	}
	if items, ok := node["items"].(map[string]interface{}); ok {
		checkSchemaContracts(t, items, root, path+"/items")
	}
	if additional, ok := node["additionalProperties"].(map[string]interface{}); ok {
		checkSchemaContracts(t, additional, root, path+"/additionalProperties")
	}
}

func checkModelDefaults(t *testing.T, typ reflect.Type, node, root map[string]interface{}, path string) {
	t.Helper()
	for typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	node = resolveSchemaRef(t, node, root)
	switch typ.Kind() {
	case reflect.Slice, reflect.Array:
		if items, ok := node["items"].(map[string]interface{}); ok {
			checkModelDefaults(t, typ.Elem(), items, root, path+"[]")
		}
		return
	case reflect.Map:
		if additional, ok := node["additionalProperties"].(map[string]interface{}); ok {
			checkModelDefaults(t, typ.Elem(), additional, root, path+"{}")
		}
		return
	case reflect.Struct:
	default:
		return
	}
	properties, _ := node["properties"].(map[string]interface{})
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		parts := strings.Split(field.Tag.Get("yaml"), ",")
		name := parts[0]
		if field.Anonymous && (name == "" || slices.Contains(parts[1:], "inline")) {
			checkModelDefaults(t, field.Type, node, root, path+"."+field.Name)
			continue
		}
		if name == "" || name == "-" {
			continue
		}
		property, ok := properties[name].(map[string]interface{})
		if !ok {
			continue
		}
		property = resolveSchemaRef(t, property, root)
		if want := field.Tag.Get("default"); want != "" {
			got, exists := property["default"]
			if !exists || fmt.Sprint(got) != want {
				t.Errorf("%s.%s default contract = %#v, want %q", path, field.Name, got, want)
			}
		}
		checkModelDefaults(t, field.Type, property, root, path+"."+field.Name)
	}
}

func loadContractSchema(t *testing.T) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile("gickup_spec.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]interface{}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	return schema
}

func schemaNode(t *testing.T, root map[string]interface{}, ref string) map[string]interface{} {
	t.Helper()
	var current interface{} = root
	for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		object, ok := current.(map[string]interface{})
		if !ok {
			t.Fatalf("schema path %q does not resolve to an object", ref)
		}
		current = object[part]
	}
	node, ok := current.(map[string]interface{})
	if !ok {
		t.Fatalf("schema path %q does not resolve to an object", ref)
	}
	return resolveSchemaRef(t, node, root)
}

func assertRequired(t *testing.T, root map[string]interface{}, ref, field string) {
	t.Helper()
	node := schemaNode(t, root, ref)
	for _, required := range node["required"].([]interface{}) {
		if required == field {
			return
		}
	}
	t.Errorf("%s: %q is not required", ref, field)
}

func assertEnum(t *testing.T, root map[string]interface{}, ref string, want []interface{}) {
	t.Helper()
	if got := schemaNode(t, root, ref)["enum"]; !reflect.DeepEqual(got, want) {
		t.Errorf("%s enum = %#v, want %#v", ref, got, want)
	}
}

func assertDefaultDocumented(t *testing.T, root map[string]interface{}, typ reflect.Type, fieldName string) {
	t.Helper()
	field, ok := typ.FieldByName(fieldName)
	if !ok {
		t.Fatalf("%s.%s does not exist", typ.Name(), fieldName)
	}
	want := field.Tag.Get("default")
	property := strings.Split(field.Tag.Get("yaml"), ",")[0]
	if want == "" || property == "" {
		t.Fatalf("%s.%s does not declare a YAML default contract", typ.Name(), fieldName)
	}
	var node map[string]interface{}
	switch typ.Name() {
	case "Local":
		node = schemaNode(t, root, "#/properties/destination/properties/local/items/properties/"+property)
	case "S3Repo":
		node = schemaNode(t, root, "#/properties/destination/properties/s3/items/properties/"+property)
	default:
		t.Fatalf("no schema path for %s", typ.Name())
	}
	description, _ := node["description"].(string)
	if !strings.Contains(strings.ToLower(description), "default") || !strings.Contains(strings.ToLower(description), strings.ToLower(want)) {
		t.Errorf("%s.%s default %q is not documented by schema description %q", typ.Name(), fieldName, want, description)
	}
}

func checkStructSchema(t *testing.T, typ reflect.Type, node, root map[string]interface{}) {
	t.Helper()
	for typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	node = resolveSchemaRef(t, node, root)
	properties, _ := node["properties"].(map[string]interface{})
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		tag := field.Tag.Get("yaml")
		parts := strings.Split(tag, ",")
		name := parts[0]
		if field.Anonymous && (name == "" || slices.Contains(parts[1:], "inline")) {
			checkStructSchema(t, field.Type, node, root)
			continue
		}
		if name == "" || name == "-" {
			continue
		}
		property, ok := properties[name].(map[string]interface{})
		if !ok {
			t.Errorf("%s.%s: YAML property %q is missing from schema", typ.Name(), field.Name, name)
			continue
		}
		property = resolveSchemaRef(t, property, root)
		want := jsonType(field.Type)
		if got, _ := property["type"].(string); got != want {
			t.Errorf("%s.%s: schema type %q, want %q", typ.Name(), field.Name, got, want)
		}
		checkNestedSchema(t, field.Type, property, root)
	}
}

func checkNestedSchema(t *testing.T, typ reflect.Type, node, root map[string]interface{}) {
	t.Helper()
	for typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	switch typ.Kind() {
	case reflect.Slice, reflect.Array:
		if items, ok := node["items"].(map[string]interface{}); ok {
			checkNestedSchema(t, typ.Elem(), resolveSchemaRef(t, items, root), root)
		}
	case reflect.Map:
		if additional, ok := node["additionalProperties"].(map[string]interface{}); ok {
			checkNestedSchema(t, typ.Elem(), resolveSchemaRef(t, additional, root), root)
		}
	case reflect.Struct:
		if !isProviderSpecificSchemaSubset(typ, node) {
			checkStructSchema(t, typ, node, root)
		}
		checkStructSchemaSubset(t, typ, node, root)
	}
}

func isProviderSpecificSchemaSubset(typ reflect.Type, node map[string]interface{}) bool {
	for typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	audit, ok := providerSpecificSchemaSubsetAudit[typ]
	if !ok {
		return false
	}
	properties, _ := node["properties"].(map[string]interface{})
	for name := range properties {
		if _, allowed := audit.AllowedFields[name]; !allowed {
			return false
		}
	}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		parts := strings.Split(field.Tag.Get("yaml"), ",")
		name := parts[0]
		if field.Anonymous && (name == "" || slices.Contains(parts[1:], "inline")) {
			continue
		}
		if name != "" && name != "-" {
			if _, ok := properties[name]; !ok {
				return true
			}
		}
	}
	return false
}

func checkStructSchemaSubset(t *testing.T, typ reflect.Type, node, root map[string]interface{}) {
	t.Helper()
	for typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	node = resolveSchemaRef(t, node, root)
	properties, _ := node["properties"].(map[string]interface{})
	for name, raw := range properties {
		property, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		field, ok := yamlField(typ, name)
		if !ok {
			t.Errorf("%s: schema property %q has no Go YAML field", typ.Name(), name)
			continue
		}
		property = resolveSchemaRef(t, property, root)
		want := jsonType(field.Type)
		if got, _ := property["type"].(string); got != want {
			t.Errorf("%s.%s: schema type %q, want %q", typ.Name(), field.Name, got, want)
		}
		child := field.Type
		if child.Kind() == reflect.Slice {
			child = child.Elem()
			if child.Kind() == reflect.Ptr {
				child = child.Elem()
			}
			if child.Kind() == reflect.Struct {
				if items, ok := property["items"].(map[string]interface{}); ok {
					checkStructSchemaSubset(t, child, items, root)
				}
			}
		} else if child.Kind() == reflect.Struct {
			checkStructSchemaSubset(t, child, property, root)
		}
	}
}

func yamlField(typ reflect.Type, name string) (reflect.StructField, bool) {
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if strings.Split(field.Tag.Get("yaml"), ",")[0] == name {
			return field, true
		}
	}
	return reflect.StructField{}, false
}

func resolveSchemaRef(t *testing.T, node, root map[string]interface{}) map[string]interface{} {
	t.Helper()
	ref, _ := node["$ref"].(string)
	if ref == "" {
		return node
	}
	if !strings.HasPrefix(ref, "#/") {
		t.Fatalf("unsupported schema ref %q", ref)
	}
	var current interface{} = root
	for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		current = current.(map[string]interface{})[part]
	}
	resolved, ok := current.(map[string]interface{})
	if !ok {
		t.Fatalf("schema ref %q does not resolve to an object", ref)
	}
	return resolved
}

func jsonType(typ reflect.Type) string {
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	switch typ.Kind() {
	case reflect.Struct, reflect.Map:
		return "object"
	case reflect.Slice, reflect.Array:
		return "array"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return "integer"
	default:
		return "string"
	}
}
