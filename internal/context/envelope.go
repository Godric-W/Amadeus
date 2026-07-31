package agentcontext

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/Godric-W/Amadeus/internal/instruction"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/prompt"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type SourceKind string

const (
	SourcePromptBundle SourceKind = "prompt_bundle"
	SourcePromptLayer  SourceKind = "prompt_layer"
	SourceInstruction  SourceKind = "instruction"
	SourceTask         SourceKind = "task"
	SourceTool         SourceKind = "tool"
)

type Source struct {
	Kind      SourceKind `json:"kind"`
	ID        string     `json:"id"`
	Path      string     `json:"path,omitempty"`
	ScopeKind string     `json:"scope_kind,omitempty"`
	ScopePath string     `json:"scope_path,omitempty"`
	SHA256    string     `json:"sha256"`
}

type BuildInput struct {
	Prompt             prompt.Bundle
	InstructionRequest instruction.ResolveRequest
	Instructions       instruction.Resolution
	Task               string
	Tools              []tool.Spec
}

type Envelope struct {
	Messages       []llm.Message `json:"messages"`
	AvailableTools []tool.Spec   `json:"available_tools"`
	Sources        []Source      `json:"sources"`
	SHA256         string        `json:"sha256"`
}

type Builder struct{}

func NewBuilder() *Builder { return &Builder{} }

func (builder *Builder) Build(ctx context.Context, input BuildInput) (Envelope, error) {
	if builder == nil {
		return Envelope{}, errors.New("Agent context builder is nil")
	}
	if ctx == nil {
		return Envelope{}, errors.New("Agent context context is nil")
	}
	if err := ctx.Err(); err != nil {
		return Envelope{}, err
	}
	if err := validatePromptBundle(input.Prompt); err != nil {
		return Envelope{}, err
	}
	if err := input.Instructions.Validate(input.InstructionRequest); err != nil {
		return Envelope{}, fmt.Errorf("validate Agent context instructions: %w", err)
	}
	task := strings.TrimSpace(input.Task)
	if task == "" {
		return Envelope{}, errors.New("Agent context task is empty")
	}
	tools, err := normalizeTools(input.Tools)
	if err != nil {
		return Envelope{}, err
	}
	instructionContent, err := marshalInstructionEnvelope(input.Instructions)
	if err != nil {
		return Envelope{}, err
	}
	if err := ctx.Err(); err != nil {
		return Envelope{}, err
	}

	envelope := Envelope{
		Messages: []llm.Message{
			llm.SystemMessage(input.Prompt.Content),
			llm.DeveloperMessage(instructionContent),
			llm.UserMessage(task),
		},
		AvailableTools: tools,
		Sources:        envelopeSources(input.Prompt, input.Instructions, task, tools),
	}
	hash, err := envelopeHash(envelope)
	if err != nil {
		return Envelope{}, err
	}
	envelope.SHA256 = hash
	return envelope, nil
}

func (envelope Envelope) Clone() Envelope {
	cloned := Envelope{SHA256: envelope.SHA256}
	cloned.Messages = cloneMessages(envelope.Messages)
	cloned.AvailableTools = cloneSpecs(envelope.AvailableTools)
	cloned.Sources = append([]Source(nil), envelope.Sources...)
	return cloned
}

func validatePromptBundle(bundle prompt.Bundle) error {
	if strings.TrimSpace(bundle.Content) == "" {
		return errors.New("Agent context Prompt content is empty")
	}
	if bundle.SHA256 != contentHash(bundle.Content) {
		return errors.New("Agent context Prompt SHA-256 does not match content")
	}
	if len(bundle.Sources) == 0 {
		return errors.New("Agent context Prompt sources are empty")
	}
	for index, source := range bundle.Sources {
		if strings.TrimSpace(source.Kind) == "" || strings.TrimSpace(source.Path) == "" || !validHash(source.SHA256) {
			return fmt.Errorf("Agent context Prompt source %d is invalid", index)
		}
	}
	return nil
}

func normalizeTools(specs []tool.Spec) ([]tool.Spec, error) {
	result := cloneSpecs(specs)
	sort.Slice(result, func(left, right int) bool { return result[left].Name < result[right].Name })
	for index := range result {
		if err := tool.ValidateSpec(result[index]); err != nil {
			return nil, fmt.Errorf("validate Agent context tool %d: %w", index, err)
		}
		if index > 0 && result[index-1].Name == result[index].Name {
			return nil, fmt.Errorf("Agent context tool %q is duplicated", result[index].Name)
		}
		canonical, err := canonicalJSON(result[index].InputSchema)
		if err != nil {
			return nil, fmt.Errorf("validate Agent context tool %q schema: %w", result[index].Name, err)
		}
		result[index].InputSchema = canonical
	}
	return result, nil
}

type instructionEnvelope struct {
	Type      string                        `json:"type"`
	Target    instructionTarget             `json:"target"`
	Documents []instructionEnvelopeDocument `json:"documents"`
}

type instructionTarget struct {
	Path string                 `json:"path"`
	Kind instruction.TargetKind `json:"kind"`
}

type instructionEnvelopeDocument struct {
	Source  instruction.Source `json:"source"`
	Path    string             `json:"path"`
	Scope   instruction.Scope  `json:"scope"`
	SHA256  string             `json:"sha256"`
	Content string             `json:"content"`
}

func marshalInstructionEnvelope(resolution instruction.Resolution) (string, error) {
	payload := instructionEnvelope{
		Type:      "amadeus.instructions.v1",
		Target:    instructionTarget{Path: resolution.TargetPath, Kind: resolution.TargetKind},
		Documents: make([]instructionEnvelopeDocument, len(resolution.Documents)),
	}
	for index, document := range resolution.Documents {
		payload.Documents[index] = instructionEnvelopeDocument{
			Source: document.Source, Path: document.Path, Scope: document.Scope,
			SHA256: document.SHA256, Content: document.Content,
		}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode Agent instruction envelope: %w", err)
	}
	return string(encoded), nil
}

func envelopeSources(bundle prompt.Bundle, resolution instruction.Resolution, task string, tools []tool.Spec) []Source {
	sources := make([]Source, 0, 2+len(bundle.Sources)+len(resolution.Documents)+len(tools))
	sources = append(sources, Source{Kind: SourcePromptBundle, ID: "agent", SHA256: bundle.SHA256})
	for _, source := range bundle.Sources {
		sources = append(sources, Source{Kind: SourcePromptLayer, ID: source.Kind + ":" + source.Path, Path: source.Path, SHA256: source.SHA256})
	}
	for _, document := range resolution.Documents {
		sources = append(sources, Source{
			Kind: SourceInstruction, ID: string(document.Source) + ":" + document.Path, Path: document.Path,
			ScopeKind: string(document.Scope.Kind), ScopePath: document.Scope.Path, SHA256: document.SHA256,
		})
	}
	sources = append(sources, Source{Kind: SourceTask, ID: "current", SHA256: contentHash(task)})
	for _, spec := range tools {
		encoded, _ := json.Marshal(spec)
		sources = append(sources, Source{Kind: SourceTool, ID: spec.Name, SHA256: contentHash(string(encoded))})
	}
	return sources
}

func envelopeHash(envelope Envelope) (string, error) {
	payload := struct {
		Messages       []llm.Message `json:"messages"`
		AvailableTools []tool.Spec   `json:"available_tools"`
		Sources        []Source      `json:"sources"`
	}{Messages: envelope.Messages, AvailableTools: envelope.AvailableTools, Sources: envelope.Sources}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode Agent context hash payload: %w", err)
	}
	return contentHash(string(encoded)), nil
}

func canonicalJSON(content json.RawMessage) (json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if value == nil {
		return nil, errors.New("JSON value is null")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return nil, errors.New("multiple JSON values")
	} else if !errors.Is(err, io.EOF) {
		return nil, err
	}
	return json.Marshal(value)
}

func cloneMessages(messages []llm.Message) []llm.Message {
	cloned := make([]llm.Message, len(messages))
	for index, message := range messages {
		cloned[index] = message
		cloned[index].ToolCalls = make([]llm.ToolCall, len(message.ToolCalls))
		for callIndex, call := range message.ToolCalls {
			cloned[index].ToolCalls[callIndex] = call
			cloned[index].ToolCalls[callIndex].Arguments = append(json.RawMessage(nil), call.Arguments...)
		}
	}
	return cloned
}

func cloneSpecs(specs []tool.Spec) []tool.Spec {
	cloned := make([]tool.Spec, len(specs))
	for index, spec := range specs {
		cloned[index] = spec.Clone()
	}
	return cloned
}

func contentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func validHash(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
