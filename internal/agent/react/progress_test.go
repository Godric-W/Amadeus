package react

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/Godric-W/Amadeus/internal/tool"
)

func TestProgressMonitorDetectsRepeatedCanonicalAction(t *testing.T) {
	monitor := DefaultProgressMonitor()
	first := ProgressSample{Calls: []tool.Call{tool.NewCall("call_1", "read_file", json.RawMessage(`{"path":"README.md","offset":0}`))}}
	second := ProgressSample{Calls: []tool.Call{tool.NewCall("call_2", "read_file", json.RawMessage(`{"offset":0,"path":"README.md"}`))}}
	if signals, err := monitor.Observe(first); err != nil || hasSignal(signals, ProgressRepeatedAction) {
		t.Fatalf("unexpected first action signals=%#v err=%v", signals, err)
	}
	signals, err := monitor.Observe(second)
	if err != nil {
		t.Fatalf("observe repeated action: %v", err)
	}
	if !hasSignal(signals, ProgressRepeatedAction) {
		t.Fatalf("repeated canonical action was not detected: %#v", signals)
	}
}

func TestProgressMonitorDetectsRepeatedErrorAndOutcome(t *testing.T) {
	monitor := DefaultProgressMonitor()
	sample := ProgressSample{Outcomes: []ToolOutcome{{CallID: "call", ToolName: "read_file", Status: ToolOutcomeFailed, Error: &ToolError{Kind: "execution_failed", Message: " Permission   Denied "}}}}
	if signals, err := monitor.Observe(sample); err != nil || len(signals) != 0 {
		t.Fatalf("unexpected first failure signals=%#v err=%v", signals, err)
	}
	sample.Outcomes[0].Error.Message = "permission denied"
	signals, err := monitor.Observe(sample)
	if err != nil {
		t.Fatalf("observe repeated failure: %v", err)
	}
	if !hasSignal(signals, ProgressRepeatedError) || !hasSignal(signals, ProgressRepeatedOutcome) {
		t.Fatalf("expected repeated error and outcome signals: %#v", signals)
	}
}

func TestProgressMonitorDoesNotTreatDifferentCallsWithSameOutputAsRepeated(t *testing.T) {
	monitor := DefaultProgressMonitor()
	first := ProgressSample{
		Calls:    []tool.Call{tool.NewCall("first", "read_file", json.RawMessage(`{"path":"a.go"}`))},
		Outcomes: []ToolOutcome{{CallID: "first", ToolName: "read_file", Status: ToolOutcomeSucceeded, Result: tool.Result{Text: "same"}}},
	}
	second := ProgressSample{
		Calls:    []tool.Call{tool.NewCall("second", "read_file", json.RawMessage(`{"path":"b.go"}`))},
		Outcomes: []ToolOutcome{{CallID: "second", ToolName: "read_file", Status: ToolOutcomeSucceeded, Result: tool.Result{Text: "same"}}},
	}
	if _, err := monitor.Observe(first); err != nil {
		t.Fatal(err)
	}
	signals, err := monitor.Observe(second)
	if err != nil {
		t.Fatal(err)
	}
	if hasSignal(signals, ProgressRepeatedOutcome) {
		t.Fatalf("different calls were treated as a repeated outcome: %#v", signals)
	}
}

func TestProgressMonitorAllowsPermissionRequiredCallToBeRetried(t *testing.T) {
	monitor := DefaultProgressMonitor()
	first := tool.NewCall("patch-1", "apply_patch", json.RawMessage(`{"patch":"same"}`))
	signals, err := monitor.Observe(ProgressSample{
		Calls: []tool.Call{first},
		Outcomes: []ToolOutcome{{
			CallID: first.ID, ToolName: first.Name, Status: ToolOutcomeDenied,
			Error: &ToolError{Kind: "permission_required", Message: "request write access"},
		}},
	})
	if err != nil || hasSignal(signals, ProgressRepeatedAction) {
		t.Fatalf("permission preflight counted as an attempted action: signals=%#v err=%v", signals, err)
	}
	if _, err := monitor.Observe(ProgressSample{Calls: []tool.Call{tool.NewCall("grant-1", "request_permissions", json.RawMessage(`{"writable_roots":["/outside"]}`))}}); err != nil {
		t.Fatal(err)
	}
	retry := tool.NewCall("patch-2", "apply_patch", json.RawMessage(`{"patch":"same"}`))
	signals, err = monitor.Observe(ProgressSample{
		Calls:    []tool.Call{retry},
		Outcomes: []ToolOutcome{{CallID: retry.ID, ToolName: retry.Name, Status: ToolOutcomeSucceeded, Result: tool.Result{Text: "applied"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if hasSignal(signals, ProgressRepeatedAction) {
		t.Fatalf("authorized retry was treated as a repeated action: %#v", signals)
	}
}

func TestProgressMonitorDetectsHighImpactCalls(t *testing.T) {
	monitor := DefaultProgressMonitor()
	call := tool.NewCall("call_write", "write_file", json.RawMessage(`{"path":"main.go"}`))
	signals, err := monitor.Observe(ProgressSample{
		Calls: []tool.Call{call},
		Specs: []tool.Spec{{Name: "write_file", SideEffect: tool.SideEffectWrite, Concurrency: tool.ToolConcurrencyExclusive}},
	})
	if err != nil {
		t.Fatalf("observe high-impact call: %v", err)
	}
	if !hasSignal(signals, ProgressHighImpact) {
		t.Fatalf("high-impact call was not detected: %#v", signals)
	}
}

func TestProgressMonitorIsConcurrentSafe(t *testing.T) {
	monitor := DefaultProgressMonitor()
	var wait sync.WaitGroup
	for index := 0; index < 32; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, _ = monitor.Observe(ProgressSample{Calls: []tool.Call{tool.NewCall("call", "read_file", json.RawMessage(`{"path":"README.md"}`))}})
		}()
	}
	wait.Wait()
	monitor.mutex.Lock()
	count := monitor.actionCounts[`read_file:{"path":"README.md"}`]
	monitor.mutex.Unlock()
	if count != 32 {
		t.Fatalf("unexpected concurrent action count: %d", count)
	}
}

func hasSignal(signals []ProgressSignal, kind ProgressSignalKind) bool {
	for _, signal := range signals {
		if signal.Kind == kind {
			return true
		}
	}
	return false
}
