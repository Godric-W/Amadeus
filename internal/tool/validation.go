package tool

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

const defaultMaxRepairBytes = 1 << 20

type ArgumentErrorKind string

const (
	ArgumentErrorSchema     ArgumentErrorKind = "schema"
	ArgumentErrorParse      ArgumentErrorKind = "parse"
	ArgumentErrorValidation ArgumentErrorKind = "validation"
)

type ArgumentError struct {
	Kind    ArgumentErrorKind
	Path    string
	Message string
	Cause   error
}

func (err *ArgumentError) Error() string {
	if err.Path == "" {
		return fmt.Sprintf("tool arguments %s error: %s", err.Kind, err.Message)
	}
	return fmt.Sprintf("tool arguments %s error at %s: %s", err.Kind, err.Path, err.Message)
}

func (err *ArgumentError) Unwrap() error {
	return err.Cause
}

type ArgumentValidator struct {
	maxRepairBytes int
}

type ArgumentRepairKind string

const (
	ArgumentRepairInvalidEscape ArgumentRepairKind = "invalid_escape"
	ArgumentRepairMissingCloser ArgumentRepairKind = "missing_closer"
	ArgumentRepairTrailingComma ArgumentRepairKind = "trailing_comma"
)

type NormalizedArguments struct {
	Payload     json.RawMessage
	RepairKinds []ArgumentRepairKind
}

func NewArgumentValidator() *ArgumentValidator {
	return &ArgumentValidator{maxRepairBytes: defaultMaxRepairBytes}
}

func (validator *ArgumentValidator) Validate(spec ToolSpec, arguments json.RawMessage) (json.RawMessage, error) {
	normalized, err := validator.Normalize(spec, arguments)
	if err != nil {
		return nil, err
	}
	return normalized.Payload, nil
}

func (validator *ArgumentValidator) Normalize(spec ToolSpec, arguments json.RawMessage) (NormalizedArguments, error) {
	schema, err := compileInputSchema(spec)
	if err != nil {
		return NormalizedArguments{}, err
	}

	normalized, repairs, err := validator.parseArguments(arguments)
	if err != nil {
		return NormalizedArguments{}, err
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(normalized))
	if err != nil {
		return NormalizedArguments{}, &ArgumentError{Kind: ArgumentErrorParse, Message: "arguments are not valid JSON", Cause: err}
	}
	if _, ok := instance.(map[string]any); !ok {
		return NormalizedArguments{}, &ArgumentError{Kind: ArgumentErrorValidation, Path: "/", Message: "arguments must be a JSON object"}
	}
	if err := schema.Validate(instance); err != nil {
		return NormalizedArguments{}, validationArgumentError(err)
	}
	return NormalizedArguments{Payload: append(json.RawMessage(nil), normalized...), RepairKinds: append([]ArgumentRepairKind(nil), repairs...)}, nil
}

func (validator *ArgumentValidator) parseArguments(arguments json.RawMessage) (json.RawMessage, []ArgumentRepairKind, error) {
	trimmed := bytes.TrimSpace(arguments)
	if len(trimmed) == 0 {
		return nil, nil, &ArgumentError{Kind: ArgumentErrorParse, Message: "arguments are empty"}
	}
	if json.Valid(trimmed) {
		return append(json.RawMessage(nil), trimmed...), nil, nil
	}
	if len(trimmed) > validator.maxRepairBytes {
		return nil, nil, &ArgumentError{Kind: ArgumentErrorParse, Message: "invalid JSON exceeds repair limit"}
	}
	repaired, repairs, ok := repairJSON(trimmed)
	if !ok || !json.Valid(repaired) {
		return nil, nil, &ArgumentError{Kind: ArgumentErrorParse, Message: "arguments are not valid JSON"}
	}
	return repaired, repairs, nil
}

func compileInputSchema(spec ToolSpec) (*jsonschema.Schema, error) {
	if len(bytes.TrimSpace(spec.InputSchema)) == 0 {
		return nil, &ArgumentError{Kind: ArgumentErrorSchema, Message: "input schema is empty"}
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(spec.InputSchema))
	if err != nil {
		return nil, &ArgumentError{Kind: ArgumentErrorSchema, Message: "input schema is not valid JSON", Cause: err}
	}

	compiler := jsonschema.NewCompiler()
	compiler.UseLoader(denyExternalSchemaLoader{})
	const schemaURL = "urn:amadeus:tool-input-schema"
	if err := compiler.AddResource(schemaURL, document); err != nil {
		return nil, &ArgumentError{Kind: ArgumentErrorSchema, Message: "input schema cannot be loaded", Cause: err}
	}
	schema, err := compiler.Compile(schemaURL)
	if err != nil {
		return nil, &ArgumentError{Kind: ArgumentErrorSchema, Message: "input schema cannot be compiled", Cause: err}
	}
	return schema, nil
}

func validationArgumentError(err error) error {
	var validationError *jsonschema.ValidationError
	if !errors.As(err, &validationError) {
		return &ArgumentError{Kind: ArgumentErrorValidation, Message: "arguments do not match schema", Cause: err}
	}
	leaves := validationError.BasicOutput().Errors
	if len(leaves) == 0 {
		return &ArgumentError{
			Kind:    ArgumentErrorValidation,
			Path:    normalizeInstancePath(validationError.InstanceLocation),
			Message: validationError.Error(),
			Cause:   err,
		}
	}
	leaf := leaves[0]
	message := "arguments do not match schema"
	if leaf.Error != nil {
		message = leaf.Error.String()
	}
	return &ArgumentError{
		Kind:    ArgumentErrorValidation,
		Path:    normalizeJSONPointer(leaf.InstanceLocation),
		Message: message,
		Cause:   err,
	}
}

func normalizeInstancePath(tokens []string) string {
	if len(tokens) == 0 {
		return "/"
	}
	var builder strings.Builder
	for _, token := range tokens {
		builder.WriteByte('/')
		builder.WriteString(strings.ReplaceAll(strings.ReplaceAll(token, "~", "~0"), "/", "~1"))
	}
	return builder.String()
}

func normalizeJSONPointer(pointer string) string {
	if pointer == "" {
		return "/"
	}
	return pointer
}

type denyExternalSchemaLoader struct{}

func (denyExternalSchemaLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("external schema reference is disabled: %s", url)
}

func repairJSON(input []byte) (json.RawMessage, []ArgumentRepairKind, bool) {
	repaired, escaped := escapeInvalidStringEscapes(input)
	closed, ok := appendMissingClosers(repaired)
	if !ok {
		return nil, nil, false
	}
	withoutCommas := removeTrailingCommas(closed)
	repairs := make([]ArgumentRepairKind, 0, 3)
	if escaped {
		repairs = append(repairs, ArgumentRepairInvalidEscape)
	}
	if !bytes.Equal(closed, repaired) {
		repairs = append(repairs, ArgumentRepairMissingCloser)
	}
	if !bytes.Equal(withoutCommas, closed) {
		repairs = append(repairs, ArgumentRepairTrailingComma)
	}
	if len(repairs) == 0 {
		return nil, nil, false
	}
	return json.RawMessage(withoutCommas), repairs, true
}

func escapeInvalidStringEscapes(input []byte) ([]byte, bool) {
	output := make([]byte, 0, len(input)+8)
	inString := false
	changed := false
	for index := 0; index < len(input); index++ {
		current := input[index]
		if current == '"' {
			inString = !inString
			output = append(output, current)
			continue
		}
		if !inString || current != '\\' || index+1 >= len(input) {
			output = append(output, current)
			continue
		}
		next := input[index+1]
		if validSimpleJSONEscape(next) || next == 'u' && validUnicodeEscape(input[index+1:]) {
			output = append(output, current, next)
			index++
			continue
		}
		output = append(output, '\\', '\\')
		changed = true
	}
	return output, changed
}

func validSimpleJSONEscape(value byte) bool {
	return strings.ContainsRune(`"\\/bfnrt`, rune(value))
}

func validUnicodeEscape(value []byte) bool {
	if len(value) < 5 || value[0] != 'u' {
		return false
	}
	for _, digit := range value[1:5] {
		if !(digit >= '0' && digit <= '9' || digit >= 'a' && digit <= 'f' || digit >= 'A' && digit <= 'F') {
			return false
		}
	}
	return true
}

func removeTrailingCommas(input []byte) []byte {
	output := make([]byte, 0, len(input))
	inString := false
	escaped := false
	for index := 0; index < len(input); index++ {
		current := input[index]
		if inString {
			output = append(output, current)
			if escaped {
				escaped = false
				continue
			}
			if current == '\\' {
				escaped = true
			} else if current == '"' {
				inString = false
			}
			continue
		}
		if current == '"' {
			inString = true
			output = append(output, current)
			continue
		}
		if current == ',' {
			next := index + 1
			for next < len(input) && isJSONWhitespace(input[next]) {
				next++
			}
			if next < len(input) && (input[next] == '}' || input[next] == ']') {
				continue
			}
		}
		output = append(output, current)
	}
	return output
}

func appendMissingClosers(input []byte) ([]byte, bool) {
	stack := make([]byte, 0)
	inString := false
	escaped := false
	for _, current := range input {
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if current == '\\' {
				escaped = true
			} else if current == '"' {
				inString = false
			}
			continue
		}
		switch current {
		case '"':
			inString = true
		case '{', '[':
			stack = append(stack, current)
		case '}', ']':
			if len(stack) == 0 || !matchingBrackets(stack[len(stack)-1], current) {
				return nil, false
			}
			stack = stack[:len(stack)-1]
		}
	}
	if inString {
		return nil, false
	}
	output := append([]byte(nil), input...)
	for index := len(stack) - 1; index >= 0; index-- {
		if stack[index] == '{' {
			output = append(output, '}')
		} else {
			output = append(output, ']')
		}
	}
	return output, true
}

func matchingBrackets(open, close byte) bool {
	return open == '{' && close == '}' || open == '[' && close == ']'
}

func isJSONWhitespace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\r' || value == '\n'
}
