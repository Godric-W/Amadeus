package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/project"
)

func TestGrepFallbackReturnsLineNumbersAndContext(t *testing.T) {
	rootPath := t.TempDir()
	content := "package sample\n\nfunc Alpha() {}\nfunc Beta() {}\n"
	if err := os.WriteFile(filepath.Join(rootPath, "sample.go"), []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	grep := newTestGrep(t, rootPath, 10, 1024)

	result, err := executePreparedTool(t, context.Background(), grep, json.RawMessage(`{"query":"func Alpha","context":1}`))
	if err != nil {
		t.Fatalf("grep code: %v", err)
	}
	want := "sample.go-2-\nsample.go:3:1:func Alpha() {}\nsample.go-4-func Beta() {}"
	if result.Text != want || result.Partial || result.Metadata["backend"] != "go" {
		t.Fatalf("unexpected grep result: %#v", result)
	}
}

func TestGrepRipgrepFastPathMatchesFallbackSemantics(t *testing.T) {
	rootPath := t.TempDir()
	for _, fixture := range []struct{ path, content string }{{"a.go", "before\nneedle\nafter\n"}, {"b.txt", "none\n"}} {
		if err := os.WriteFile(filepath.Join(rootPath, fixture.path), []byte(fixture.content), 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}
	fakeRG := filepath.Join(t.TempDir(), "rg")
	script := `#!/bin/sh
printf '%s\n' '{"type":"context","data":{"path":{"text":"a.go"},"lines":{"text":"before\n"},"line_number":1,"submatches":[]}}'
printf '%s\n' '{"type":"match","data":{"path":{"text":"a.go"},"lines":{"text":"needle\n"},"line_number":2,"submatches":[{"start":0,"end":6,"match":{"text":"needle"}}]}}'
printf '%s\n' '{"type":"context","data":{"path":{"text":"a.go"},"lines":{"text":"after\n"},"line_number":3,"submatches":[]}}'
`
	if err := os.WriteFile(fakeRG, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake rg: %v", err)
	}
	root, err := project.NewRoot(rootPath)
	if err != nil {
		t.Fatalf("create project root: %v", err)
	}
	fast, err := NewGrep(root, GrepOptions{MaxResults: 10, MaxFileBytes: 1024, MaxContextLines: 2, RipgrepPath: fakeRG})
	if err != nil {
		t.Fatalf("create fast grep: %v", err)
	}
	fallback := newTestGrep(t, rootPath, 10, 1024)
	input := json.RawMessage(`{"query":"needle","context":1}`)
	fastResult, err := executePreparedTool(t, context.Background(), fast, input)
	if err != nil {
		t.Fatalf("run rg grep: %v", err)
	}
	fallbackResult, err := executePreparedTool(t, context.Background(), fallback, input)
	if err != nil {
		t.Fatalf("run fallback grep: %v", err)
	}
	if fastResult.Text != fallbackResult.Text || fastResult.Partial != fallbackResult.Partial || fastResult.Metadata["backend"] != "rg" {
		t.Fatalf("rg semantics differ: fast=%#v fallback=%#v", fastResult, fallbackResult)
	}
}

func TestGrepFallsBackWhenRipgrepFails(t *testing.T) {
	rootPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootPath, "a.txt"), []byte("needle\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	fakeRG := filepath.Join(t.TempDir(), "rg")
	if err := os.WriteFile(fakeRG, []byte("#!/bin/sh\nexit 2\n"), 0o755); err != nil {
		t.Fatalf("write failing rg: %v", err)
	}
	root, err := project.NewRoot(rootPath)
	if err != nil {
		t.Fatalf("create project root: %v", err)
	}
	grep, err := NewGrep(root, GrepOptions{MaxResults: 10, MaxFileBytes: 1024, MaxContextLines: 2, RipgrepPath: fakeRG})
	if err != nil {
		t.Fatalf("create grep: %v", err)
	}
	result, err := executePreparedTool(t, context.Background(), grep, json.RawMessage(`{"query":"needle"}`))
	if err != nil || result.Text != "a.txt:1:1:needle" || result.Metadata["backend"] != "go" {
		t.Fatalf("unexpected fallback result: result=%#v err=%v", result, err)
	}
}

func TestGrepFallbackSupportsRegexCaseAndResultLimit(t *testing.T) {
	rootPath := t.TempDir()
	for _, fixture := range []struct{ path, content string }{
		{"a.go", "TODO first\n"}, {"b.go", "todo second\n"}, {"c.go", "TODO third\n"},
	} {
		if err := os.WriteFile(filepath.Join(rootPath, fixture.path), []byte(fixture.content), 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}
	grep := newTestGrep(t, rootPath, 2, 1024)
	result, err := executePreparedTool(t, context.Background(), grep, json.RawMessage(`{"query":"^todo","regex":true,"case_sensitive":false}`))
	if err != nil || !result.Partial || !strings.Contains(result.Text, "a.go:1") || !strings.Contains(result.Text, "b.go:1") || strings.Contains(result.Text, "c.go") {
		t.Fatalf("unexpected limited regex result: result=%#v err=%v", result, err)
	}
}

func TestGrepFallbackSkipsBinaryOversizeAndIgnoredTrees(t *testing.T) {
	rootPath := t.TempDir()
	fixtures := map[string][]byte{
		"visible.txt":      []byte("needle\n"),
		"binary.bin":       {'n', 0, 'e'},
		"large.txt":        []byte("needle too large"),
		".git/config":      []byte("needle\n"),
		"vendor/vendor.go": []byte("needle\n"),
	}
	for name, content := range fixtures {
		path := filepath.Join(rootPath, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create fixture parent: %v", err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}
	grep := newTestGrep(t, rootPath, 10, 8)
	result, err := executePreparedTool(t, context.Background(), grep, json.RawMessage(`{"query":"needle"}`))
	if err != nil || result.Text != "visible.txt:1:1:needle" || result.Metadata["files_skipped"] != 2 {
		t.Fatalf("unexpected skip result: result=%#v err=%v", result, err)
	}
}

func newTestGrep(t *testing.T, rootPath string, maxResults int, maxFileBytes int64) *Grep {
	t.Helper()
	root, err := project.NewRoot(rootPath)
	if err != nil {
		t.Fatalf("create project root: %v", err)
	}
	grep, err := NewGrep(root, GrepOptions{MaxResults: maxResults, MaxFileBytes: maxFileBytes, MaxContextLines: 3, DisableRipgrep: true})
	if err != nil {
		t.Fatalf("create grep: %v", err)
	}
	return grep
}
