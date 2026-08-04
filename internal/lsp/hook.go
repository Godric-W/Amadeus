package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/agent/react"
	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type WriteHook struct {
	client     Client
	root       project.Root
	events     event.Sink
	extensions map[string]struct{}
}

type WriteHookOptions struct {
	Extensions []string
}

func NewWriteHook(client Client, root project.Root, events event.Sink) (*WriteHook, error) {
	return NewWriteHookWithOptions(client, root, events, WriteHookOptions{})
}

func NewWriteHookWithOptions(client Client, root project.Root, events event.Sink, options WriteHookOptions) (*WriteHook, error) {
	if client == nil {
		return nil, errors.New("LSP write hook client is nil")
	}
	if root.Path() == "" {
		return nil, errors.New("LSP write hook project root is empty")
	}
	extensions := make(map[string]struct{}, len(options.Extensions))
	for _, extension := range options.Extensions {
		extension = strings.ToLower(strings.TrimSpace(extension))
		if extension != "" {
			extensions[extension] = struct{}{}
		}
	}
	return &WriteHook{client: client, root: root, events: events, extensions: extensions}, nil
}

func (hook *WriteHook) After(ctx context.Context, spec tool.Spec, call tool.Call, result tool.Result) ([]react.Evidence, error) {
	if hook == nil || hook.client == nil {
		return nil, errors.New("LSP write hook is nil")
	}
	if spec.SideEffect != tool.SideEffectWrite {
		return nil, nil
	}
	documents, err := hook.documents(call, result)
	if err != nil {
		return hook.failureEvidence(ctx, call, err), nil
	}
	evidence := make([]react.Evidence, 0)
	for documentIndex, document := range documents {
		if !hook.matches(document.Path) {
			continue
		}
		diagnostics, observeErr := hook.client.Observe(ctx, document)
		if observeErr != nil {
			evidence = append(evidence, hook.failureEvidence(ctx, call, observeErr)...)
			continue
		}
		for diagnosticIndex, diagnostic := range diagnostics {
			summary := strings.TrimSpace(diagnostic.Message)
			if summary == "" {
				summary = "LSP diagnostic without message"
			}
			if diagnostic.Code != "" {
				summary = diagnostic.Code + ": " + summary
			}
			item := react.Evidence{
				ID: react.EvidenceID(fmt.Sprintf("lsp/%s/%d/%d", call.ID, documentIndex+1, diagnosticIndex+1)), Kind: react.EvidenceDiagnostic,
				Source: nonEmpty(diagnostic.Source, "lsp"), Summary: summary,
				Artifact: &react.ArtifactRef{Path: document.Path}, Verified: diagnostic.Severity != SeverityError,
			}
			evidence = append(evidence, item)
			if hook.events != nil {
				_ = hook.events.Publish(ctx, event.DiagnosticPublished{Severity: string(diagnostic.Severity), Code: diagnostic.Code, Message: summary})
			}
		}
	}
	return evidence, nil
}

func (hook *WriteHook) matches(path string) bool {
	if hook == nil || len(hook.extensions) == 0 {
		return true
	}
	_, ok := hook.extensions[strings.ToLower(filepath.Ext(path))]
	return ok
}

func (hook *WriteHook) documents(call tool.Call, result tool.Result) ([]Document, error) {
	if call.Name == "write_file" {
		var arguments struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(call.Arguments, &arguments); err != nil {
			return nil, fmt.Errorf("decode write_file diagnostics input: %w", err)
		}
		if strings.TrimSpace(arguments.Path) == "" {
			return nil, errors.New("write_file diagnostics path is empty")
		}
		return []Document{{Path: arguments.Path, Content: arguments.Content}}, nil
	}
	if call.Name != "apply_patch" {
		return nil, nil
	}
	operations, ok := result.Metadata["operations"].([]map[string]any)
	if !ok {
		return nil, nil
	}
	documents := make([]Document, 0, len(operations))
	seen := make(map[string]struct{}, len(operations))
	for _, operation := range operations {
		path, _ := operation["path"].(string)
		deleted, _ := operation["deleted"].(bool)
		if path == "" || deleted {
			continue
		}
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		resolved, err := hook.root.Resolve(path)
		if err != nil {
			return nil, err
		}
		content, err := os.ReadFile(resolved)
		if err != nil {
			return nil, fmt.Errorf("read patched file %q for LSP diagnostics: %w", path, err)
		}
		documents = append(documents, Document{Path: filepath.ToSlash(path), Content: string(content)})
	}
	return documents, nil
}

func (hook *WriteHook) failureEvidence(ctx context.Context, call tool.Call, cause error) []react.Evidence {
	summary := "LSP diagnostics unavailable: " + strings.TrimSpace(cause.Error())
	if hook.events != nil {
		_ = hook.events.Publish(ctx, event.DiagnosticPublished{Severity: string(SeverityWarning), Code: "lsp_hook_failed", Message: summary})
	}
	return []react.Evidence{{ID: react.EvidenceID("lsp/" + call.ID + "/error"), Kind: react.EvidenceDiagnostic, Source: "lsp", Summary: summary, Verified: false}}
}

func nonEmpty(value, fallback string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return fallback
}
