package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type SideEffect string

const (
	SideEffectNone    SideEffect = "none"
	SideEffectRead    SideEffect = "read"
	SideEffectWrite   SideEffect = "write"
	SideEffectExecute SideEffect = "execute"
	SideEffectNetwork SideEffect = "network"
)

func (effect SideEffect) Valid() bool {
	switch effect {
	case SideEffectNone, SideEffectRead, SideEffectWrite, SideEffectExecute, SideEffectNetwork:
		return true
	default:
		return false
	}
}

type Exposure string

const (
	ExposureDirect      Exposure = "direct"
	ExposureConditional Exposure = "conditional"
	ExposureDeferred    Exposure = "deferred"
	ExposureHidden      Exposure = "hidden"
)

func (exposure Exposure) Valid() bool {
	switch exposure {
	case ExposureDirect, ExposureConditional, ExposureDeferred, ExposureHidden:
		return true
	default:
		return false
	}
}

type Registration struct {
	Exposure  Exposure
	Condition string
}

func DirectRegistration() Registration {
	return Registration{Exposure: ExposureDirect}
}

type ToolConcurrency string

const (
	ToolConcurrencyShared    ToolConcurrency = "shared"
	ToolConcurrencyExclusive ToolConcurrency = "exclusive"
)

func (concurrency ToolConcurrency) Valid() bool {
	switch concurrency {
	case ToolConcurrencyShared, ToolConcurrencyExclusive:
		return true
	default:
		return false
	}
}

type Spec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
	SideEffect  SideEffect      `json:"side_effect"`
	Concurrency ToolConcurrency `json:"concurrency"`
	Idempotent  bool            `json:"idempotent"`
}

func (spec Spec) Clone() Spec {
	spec.InputSchema = append(json.RawMessage(nil), spec.InputSchema...)
	return spec
}

type Call struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type RequestSnapshot struct {
	MCPBindingRevision string
	SkillRevision      string
	ToolRevision       string
}

type requestSnapshotContextKey struct{}

func WithRequestSnapshot(ctx context.Context, snapshot RequestSnapshot) context.Context {
	return context.WithValue(ctx, requestSnapshotContextKey{}, snapshot)
}

func RequestSnapshotFromContext(ctx context.Context) (RequestSnapshot, bool) {
	if ctx == nil {
		return RequestSnapshot{}, false
	}
	snapshot, ok := ctx.Value(requestSnapshotContextKey{}).(RequestSnapshot)
	return snapshot, ok
}

func NewCall(id, name string, arguments json.RawMessage) Call {
	return Call{
		ID:        strings.TrimSpace(id),
		Name:      strings.TrimSpace(name),
		Arguments: append(json.RawMessage(nil), arguments...),
	}
}

func (call Call) Clone() Call {
	call.Arguments = append(json.RawMessage(nil), call.Arguments...)
	return call
}

type TargetKind string

const (
	TargetFilesystem TargetKind = "filesystem"
	TargetProcess    TargetKind = "process"
	TargetMCP        TargetKind = "mcp"
	TargetWeb        TargetKind = "web"
	TargetSkill      TargetKind = "skill"
)

type TargetAccess string

const (
	TargetAccessRead    TargetAccess = "read"
	TargetAccessWrite   TargetAccess = "write"
	TargetAccessExecute TargetAccess = "execute"
	TargetAccessNetwork TargetAccess = "network"
)

type PreparedTarget struct {
	Kind          TargetKind   `json:"kind"`
	RequestedPath string       `json:"requested_path,omitempty"`
	CanonicalPath string       `json:"canonical_path,omitempty"`
	Access        TargetAccess `json:"access"`
	MatchedRoot   string       `json:"matched_root,omitempty"`
	RootSource    string       `json:"root_source,omitempty"`
	Identity      string       `json:"identity,omitempty"`
}

type PreparedOptions struct {
	Targets       []PreparedTarget
	Command       string
	Shell         string
	CWD           string
	TTY           bool
	IsolationMode string
	Payload       any
}

type PreparedCall struct {
	call          Call
	targets       []PreparedTarget
	command       string
	shell         string
	cwd           string
	tty           bool
	isolationMode string
	payload       any
}

func NewPreparedCall(call Call, options PreparedOptions) (PreparedCall, error) {
	if strings.TrimSpace(call.ID) == "" {
		return PreparedCall{}, errors.New("prepared tool call ID is empty")
	}
	if strings.TrimSpace(call.Name) == "" {
		return PreparedCall{}, errors.New("prepared tool call name is empty")
	}
	targets := append([]PreparedTarget(nil), options.Targets...)
	for index := range targets {
		if targets[index].Kind == "" || targets[index].Access == "" {
			return PreparedCall{}, fmt.Errorf("prepared target %d is incomplete", index)
		}
		if targets[index].Kind == TargetFilesystem && strings.TrimSpace(targets[index].CanonicalPath) == "" {
			return PreparedCall{}, fmt.Errorf("prepared filesystem target %d has no canonical path", index)
		}
	}
	return PreparedCall{
		call: call.Clone(), targets: targets, command: options.Command,
		shell: strings.TrimSpace(options.Shell), cwd: strings.TrimSpace(options.CWD), tty: options.TTY,
		isolationMode: strings.TrimSpace(options.IsolationMode), payload: options.Payload,
	}, nil
}

func PreparePassthrough(call Call, payload any) (PreparedCall, error) {
	return NewPreparedCall(call, PreparedOptions{Payload: payload})
}

func (prepared PreparedCall) Call() Call { return prepared.call.Clone() }

func (prepared PreparedCall) Targets() []PreparedTarget {
	return append([]PreparedTarget(nil), prepared.targets...)
}

func (prepared PreparedCall) Command() string       { return prepared.command }
func (prepared PreparedCall) Shell() string         { return prepared.shell }
func (prepared PreparedCall) CWD() string           { return prepared.cwd }
func (prepared PreparedCall) TTY() bool             { return prepared.tty }
func (prepared PreparedCall) IsolationMode() string { return prepared.isolationMode }

func PreparedPayloadAs[T any](prepared PreparedCall) (T, bool) {
	value, ok := prepared.payload.(T)
	return value, ok
}

type ContentKind string

const (
	ContentText  ContentKind = "text"
	ContentImage ContentKind = "image"
)

type ContentPart struct {
	Kind      ContentKind `json:"kind"`
	Text      string      `json:"text,omitempty"`
	MediaType string      `json:"media_type,omitempty"`
	Data      string      `json:"data,omitempty"`
}

type Result struct {
	CallID   string         `json:"call_id"`
	ToolName string         `json:"tool_name"`
	Text     string         `json:"text,omitempty"`
	Parts    []ContentPart  `json:"parts,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Partial  bool           `json:"partial,omitempty"`
}

type ErrorKindProvider interface {
	ToolErrorKind() string
}

func (result Result) Clone() Result {
	result.Parts = append([]ContentPart(nil), result.Parts...)
	if result.Metadata != nil {
		result.Metadata = cloneMetadata(result.Metadata)
	}
	return result
}

func cloneMetadata(metadata map[string]any) map[string]any {
	cloned := make(map[string]any, len(metadata))
	for key, value := range metadata {
		cloned[key] = value
	}
	return cloned
}
