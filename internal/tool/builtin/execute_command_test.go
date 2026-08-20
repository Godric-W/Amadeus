package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/policy"
	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/skill"
	"github.com/Godric-W/Amadeus/internal/tool"
)

func TestExecuteCommandApprovalUsesClearClaudeStylePresentation(t *testing.T) {
	rootPath := t.TempDir()
	executeCommand := newTestExecuteCommand(t, rootPath, 5*time.Second, 1024, 100)
	approvalPort := &testApprovalPort{decision: policy.ApprovalDecision{
		Outcome: policy.ApprovalAllow, Scope: policy.ApprovalOnce, Source: policy.ApprovalSourceUser, Reason: "test approved once",
	}}
	coordinator, err := policy.NewApprovalCoordinator(approvalPort)
	if err != nil {
		t.Fatal(err)
	}
	ctx := withTestApprovalCoordinator(context.Background(), coordinator)
	if _, err := executePreparedTool(t, ctx, executeCommand, json.RawMessage(`{"command":"printf ok","description":"Print a success marker"}`)); err != nil {
		t.Fatalf("execute command: %v", err)
	}
	if len(approvalPort.requests) != 1 {
		t.Fatalf("unexpected approval requests: %#v", approvalPort.requests)
	}
	request := approvalPort.requests[0]
	if request.Cause.Kind != policy.ApprovalCauseCommand || request.Cause.Code != "host_command" {
		t.Fatalf("unexpected command approval cause: %#v", request.Cause)
	}
	if request.Presentation.Title != "Bash command" || request.Presentation.Question != "Do you want to proceed?" {
		t.Fatalf("unexpected command approval presentation: %#v", request.Presentation)
	}
	if !strings.Contains(strings.Join(request.Presentation.Details, "\n"), "Description: Print a success marker") {
		t.Fatalf("unexpected command approval details: %#v", request.Presentation.Details)
	}
	text := request.Presentation.Title + " " + request.Presentation.Question + " " + strings.Join(request.Presentation.Details, " ")
	for _, option := range request.Presentation.Options {
		text += " " + option.Label + " " + option.Description
	}
	if strings.Contains(strings.ToLower(text), "unsandbox") || !strings.Contains(text, "this exact command during this session") {
		t.Fatalf("unclear command approval presentation: %q", text)
	}
}

func TestExecuteCommandUsesFixedProjectCWDAndCombinedOutput(t *testing.T) {
	rootPath := t.TempDir()
	if err := os.Mkdir(filepath.Join(rootPath, "sub"), 0o755); err != nil {
		t.Fatalf("create subdirectory: %v", err)
	}
	executeCommand := newTestExecuteCommand(t, rootPath, 5*time.Second, 1024, 100)
	result, err := executePreparedTool(t, context.Background(), executeCommand, json.RawMessage(`{"command":"printf 'out\\n'; printf 'err\\n' >&2; pwd","cwd":"sub"}`))
	if err != nil {
		t.Fatalf("execute command: %v", err)
	}
	if !strings.HasPrefix(result.Text, "out\nerr\n") || !strings.Contains(result.Text, filepath.Join(rootPath, "sub")) || result.Metadata["exit_code"] != 0 {
		t.Fatalf("unexpected command result: %#v", result)
	}
}

func TestExecuteCommandPreparesCanonicalInstructionTarget(t *testing.T) {
	rootPath := t.TempDir()
	subdirectory := filepath.Join(rootPath, "sub")
	if err := os.Mkdir(subdirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	executeCommand := newTestExecuteCommand(t, rootPath, 5*time.Second, 1024, 100)
	call := tool.NewCall("target", "execute_command", json.RawMessage(`{"command":"pwd","cwd":"sub"}`))
	invocation := tool.Invocation{Call: call, Source: tool.ToolCallSourceModel}
	toolContext := tool.ToolUseContext{Context: context.Background(), Invocation: invocation}
	prepared, err := executeCommand.Prepare(toolContext, invocation)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Target == nil || prepared.Target.Path != subdirectory || prepared.Target.Kind != tool.ContextTargetCommandCWD || prepared.Target.SideEffect != tool.SideEffectExecute {
		t.Fatalf("command context target = %#v", prepared.Target)
	}
}

func TestExecuteCommandReturnsOutputWithExitError(t *testing.T) {
	executeCommand := newTestExecuteCommand(t, t.TempDir(), 5*time.Second, 1024, 100)
	result, err := executePreparedTool(t, context.Background(), executeCommand, json.RawMessage(`{"command":"printf failure; exit 7"}`))
	var exitError *CommandExitError
	if !errors.As(err, &exitError) || exitError.ExitCode != 7 || result.Text != "failure" || result.Metadata["exit_code"] != 7 {
		t.Fatalf("unexpected exit failure: result=%#v err=%v", result, err)
	}
}

func TestExecuteCommandTimeoutAndCancellationArePartial(t *testing.T) {
	executeCommand := newTestExecuteCommand(t, t.TempDir(), 2*time.Second, 1024, 100)
	startedAt := time.Now()
	result, err := executePreparedTool(t, context.Background(), executeCommand, json.RawMessage(`{"command":"printf started; sleep 5","timeout_ms":30}`))
	if !errors.Is(err, ErrCommandTimeout) || !result.Partial || result.Metadata["timed_out"] != true || time.Since(startedAt) > time.Second {
		t.Fatalf("unexpected timeout result: result=%#v err=%v elapsed=%s", result, err, time.Since(startedAt))
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err = executePreparedTool(t, ctx, executeCommand, json.RawMessage(`{"command":"printf ignored"}`))
	if !errors.Is(err, context.Canceled) || result.CallID != "test-call" || result.ToolName != "execute_command" || result.Text != context.Canceled.Error() {
		t.Fatalf("unexpected pre-cancel result: result=%#v err=%v", result, err)
	}
}

func TestExecuteCommandAppliesByteAndLineOutputBudget(t *testing.T) {
	executeCommand := newTestExecuteCommand(t, t.TempDir(), 5*time.Second, 8, 2)
	result, err := executePreparedTool(t, context.Background(), executeCommand, json.RawMessage(`{"command":"printf 'one\\ntwo\\nthree\\n'"}`))
	if err != nil {
		t.Fatalf("execute bounded command: %v", err)
	}
	if !result.Partial || !strings.Contains(result.Text, "one\n") || !strings.Contains(result.Text, "ree\n") || !strings.Contains(result.Text, "output truncated") || result.Metadata["output_bytes"] != int64(14) || result.Metadata["output_truncated"] != true {
		t.Fatalf("unexpected bounded output: %#v", result)
	}
}

func TestExecuteCommandAllowsReadableExternalCWD(t *testing.T) {
	rootPath := t.TempDir()
	root, err := project.NewRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	filesystemPolicy, err := project.NewFileSystemPolicy(project.FileSystemPolicyOptions{CWD: rootPath, Profile: project.PermissionProfile{ReadHost: true, WorkspaceRoots: []string{rootPath}}})
	if err != nil {
		t.Fatal(err)
	}
	executeCommand, err := NewExecuteCommand(root, ExecuteCommandOptions{DefaultTimeout: 5 * time.Second, MaxTimeout: 5 * time.Second, MaxOutputBytes: 1024, MaxOutputLines: 100, FileSystemPolicy: filesystemPolicy})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := executePreparedTool(t, context.Background(), executeCommand, json.RawMessage(`{"command":"pwd","cwd":".."}`)); err != nil {
		t.Fatalf("external readable cwd rejected: %v", err)
	}
}

func TestExecuteCommandAttributesSkillScriptWithoutChangingApproval(t *testing.T) {
	rootPath := t.TempDir()
	skillRoot := filepath.Join(rootPath, ".amadeus", "skills", "review")
	scriptPath := filepath.Join(skillRoot, "scripts", "check.sh")
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillRoot, "SKILL.md"), []byte("---\nname: review\ndescription: review code\n---\nReview workflow\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scriptPath, []byte("printf skill-script\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := project.NewRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	catalog, warnings, err := skill.Load("", root, skill.DefaultLoadOptions())
	if err != nil || len(warnings) != 0 {
		t.Fatalf("load skill catalog: warnings=%v err=%v", warnings, err)
	}
	executeCommand, err := NewExecuteCommand(root, ExecuteCommandOptions{DefaultTimeout: 5 * time.Second, MaxTimeout: 5 * time.Second, MaxOutputBytes: 1024, MaxOutputLines: 100, SkillCatalog: catalog})
	if err != nil {
		t.Fatal(err)
	}
	result, err := executePreparedTool(t, context.Background(), executeCommand, json.RawMessage(`{"command":"bash .amadeus/skills/review/scripts/check.sh"}`))
	if err != nil || result.Metadata["skill_name"] != "review" || result.Metadata["skill_script"] != "scripts/check.sh" || result.Metadata["skill_revision"] == "" {
		t.Fatalf("skill script attribution failed: result=%#v err=%v", result, err)
	}
}

func TestWriteStdinPreservesSkillScriptAttribution(t *testing.T) {
	rootPath := t.TempDir()
	skillRoot := filepath.Join(rootPath, ".amadeus", "skills", "review")
	scriptPath := filepath.Join(skillRoot, "scripts", "check.sh")
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillRoot, "SKILL.md"), []byte("---\nname: review\ndescription: review code\n---\nReview workflow\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scriptPath, []byte("read value; printf 'got:%s' \"$value\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := project.NewRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	catalog, _, err := skill.Load("", root, skill.DefaultLoadOptions())
	if err != nil {
		t.Fatal(err)
	}
	executeCommand, err := NewExecuteCommand(root, ExecuteCommandOptions{
		DefaultTimeout: 5 * time.Second, MaxTimeout: 5 * time.Second, DefaultYield: 10 * time.Millisecond,
		MaxYield: 100 * time.Millisecond, MaxOutputBytes: 1024, MaxOutputLines: 100, SkillCatalog: catalog,
	})
	if err != nil {
		t.Fatal(err)
	}
	writeStdin, err := NewWriteStdin(WriteStdinOptions{Manager: executeCommand.ProcessManager(), DefaultYield: 10 * time.Millisecond, MaxYield: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	executeCall := tool.NewCall("call-1", "execute_command", json.RawMessage(`{"command":"bash .amadeus/skills/review/scripts/check.sh","yield_time_ms":1}`))
	executeInvocation := tool.Invocation{TurnID: "turn-1", Call: executeCall, Source: tool.ToolCallSourceModel}
	prepared, err := executeCommand.Prepare(tool.ToolUseContext{Context: context.Background(), Invocation: executeInvocation}, executeInvocation)
	if err != nil {
		t.Fatal(err)
	}
	first, err := executeCommand.Execute(tool.ToolUseContext{Context: context.Background(), Invocation: executeInvocation}, prepared)
	if err != nil && !errors.Is(err, ErrCommandTimeout) {
		t.Fatal(err)
	}
	processResult, ok := first.Data.(ProcessResult)
	if !ok || processResult.ProcessID == "" {
		t.Fatalf("execute_command did not return process state: %#v err=%v", first, err)
	}
	stdinCall := tool.NewCall("call-2", "write_stdin", json.RawMessage(`{"process_id":"`+processResult.ProcessID+`","origin_call_id":"call-1","chars":"hello\n","yield_time_ms":100}`))
	stdinInvocation := tool.Invocation{TurnID: "turn-1", Call: stdinCall, Source: tool.ToolCallSourceModel}
	stdinPrepared, err := writeStdin.Prepare(tool.ToolUseContext{Context: context.Background(), Invocation: stdinInvocation}, stdinInvocation)
	if err != nil {
		t.Fatal(err)
	}
	continued, err := writeStdin.Execute(tool.ToolUseContext{Context: context.Background(), Invocation: stdinInvocation}, stdinPrepared)
	if err != nil {
		t.Fatal(err)
	}
	if continued.Metadata["skill_name"] != "review" || continued.Metadata["skill_script"] != "scripts/check.sh" || continued.Metadata["skill_revision"] == "" || !strings.Contains(continued.Text, "got:hello") {
		t.Fatalf("write_stdin lost Skill attribution: result=%#v", continued)
	}
}

func newTestExecuteCommand(t *testing.T, rootPath string, maxTimeout time.Duration, maxBytes int64, maxLines int) *ExecuteCommand {
	t.Helper()
	root, err := project.NewRoot(rootPath)
	if err != nil {
		t.Fatalf("create project root: %v", err)
	}
	executeCommand, err := NewExecuteCommand(root, ExecuteCommandOptions{
		DefaultTimeout: maxTimeout, MaxTimeout: maxTimeout, MaxOutputBytes: maxBytes, MaxOutputLines: maxLines,
	})
	if err != nil {
		t.Fatalf("create execute_command: %v", err)
	}
	return executeCommand
}
