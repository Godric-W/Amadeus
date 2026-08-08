package patch

import (
	"errors"
	"strings"
	"testing"
)

func TestParseDocument(t *testing.T) {
	input := strings.Join([]string{
		"*** Begin Patch v1",
		"*** Add File: docs/new.md",
		"+# New",
		"+",
		"+body",
		"*** Update File: internal/example.go",
		"@@ function greeting",
		" func greeting() string {",
		"-\treturn \"old\"",
		"+\treturn \"new\"",
		" }",
		"@@ second change",
		"-old tail",
		"+new tail",
		"*** Delete File: obsolete.txt",
		"*** End Patch",
	}, "\n")

	document, err := Parse([]byte(input), ParseOptions{})
	if err != nil {
		t.Fatalf("parse patch: %v", err)
	}
	if document.Version != Version1 || document.Bytes != len(input) || len(document.Operations) != 3 {
		t.Fatalf("unexpected document: %#v", document)
	}
	add := document.Operations[0]
	if add.Kind != OperationAdd || add.Path != "docs/new.md" || len(add.AddLines) != 3 || add.AddLines[1] != "" {
		t.Fatalf("unexpected add operation: %#v", add)
	}
	update := document.Operations[1]
	if update.Kind != OperationUpdate || update.Path != "internal/example.go" || len(update.Hunks) != 2 {
		t.Fatalf("unexpected update operation: %#v", update)
	}
	if update.Hunks[0].Header != "function greeting" || len(update.Hunks[0].Lines) != 4 {
		t.Fatalf("unexpected first hunk: %#v", update.Hunks[0])
	}
	if update.Hunks[0].Lines[1].Kind != LineDelete || update.Hunks[0].Lines[2].Kind != LineAdd {
		t.Fatalf("unexpected hunk line kinds: %#v", update.Hunks[0].Lines)
	}
	deleted := document.Operations[2]
	if deleted.Kind != OperationDelete || deleted.Path != "obsolete.txt" {
		t.Fatalf("unexpected delete operation: %#v", deleted)
	}
}

func TestParseAcceptsLegacyHeaderAndCRLF(t *testing.T) {
	input := "*** Begin Patch\r\n*** Add File: empty.txt\r\n*** End Patch\r\n"
	document, err := Parse([]byte(input), ParseOptions{})
	if err != nil {
		t.Fatalf("parse legacy patch: %v", err)
	}
	if document.Version != Version1 || len(document.Operations) != 1 || len(document.Operations[0].AddLines) != 0 {
		t.Fatalf("unexpected document: %#v", document)
	}
}

func TestDocumentCloneIsolated(t *testing.T) {
	document, err := Parse([]byte(strings.Join([]string{
		"*** Begin Patch",
		"*** Update File: a.txt",
		"@@",
		"-old",
		"+new",
		"*** End Patch",
	}, "\n")), ParseOptions{})
	if err != nil {
		t.Fatalf("parse patch: %v", err)
	}
	cloned := document.Clone()
	cloned.Operations[0].Path = "changed.txt"
	cloned.Operations[0].Hunks[0].Lines[0].Content = "changed"
	if document.Operations[0].Path != "a.txt" || document.Operations[0].Hunks[0].Lines[0].Content != "old" {
		t.Fatalf("clone mutated original: %#v", document)
	}
}

func TestParseMoveWithOptionalUpdate(t *testing.T) {
	document, err := Parse([]byte(strings.Join([]string{
		"*** Begin Patch",
		"*** Update File: old.txt",
		"*** Move to: nested/new.txt",
		"@@",
		"-old",
		"+new",
		"*** End Patch",
	}, "\n")), ParseOptions{})
	if err != nil {
		t.Fatalf("parse move patch: %v", err)
	}
	if len(document.Operations) != 1 || document.Operations[0].Kind != OperationMove || document.Operations[0].Path != "old.txt" || document.Operations[0].MovePath != "nested/new.txt" || len(document.Operations[0].Hunks) != 1 {
		t.Fatalf("unexpected move operation: %#v", document.Operations)
	}
}

func TestParseEndOfFileMarker(t *testing.T) {
	document, err := Parse([]byte("*** Begin Patch\n*** Update File: a.txt\n@@\n-old\n+new\n*** End of File\n*** End Patch"), ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Operations) != 1 || len(document.Operations[0].Hunks) != 1 || !document.Operations[0].Hunks[0].EndOfFile {
		t.Fatalf("End of File marker was not preserved: %#v", document)
	}
}

func TestParseRejectsInvalidDocuments(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		line    int
		column  int
		message string
	}{
		{name: "unknown version", input: "*** Begin Patch v2\n*** End Patch", line: 1, column: 17, message: "unsupported patch version"},
		{name: "missing header", input: "*** Add File: a\n+x\n*** End Patch", line: 1, column: 1, message: "expected"},
		{name: "no operations", input: "*** Begin Patch\n*** End Patch", line: 2, column: 1, message: "no operations"},
		{name: "missing end", input: "*** Begin Patch\n*** Add File: a\n+x", line: 3, column: 3, message: "missing end marker"},
		{name: "trailing content", input: "*** Begin Patch\n*** Add File: a\n+x\n*** End Patch\nextra", line: 5, column: 1, message: "unexpected content"},
		{name: "empty path", input: "*** Begin Patch\n*** Add File:   \n+x\n*** End Patch", line: 2, column: 14, message: "path is empty"},
		{name: "duplicate path", input: "*** Begin Patch\n*** Add File: a\n+x\n*** Delete File: a\n*** End Patch", line: 4, column: 1, message: "duplicate operation"},
		{name: "add body prefix", input: "*** Begin Patch\n*** Add File: a\nplain\n*** End Patch", line: 3, column: 1, message: "must start with '+'"},
		{name: "delete body", input: "*** Begin Patch\n*** Delete File: a\n-body\n*** End Patch", line: 3, column: 1, message: "cannot contain body"},
		{name: "update without hunk", input: "*** Begin Patch\n*** Update File: a\n*** End Patch", line: 2, column: 1, message: "requires at least one hunk"},
		{name: "empty move destination", input: "*** Begin Patch\n*** Update File: a\n*** Move to:   \n*** End Patch", line: 3, column: 13, message: "move destination is empty"},
		{name: "duplicate move destination", input: "*** Begin Patch\n*** Add File: b\n+x\n*** Update File: a\n*** Move to: b\n*** End Patch", line: 4, column: 1, message: "duplicate operation"},
		{name: "invalid hunk header", input: "*** Begin Patch\n*** Update File: a\nnot-a-hunk\n*** End Patch", line: 3, column: 1, message: "must start with '@@'"},
		{name: "empty hunk", input: "*** Begin Patch\n*** Update File: a\n@@\n*** End Patch", line: 3, column: 1, message: "hunk is empty"},
		{name: "hunk no old lines", input: "*** Begin Patch\n*** Update File: a\n@@\n+new\n*** End Patch", line: 3, column: 1, message: "requires context"},
		{name: "hunk no changes", input: "*** Begin Patch\n*** Update File: a\n@@\n old\n*** End Patch", line: 3, column: 1, message: "has no changes"},
		{name: "invalid hunk line", input: "*** Begin Patch\n*** Update File: a\n@@\n?old\n*** End Patch", line: 4, column: 1, message: "must start with space"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Parse([]byte(test.input), ParseOptions{})
			if err == nil {
				t.Fatal("expected parse error")
			}
			var parseError *ParseError
			if !errors.As(err, &parseError) {
				t.Fatalf("expected ParseError, got %T: %v", err, err)
			}
			if parseError.Line != test.line || parseError.Column != test.column || !strings.Contains(parseError.Message, test.message) {
				t.Fatalf("unexpected parse error: %#v", parseError)
			}
		})
	}
}

func TestParseRejectsBudgetsAndInvalidUTF8(t *testing.T) {
	valid := []byte("*** Begin Patch\n*** Add File: a\n+x\n*** End Patch")
	tests := []struct {
		name    string
		input   []byte
		options ParseOptions
		message string
	}{
		{name: "bytes", input: valid, options: ParseOptions{MaxBytes: len(valid) - 1}, message: "size"},
		{name: "lines", input: valid, options: ParseOptions{MaxLines: 3}, message: "lines"},
		{name: "operations", input: valid, options: ParseOptions{MaxOperations: 0, MaxBytes: 1}, message: "size"},
		{name: "invalid utf8", input: []byte{0xff}, options: ParseOptions{}, message: "UTF-8"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Parse(test.input, test.options)
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestParseRejectsOperationAndHunkLimits(t *testing.T) {
	operations := "*** Begin Patch\n*** Add File: a\n+x\n*** Add File: b\n+y\n*** End Patch"
	_, err := Parse([]byte(operations), ParseOptions{MaxOperations: 1})
	if err == nil || !strings.Contains(err.Error(), "operation count exceeds") {
		t.Fatalf("unexpected operation limit error: %v", err)
	}

	hunks := "*** Begin Patch\n*** Update File: a\n@@\n-old\n+new\n@@\n-tail\n+next\n*** End Patch"
	_, err = Parse([]byte(hunks), ParseOptions{MaxHunks: 1})
	if err == nil || !strings.Contains(err.Error(), "hunk count exceeds") {
		t.Fatalf("unexpected hunk limit error: %v", err)
	}
}
