package patch

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	DefaultMaxBytes      = 1 << 20
	DefaultMaxOperations = 128
	DefaultMaxHunks      = 1024
	DefaultMaxLines      = 20000
)

const (
	legacyBeginMarker = "*** Begin Patch"
	beginMarkerV1     = "*** Begin Patch v1"
	endMarker         = "*** End Patch"
	addPrefix         = "*** Add File:"
	updatePrefix      = "*** Update File:"
	deletePrefix      = "*** Delete File:"
	moveToPrefix      = "*** Move to:"
)

type ParseOptions struct {
	MaxBytes      int
	MaxOperations int
	MaxHunks      int
	MaxLines      int
}

type ParseError struct {
	Line    int
	Column  int
	Message string
}

func (parseError *ParseError) Error() string {
	return fmt.Sprintf("patch line %d column %d: %s", parseError.Line, parseError.Column, parseError.Message)
}

func Parse(input []byte, options ParseOptions) (Document, error) {
	options = normalizeOptions(options)
	if len(input) == 0 {
		return Document{}, errors.New("patch document is empty")
	}
	if len(input) > options.MaxBytes {
		return Document{}, fmt.Errorf("patch document size %d exceeds limit %d", len(input), options.MaxBytes)
	}
	if !utf8.Valid(input) {
		return Document{}, errors.New("patch document is not valid UTF-8")
	}

	lines := splitDocumentLines(string(input))
	if len(lines) > options.MaxLines {
		return Document{}, fmt.Errorf("patch document lines %d exceeds limit %d", len(lines), options.MaxLines)
	}
	if len(lines) == 0 {
		return Document{}, errors.New("patch document is empty")
	}

	version, err := parseVersion(lines[0])
	if err != nil {
		return Document{}, err
	}

	parser := documentParser{
		lines:   lines,
		options: options,
		seen:    make(map[string]int),
	}
	document := Document{Version: version, Bytes: len(input)}
	parser.index = 1

	for parser.index < len(parser.lines) {
		line := parser.lines[parser.index]
		if line == endMarker {
			if len(document.Operations) == 0 {
				return Document{}, parser.errorAt(parser.index, 1, "patch document has no operations")
			}
			parser.index++
			if parser.index != len(parser.lines) {
				return Document{}, parser.errorAt(parser.index, 1, "unexpected content after end marker")
			}
			return document, nil
		}
		if len(document.Operations) >= options.MaxOperations {
			return Document{}, parser.errorAt(parser.index, 1,
				fmt.Sprintf("operation count exceeds limit %d", options.MaxOperations))
		}

		operation, parseErr := parser.parseOperation()
		if parseErr != nil {
			return Document{}, parseErr
		}
		if previousLine, duplicate := parser.seen[operation.Path]; duplicate {
			return Document{}, parser.errorAt(operation.Line-1, 1,
				fmt.Sprintf("duplicate operation for path %q; first declared at line %d", operation.Path, previousLine))
		}
		parser.seen[operation.Path] = operation.Line
		if operation.MovePath != "" {
			if previousLine, duplicate := parser.seen[operation.MovePath]; duplicate {
				return Document{}, parser.errorAt(operation.Line-1, 1,
					fmt.Sprintf("duplicate operation for path %q; first declared at line %d", operation.MovePath, previousLine))
			}
			parser.seen[operation.MovePath] = operation.Line
		}
		document.Operations = append(document.Operations, operation)
	}

	return Document{}, parser.errorAt(len(parser.lines)-1, len(parser.lines[len(parser.lines)-1])+1,
		"missing end marker")
}

func normalizeOptions(options ParseOptions) ParseOptions {
	if options.MaxBytes <= 0 {
		options.MaxBytes = DefaultMaxBytes
	}
	if options.MaxOperations <= 0 {
		options.MaxOperations = DefaultMaxOperations
	}
	if options.MaxHunks <= 0 {
		options.MaxHunks = DefaultMaxHunks
	}
	if options.MaxLines <= 0 {
		options.MaxLines = DefaultMaxLines
	}
	return options
}

func splitDocumentLines(input string) []string {
	lines := strings.Split(input, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for index := range lines {
		lines[index] = strings.TrimSuffix(lines[index], "\r")
	}
	return lines
}

func parseVersion(firstLine string) (Version, error) {
	switch firstLine {
	case legacyBeginMarker, beginMarkerV1:
		return Version1, nil
	default:
		if strings.HasPrefix(firstLine, legacyBeginMarker+" ") {
			version := strings.TrimSpace(strings.TrimPrefix(firstLine, legacyBeginMarker))
			return "", &ParseError{Line: 1, Column: len(legacyBeginMarker) + 2,
				Message: fmt.Sprintf("unsupported patch version %q", version)}
		}
		return "", &ParseError{Line: 1, Column: 1,
			Message: fmt.Sprintf("expected %q or %q", legacyBeginMarker, beginMarkerV1)}
	}
}

type documentParser struct {
	lines     []string
	index     int
	hunkCount int
	options   ParseOptions
	seen      map[string]int
}

func (parser *documentParser) parseOperation() (Operation, error) {
	lineIndex := parser.index
	line := parser.lines[lineIndex]

	var kind OperationKind
	var prefix string
	switch {
	case strings.HasPrefix(line, addPrefix):
		kind, prefix = OperationAdd, addPrefix
	case strings.HasPrefix(line, updatePrefix):
		kind, prefix = OperationUpdate, updatePrefix
	case strings.HasPrefix(line, deletePrefix):
		kind, prefix = OperationDelete, deletePrefix
	default:
		return Operation{}, parser.errorAt(lineIndex, 1, "expected add, update, delete, or end marker")
	}

	path := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	if path == "" {
		return Operation{}, parser.errorAt(lineIndex, len(prefix)+1, "operation path is empty")
	}
	if strings.ContainsRune(path, '\x00') {
		return Operation{}, parser.errorAt(lineIndex, len(prefix)+1, "operation path contains NUL")
	}

	operation := Operation{Kind: kind, Path: path, Line: lineIndex + 1}
	parser.index++

	switch kind {
	case OperationAdd:
		for parser.index < len(parser.lines) && !isBoundary(parser.lines[parser.index]) {
			bodyLine := parser.lines[parser.index]
			if !strings.HasPrefix(bodyLine, "+") {
				return Operation{}, parser.errorAt(parser.index, 1, "add file content line must start with '+'")
			}
			operation.AddLines = append(operation.AddLines, strings.TrimPrefix(bodyLine, "+"))
			parser.index++
		}
	case OperationDelete:
		if parser.index < len(parser.lines) && !isBoundary(parser.lines[parser.index]) {
			return Operation{}, parser.errorAt(parser.index, 1, "delete operation cannot contain body lines")
		}
	case OperationUpdate:
		if parser.index < len(parser.lines) && strings.HasPrefix(parser.lines[parser.index], moveToPrefix) {
			moveLine := parser.lines[parser.index]
			operation.MovePath = strings.TrimSpace(strings.TrimPrefix(moveLine, moveToPrefix))
			if operation.MovePath == "" {
				return Operation{}, parser.errorAt(parser.index, len(moveToPrefix)+1, "move destination is empty")
			}
			if strings.ContainsRune(operation.MovePath, '\x00') {
				return Operation{}, parser.errorAt(parser.index, len(moveToPrefix)+1, "move destination contains NUL")
			}
			operation.Kind = OperationMove
			parser.index++
		}
		for parser.index < len(parser.lines) && !isOperationOrEnd(parser.lines[parser.index]) {
			if parser.hunkCount >= parser.options.MaxHunks {
				return Operation{}, parser.errorAt(parser.index, 1,
					fmt.Sprintf("hunk count exceeds limit %d", parser.options.MaxHunks))
			}
			hunk, err := parser.parseHunk()
			if err != nil {
				return Operation{}, err
			}
			parser.hunkCount++
			operation.Hunks = append(operation.Hunks, hunk)
		}
		if len(operation.Hunks) == 0 && operation.Kind != OperationMove {
			return Operation{}, parser.errorAt(lineIndex, 1, "update operation requires at least one hunk")
		}
	}

	return operation, nil
}

func (parser *documentParser) parseHunk() (Hunk, error) {
	lineIndex := parser.index
	header := parser.lines[lineIndex]
	if header != "@@" && !strings.HasPrefix(header, "@@ ") {
		return Hunk{}, parser.errorAt(lineIndex, 1, "update hunk must start with '@@'")
	}

	hunk := Hunk{Header: strings.TrimSpace(strings.TrimPrefix(header, "@@")), Line: lineIndex + 1}
	parser.index++
	hasOldLine := false
	hasChange := false

	for parser.index < len(parser.lines) && !isBoundary(parser.lines[parser.index]) {
		bodyLine := parser.lines[parser.index]
		if bodyLine == "" {
			return Hunk{}, parser.errorAt(parser.index, 1, "hunk line must start with space, '+', or '-'")
		}
		line := Line{Content: bodyLine[1:], Line: parser.index + 1}
		switch bodyLine[0] {
		case ' ':
			line.Kind = LineContext
			hasOldLine = true
		case '+':
			line.Kind = LineAdd
			hasChange = true
		case '-':
			line.Kind = LineDelete
			hasOldLine = true
			hasChange = true
		default:
			return Hunk{}, parser.errorAt(parser.index, 1, "hunk line must start with space, '+', or '-'")
		}
		hunk.Lines = append(hunk.Lines, line)
		parser.index++
	}

	if len(hunk.Lines) == 0 {
		return Hunk{}, parser.errorAt(lineIndex, 1, "update hunk is empty")
	}
	if !hasOldLine {
		return Hunk{}, parser.errorAt(lineIndex, 1, "update hunk requires context or deleted lines")
	}
	if !hasChange {
		return Hunk{}, parser.errorAt(lineIndex, 1, "update hunk has no changes")
	}
	return hunk, nil
}

func (parser *documentParser) errorAt(lineIndex int, column int, message string) *ParseError {
	if lineIndex < 0 {
		lineIndex = 0
	}
	if column < 1 {
		column = 1
	}
	return &ParseError{Line: lineIndex + 1, Column: column, Message: message}
}

func isBoundary(line string) bool {
	return isOperationOrEnd(line) || strings.HasPrefix(line, moveToPrefix) || line == "@@" || strings.HasPrefix(line, "@@ ")
}

func isOperationOrEnd(line string) bool {
	return line == endMarker || strings.HasPrefix(line, addPrefix) ||
		strings.HasPrefix(line, updatePrefix) || strings.HasPrefix(line, deletePrefix)
}
