package tool

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestArgumentValidatorAcceptsValidArguments(t *testing.T) {
	validator := NewArgumentValidator()
	validated, err := validator.Validate(validationSpec(false), json.RawMessage(`{"path":"README.md","limit":10}`))
	if err != nil {
		t.Fatalf("validate arguments: %v", err)
	}
	if string(validated) != `{"path":"README.md","limit":10}` {
		t.Fatalf("unexpected validated arguments: %s", validated)
	}
}

func TestArgumentValidatorReportsMissingTypeAndUnknownFields(t *testing.T) {
	validator := NewArgumentValidator()
	tests := []struct {
		name      string
		arguments string
		path      string
		message   string
	}{
		{name: "missing", arguments: `{"limit":10}`, path: "/", message: "missing"},
		{name: "type", arguments: `{"path":12}`, path: "/path", message: "string"},
		{name: "unknown", arguments: `{"path":"a","extra":true}`, path: "/", message: "additional"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := validator.Validate(validationSpec(false), json.RawMessage(test.arguments))
			var argumentError *ArgumentError
			if !errors.As(err, &argumentError) || argumentError.Kind != ArgumentErrorValidation {
				t.Fatalf("unexpected validation error: %v", err)
			}
			if argumentError.Path != test.path || !strings.Contains(strings.ToLower(argumentError.Message), test.message) {
				t.Fatalf("unexpected error detail: %#v", argumentError)
			}
		})
	}
}

func TestArgumentValidatorAllowsUnknownFieldsWhenSchemaAllowsThem(t *testing.T) {
	validator := NewArgumentValidator()
	if _, err := validator.Validate(validationSpec(true), json.RawMessage(`{"path":"a","extra":true}`)); err != nil {
		t.Fatalf("schema unexpectedly rejected unknown field: %v", err)
	}
}

func TestArgumentValidatorRepairsOnlyLimitedSyntax(t *testing.T) {
	validator := NewArgumentValidator()
	repaired, err := validator.Validate(validationSpec(false), json.RawMessage(`{"path":"README.md",`))
	if err != nil {
		t.Fatalf("repair trailing comma and missing brace: %v", err)
	}
	if string(repaired) != `{"path":"README.md"}` {
		t.Fatalf("unexpected repaired arguments: %s", repaired)
	}

	_, err = validator.Validate(validationSpec(false), json.RawMessage(`{path:"README.md"}`))
	var argumentError *ArgumentError
	if !errors.As(err, &argumentError) || argumentError.Kind != ArgumentErrorParse {
		t.Fatalf("unsafe repair unexpectedly succeeded: %v", err)
	}
}

func TestArgumentValidatorValidatesAfterRepair(t *testing.T) {
	validator := NewArgumentValidator()
	_, err := validator.Validate(validationSpec(false), json.RawMessage(`{"limit":10,`))
	var argumentError *ArgumentError
	if !errors.As(err, &argumentError) || argumentError.Kind != ArgumentErrorValidation {
		t.Fatalf("repaired arguments skipped schema validation: %v", err)
	}
}

func TestArgumentValidatorRejectsInvalidSchemaExternalReferencesAndNonObjects(t *testing.T) {
	validator := NewArgumentValidator()
	tests := []struct {
		name      string
		spec      Spec
		arguments json.RawMessage
		errorKind ArgumentErrorKind
	}{
		{name: "invalid schema", spec: Spec{InputSchema: json.RawMessage(`{"type":`)}, arguments: json.RawMessage(`{}`), errorKind: ArgumentErrorSchema},
		{name: "external ref", spec: Spec{InputSchema: json.RawMessage(`{"$ref":"https://example.com/schema.json"}`)}, arguments: json.RawMessage(`{}`), errorKind: ArgumentErrorSchema},
		{name: "non object", spec: validationSpec(false), arguments: json.RawMessage(`[]`), errorKind: ArgumentErrorValidation},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := validator.Validate(test.spec, test.arguments)
			var argumentError *ArgumentError
			if !errors.As(err, &argumentError) || argumentError.Kind != test.errorKind {
				t.Fatalf("unexpected argument error: %v", err)
			}
		})
	}
}

func validationSpec(allowUnknown bool) Spec {
	schema := `{
		"type":"object",
		"properties":{
			"path":{"type":"string"},
			"limit":{"type":"integer"}
		},
		"required":["path"],
		"additionalProperties":false
	}`
	if allowUnknown {
		schema = strings.Replace(schema, `"additionalProperties":false`, `"additionalProperties":true`, 1)
	}
	return Spec{InputSchema: json.RawMessage(schema)}
}
