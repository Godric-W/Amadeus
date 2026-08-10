package agentcontext

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/instruction"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/prompt"
	"github.com/Godric-W/Amadeus/internal/skill"
	"github.com/Godric-W/Amadeus/internal/tool"
)

func TestBuilderCreatesStableCategorizedEnvelope(t *testing.T) {
	input := testBuildInput(t)
	builder := NewBuilder()
	first, err := builder.Build(context.Background(), input)
	if err != nil {
		t.Fatalf("build Agent context: %v", err)
	}
	input.Tools[0], input.Tools[1] = input.Tools[1], input.Tools[0]
	second, err := builder.Build(context.Background(), input)
	if err != nil {
		t.Fatalf("rebuild Agent context: %v", err)
	}
	if first.SHA256 != second.SHA256 || len(first.SHA256) != 64 {
		t.Fatalf("Agent context hash is unstable: first=%s second=%s", first.SHA256, second.SHA256)
	}
	if len(first.Messages) != 3 || first.Messages[0].Role != llm.RoleSystem || first.Messages[1].Role != llm.RoleDeveloper || first.Messages[2].Role != llm.RoleUser {
		t.Fatalf("unexpected message category order: %#v", first.Messages)
	}
	if first.Messages[0].Content != input.Prompt.Content || first.Messages[2].Content != "Fix the failing test" {
		t.Fatalf("prompt/task content changed: %#v", first.Messages)
	}
	var instructions instructionEnvelope
	if err := json.Unmarshal([]byte(first.Messages[1].Content), &instructions); err != nil {
		t.Fatalf("decode instruction envelope: %v", err)
	}
	if instructions.Type != "amadeus.instructions.v1" || instructions.Target.Path != "pkg/file.go" || len(instructions.Documents) != 3 {
		t.Fatalf("unexpected instruction envelope: %#v", instructions)
	}
	if instructions.Documents[0].Source != instruction.SourceUser || instructions.Documents[1].Scope.Kind != instruction.ScopeProject || instructions.Documents[2].Scope.Path != "pkg" {
		t.Fatalf("instruction priority order changed: %#v", instructions.Documents)
	}
	if got := []string{first.AvailableTools[0].Name, first.AvailableTools[1].Name}; !reflect.DeepEqual(got, []string{"read_file", "write_file"}) {
		t.Fatalf("tools are not stably sorted: %v", got)
	}
	wantKinds := []SourceKind{
		SourcePromptBundle, SourcePromptLayer, SourcePromptLayer,
		SourceInstruction, SourceInstruction, SourceInstruction,
		SourceTask, SourceTool, SourceTool,
	}
	gotKinds := make([]SourceKind, len(first.Sources))
	for index, source := range first.Sources {
		gotKinds[index] = source.Kind
		if len(source.SHA256) != 64 {
			t.Fatalf("source %d has invalid hash: %#v", index, source)
		}
	}
	if !reflect.DeepEqual(gotKinds, wantKinds) {
		t.Fatalf("unexpected source category order: got %v want %v", gotKinds, wantKinds)
	}
}

func TestBuilderProjectsDeveloperPromptSourcesBudgetAndHash(t *testing.T) {
	input := testBuildInput(t)
	developerContent := "dynamic execute and permission context"
	input.DeveloperPrompts = []prompt.NamedBundle{{
		ID: "developer.runtime",
		Bundle: prompt.Bundle{
			Content: developerContent, SHA256: contentHash(developerContent),
			Sources: []prompt.Source{{Kind: prompt.BuiltinSource, Path: "templates/runtime/workspace.md", SHA256: strings.Repeat("c", 64)}},
		},
	}}
	first, err := NewBuilder().Build(context.Background(), input)
	if err != nil {
		t.Fatalf("build context with Developer Prompt: %v", err)
	}
	if len(first.Messages) != 4 || first.Messages[0].Role != llm.RoleSystem || first.Messages[1].Role != llm.RoleDeveloper || first.Messages[1].Content != developerContent || first.Messages[2].Role != llm.RoleDeveloper || first.Messages[3].Role != llm.RoleUser {
		t.Fatalf("unexpected Developer Prompt message order: %#v", first.Messages)
	}
	foundBundle, foundLayer := false, false
	for _, source := range first.Sources {
		if source.Kind == SourcePromptBundle && source.ID == "developer.runtime" && source.SHA256 == input.DeveloperPrompts[0].Bundle.SHA256 {
			foundBundle = true
		}
		if source.Kind == SourcePromptLayer && strings.HasPrefix(source.ID, "developer.runtime:") && source.Path == "templates/runtime/workspace.md" {
			foundLayer = true
		}
	}
	if !foundBundle || !foundLayer {
		t.Fatalf("Developer Prompt sources missing: %#v", first.Sources)
	}
	instructionEnvelopeContent, err := marshalInstructionEnvelope(input.Instructions)
	if err != nil {
		t.Fatal(err)
	}
	estimator := ConservativeEstimator{}
	wantInstructionUsage := estimator.EstimateText(developerContent) + estimator.EstimateText(instructionEnvelopeContent)
	if first.BudgetUsage.Instructions != wantInstructionUsage {
		t.Fatalf("Developer Prompt budget missing: got %d, want %d", first.BudgetUsage.Instructions, wantInstructionUsage)
	}

	changed := input
	changedContent := developerContent + " with grant"
	changed.DeveloperPrompts = []prompt.NamedBundle{{
		ID: "developer.runtime",
		Bundle: prompt.Bundle{
			Content: changedContent, SHA256: contentHash(changedContent),
			Sources: []prompt.Source{{Kind: prompt.BuiltinSource, Path: "templates/runtime/workspace.md", SHA256: strings.Repeat("c", 64)}},
		},
	}}
	second, err := NewBuilder().Build(context.Background(), changed)
	if err != nil {
		t.Fatalf("build changed Developer Prompt context: %v", err)
	}
	if first.SHA256 == second.SHA256 {
		t.Fatal("Envelope hash did not change with Developer Prompt")
	}
}

func TestBuilderRejectsDuplicateDeveloperPromptIDs(t *testing.T) {
	input := testBuildInput(t)
	content := "developer context"
	bundle := prompt.NamedBundle{ID: "developer.runtime", Bundle: prompt.Bundle{
		Content: content, SHA256: contentHash(content),
		Sources: []prompt.Source{{Kind: prompt.BuiltinSource, Path: "runtime.md", SHA256: strings.Repeat("d", 64)}},
	}}
	input.DeveloperPrompts = []prompt.NamedBundle{bundle, bundle}
	if _, err := NewBuilder().Build(context.Background(), input); err == nil || !strings.Contains(err.Error(), "is duplicated") {
		t.Fatalf("unexpected duplicate Developer Prompt error: %v", err)
	}
}

func TestBuilderDoesNotDuplicateTaskAlreadyInCanonicalConversation(t *testing.T) {
	input := testBuildInput(t)
	input.Conversation = []llm.Message{
		llm.UserMessage("Fix the failing test"),
		llm.AssistantToolCallMessage("", llm.ToolCall{ID: "read-1", Name: "read_file", Arguments: json.RawMessage(`{"path":"README.md"}`)}),
		llm.ToolResultMessage("read-1", "contents"),
	}
	input.ConversationSources = []int64{1, 2, 3}
	input.TaskInConversation = true
	envelope, err := NewBuilder().Build(context.Background(), input)
	if err != nil {
		t.Fatalf("build canonical active Run context: %v", err)
	}
	if len(envelope.Messages) != 5 {
		t.Fatalf("current task was duplicated: %#v", envelope.Messages)
	}
	if envelope.Messages[2].Role != llm.RoleUser || envelope.Messages[3].Role != llm.RoleAssistant || envelope.Messages[4].Role != llm.RoleTool {
		t.Fatalf("canonical Tool protocol order changed: %#v", envelope.Messages)
	}
}

func TestBuilderHashTracksSemanticInputs(t *testing.T) {
	builder := NewBuilder()
	base := testBuildInput(t)
	first, err := builder.Build(context.Background(), base)
	if err != nil {
		t.Fatalf("build base context: %v", err)
	}

	changedTask := base
	changedTask.Task = "Fix a different test"
	second, err := builder.Build(context.Background(), changedTask)
	if err != nil {
		t.Fatalf("build changed task context: %v", err)
	}
	if first.SHA256 == second.SHA256 {
		t.Fatal("task change did not change context hash")
	}

	changedInstructions := testBuildInput(t)
	changedInstructions.Instructions.Documents[0].Content = "changed"
	if _, err := builder.Build(context.Background(), changedInstructions); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("tampered instruction was accepted: %v", err)
	}
}

func TestBuilderAddsSkillIndexWithoutSkillContent(t *testing.T) {
	input := testBuildInput(t)
	input.SkillIndex = []skill.IndexEntry{{Name: "review", Description: "Review changed code", Source: skill.SourceProject}}
	envelope, err := NewBuilder().Build(context.Background(), input)
	if err != nil {
		t.Fatalf("build Skill index context: %v", err)
	}
	if len(envelope.Messages) != 4 || envelope.Messages[2].Role != llm.RoleDeveloper {
		t.Fatalf("unexpected Skill index message placement: %#v", envelope.Messages)
	}
	if !strings.Contains(envelope.Messages[2].Content, `"type":"amadeus.skill_index.v1"`) || !strings.Contains(envelope.Messages[2].Content, `"name":"review"`) {
		t.Fatalf("Skill index message is missing metadata: %s", envelope.Messages[2].Content)
	}
	if strings.Contains(envelope.Messages[2].Content, "Use concise findings") {
		t.Fatalf("Skill index must not contain Skill body: %s", envelope.Messages[2].Content)
	}
	if envelope.Sources[len(envelope.Sources)-1].Kind != SourceSkill {
		t.Fatalf("Skill index source was not recorded: %#v", envelope.Sources)
	}
}

func TestEnvelopeCloneIsIndependent(t *testing.T) {
	envelope, err := NewBuilder().Build(context.Background(), testBuildInput(t))
	if err != nil {
		t.Fatalf("build Agent context: %v", err)
	}
	clone := envelope.Clone()
	clone.Messages[0].Content = "changed"
	clone.AvailableTools[0].InputSchema[0] = '['
	clone.Sources[0].ID = "changed"
	if envelope.Messages[0].Content == "changed" || envelope.AvailableTools[0].InputSchema[0] == '[' || envelope.Sources[0].ID == "changed" {
		t.Fatal("Agent context clone shares mutable storage")
	}
}

func TestBuilderValidatesInputsAndCancellation(t *testing.T) {
	builder := NewBuilder()
	tests := []struct {
		name     string
		mutate   func(*BuildInput)
		contains string
	}{
		{name: "empty prompt", mutate: func(input *BuildInput) { input.Prompt.Content = "" }, contains: "Prompt bundle content is empty"},
		{name: "prompt hash mismatch", mutate: func(input *BuildInput) { input.Prompt.SHA256 = strings.Repeat("0", 64) }, contains: "Prompt bundle SHA-256"},
		{name: "empty task", mutate: func(input *BuildInput) { input.Task = " \n" }, contains: "task is empty"},
		{name: "duplicate tool", mutate: func(input *BuildInput) { input.Tools[1] = input.Tools[0].Clone() }, contains: "duplicated"},
		{name: "invalid tool schema", mutate: func(input *BuildInput) { input.Tools[0].InputSchema = json.RawMessage(`{`) }, contains: "schema"},
		{name: "resolution mismatch", mutate: func(input *BuildInput) { input.Instructions.TargetPath = "." }, contains: "does not match"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := testBuildInput(t)
			test.mutate(&input)
			if envelope, err := builder.Build(context.Background(), input); err == nil || !strings.Contains(err.Error(), test.contains) || !reflect.DeepEqual(envelope, Envelope{}) {
				t.Fatalf("unexpected invalid build result: envelope=%#v err=%v", envelope, err)
			}
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if envelope, err := builder.Build(ctx, testBuildInput(t)); !errors.Is(err, context.Canceled) || !reflect.DeepEqual(envelope, Envelope{}) {
		t.Fatalf("unexpected canceled build: envelope=%#v err=%v", envelope, err)
	}
	if envelope, err := builder.Build(nil, testBuildInput(t)); err == nil || !reflect.DeepEqual(envelope, Envelope{}) {
		t.Fatalf("unexpected nil-context build: envelope=%#v err=%v", envelope, err)
	}
	var nilBuilder *Builder
	if envelope, err := nilBuilder.Build(context.Background(), testBuildInput(t)); err == nil || !reflect.DeepEqual(envelope, Envelope{}) {
		t.Fatalf("unexpected nil-builder result: envelope=%#v err=%v", envelope, err)
	}
}

func testBuildInput(t *testing.T) BuildInput {
	t.Helper()
	root, err := project.NewRoot(t.TempDir())
	if err != nil {
		t.Fatalf("create context project root: %v", err)
	}
	request, err := instruction.NewResolveRequest(root, "pkg/file.go", instruction.TargetFile)
	if err != nil {
		t.Fatalf("create instruction request: %v", err)
	}
	directoryScope, err := instruction.NewDirectoryScope("pkg")
	if err != nil {
		t.Fatalf("create directory scope: %v", err)
	}
	userDocument := mustContextInstruction(t, instruction.SourceUser, filepath.Join(t.TempDir(), "AGENTS.md"), instruction.UserScope(), "user rules")
	projectDocument := mustContextInstruction(t, instruction.SourceProject, filepath.Join(root.Path(), "AGENTS.md"), instruction.ProjectScope(), "project rules")
	directoryDocument := mustContextInstruction(t, instruction.SourceProject, filepath.Join(root.Path(), "pkg", "AGENTS.md"), directoryScope, "package rules")
	promptContent := "system protocol"
	return BuildInput{
		Prompt: prompt.Bundle{
			Content: promptContent, SHA256: contentHash(promptContent),
			Sources: []prompt.Source{
				{Kind: prompt.BuiltinSource, Path: "base.md", SHA256: strings.Repeat("a", 64)},
				{Kind: prompt.BuiltinSource, Path: "tools.md", SHA256: strings.Repeat("b", 64)},
			},
		},
		InstructionRequest: request,
		Instructions: instruction.Resolution{
			TargetPath: request.TargetPath, TargetKind: request.TargetKind,
			Documents: []instruction.InstructionDocument{userDocument, projectDocument, directoryDocument},
		},
		Task:  "  Fix the failing test  ",
		Tools: []tool.Spec{contextToolSpec("write_file", tool.SideEffectWrite), contextToolSpec("read_file", tool.SideEffectRead)},
	}
}

func mustContextInstruction(t *testing.T, source instruction.Source, path string, scope instruction.Scope, content string) instruction.InstructionDocument {
	t.Helper()
	document, err := instruction.NewInstructionDocument(source, path, scope, content)
	if err != nil {
		t.Fatalf("create context instruction: %v", err)
	}
	return document
}

func contextToolSpec(name string, sideEffect tool.SideEffect) tool.Spec {
	spec := tool.Spec{
		Name: name, Description: name + " test tool", InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
		SideEffect: sideEffect, Idempotent: true,
	}
	return spec
}

func TestBuilderCompactsHistoryWithinBudgetAndPreservesReplacementSource(t *testing.T) {
	input := testBuildInput(t)
	input.Conversation = []llm.Message{
		llm.UserMessage("first request with enough historical detail to require compaction"),
		llm.AssistantMessage("first answer with implementation details and test observations"),
		llm.UserMessage("second request with more historical detail"),
		llm.AssistantMessage("second answer with more implementation details"),
		llm.UserMessage("latest request"),
	}
	input.Budget = Budget{System: 100, Instructions: 100, History: 30, Interrupted: 40, Tools: 100, Resources: 10}
	envelope, err := NewBuilder().Build(context.Background(), input)
	if err != nil {
		t.Fatalf("build compacted context: %v", err)
	}
	if envelope.Compaction == nil || envelope.Compaction.CoveredMessages == 0 {
		t.Fatalf("expected compaction: %#v", envelope.Compaction)
	}
	if len(envelope.Messages) >= len(input.Conversation)+3 {
		t.Fatalf("history was not compacted: got %d messages", len(envelope.Messages))
	}
	foundReplacement := false
	for _, source := range envelope.Sources {
		if source.Kind == SourceReplacement {
			foundReplacement = true
		}
	}
	if !foundReplacement {
		t.Fatal("compacted context did not include replacement history source")
	}
	if envelope.BudgetUsage.History <= 0 || envelope.Sources == nil {
		t.Fatalf("missing budget/source accounting: %#v", envelope)
	}
}

func TestBuilderAcceptsCanonicalToolProtocolAndInterruptedMarker(t *testing.T) {
	input := testBuildInput(t)
	input.Conversation = []llm.Message{
		llm.UserMessage("inspect"),
		llm.AssistantToolCallMessage("", llm.ToolCall{ID: "call-1", Name: "read_file", Arguments: json.RawMessage(`{"path":"README.md"}`)}),
		llm.ToolResultMessage("call-1", `{"ok":true,"text":"contents"}`),
		llm.DeveloperMessage("Previous Run interrupted: user cancelled. Re-plan from the current workspace state."),
	}
	envelope, err := NewBuilder().Build(context.Background(), input)
	if err != nil {
		t.Fatalf("build canonical history: %v", err)
	}
	if len(envelope.Messages) < len(input.Conversation)+3 {
		t.Fatalf("canonical history was not preserved: %#v", envelope.Messages)
	}
}

func TestBuilderPlacesCompactionReplacementBeforeRetainedTail(t *testing.T) {
	input := testBuildInput(t)
	input.Budget = DefaultBudget(4000)
	input.Budget.History = 300
	input.Conversation = []llm.Message{
		llm.UserMessage("old request " + strings.Repeat("history ", 500)),
		llm.AssistantMessage("old response " + strings.Repeat("detail ", 500)),
		llm.UserMessage("recent request"),
		llm.AssistantMessage("recent response"),
	}
	input.ConversationSources = []int64{1, 2, 3, 4}
	envelope, err := NewBuilder().Build(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if envelope.Compaction == nil {
		t.Fatal("expected compaction")
	}
	replacementIndex := -1
	recentIndex := -1
	for index, message := range envelope.Messages {
		if message.Role == llm.RoleAssistant && message.Content == envelope.Compaction.ReplacementHistory[0].Content {
			replacementIndex = index
		}
		if message.Role == llm.RoleUser && message.Content == "recent request" {
			recentIndex = index
		}
	}
	if replacementIndex < 0 || recentIndex < 0 || replacementIndex >= recentIndex {
		t.Fatalf("replacement history must precede retained tail: %#v", envelope.Messages)
	}
}

func TestBuilderInjectsExplicitSkillWithSourceAndHash(t *testing.T) {
	input := testBuildInput(t)
	content := "Follow the review workflow."
	input.SkillInjections = []SkillInjection{{Name: "review", Content: content, ContentHash: contentHash(content), Source: skill.SourceProject}}
	envelope, err := NewBuilder().Build(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	foundMessage, foundSource := false, false
	for _, message := range envelope.Messages {
		if message.Role == llm.RoleDeveloper && strings.Contains(message.Content, "amadeus.skill_injection.v1") && strings.Contains(message.Content, content) {
			foundMessage = true
		}
	}
	for _, source := range envelope.Sources {
		if source.Kind == SourceSkillInject && source.ID == "review" && source.SHA256 == contentHash(content) {
			foundSource = true
		}
	}
	if !foundMessage || !foundSource || envelope.BudgetUsage.Resources == 0 {
		t.Fatalf("explicit Skill injection was not fully projected: message=%v source=%v envelope=%#v", foundMessage, foundSource, envelope)
	}
}
