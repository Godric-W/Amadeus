package react

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/Godric-W/Amadeus/internal/agent/engine"
	"github.com/Godric-W/Amadeus/internal/tool"
)

func TestProgressMonitorDetectsRepeatedCanonicalAction(t *testing.T) {
	monitor := DefaultProgressMonitor()
	first := ProgressSample{Calls: []tool.Call{tool.NewCall("call_1", "read_file", json.RawMessage(`{"path":"README.md","offset":0}`))}, EvidenceBefore: 0, EvidenceAfter: 1}
	second := ProgressSample{Calls: []tool.Call{tool.NewCall("call_2", "read_file", json.RawMessage(`{"offset":0,"path":"README.md"}`))}, EvidenceBefore: 1, EvidenceAfter: 2}
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

func TestProgressMonitorDetectsRepeatedErrorAndNoEvidence(t *testing.T) {
	monitor := DefaultProgressMonitor()
	sample := ProgressSample{
		Observations:   []engine.Observation{{ToolName: "read_file", Error: " Permission   Denied "}},
		EvidenceBefore: 1, EvidenceAfter: 1,
	}
	if signals, err := monitor.Observe(sample); err != nil || len(signals) != 0 {
		t.Fatalf("unexpected first failure signals=%#v err=%v", signals, err)
	}
	sample.Observations[0].Error = "permission denied"
	signals, err := monitor.Observe(sample)
	if err != nil {
		t.Fatalf("observe repeated failure: %v", err)
	}
	if !hasSignal(signals, ProgressRepeatedError) || !hasSignal(signals, ProgressNoEvidence) {
		t.Fatalf("expected repeated error and no-evidence signals: %#v", signals)
	}

	_, err = monitor.Observe(ProgressSample{EvidenceBefore: 1, EvidenceAfter: 2, Calls: []tool.Call{tool.NewCall("call_3", "read_file", json.RawMessage(`{}`))}})
	if err != nil {
		t.Fatalf("observe evidence progress: %v", err)
	}
	monitor.mutex.Lock()
	noEvidenceRuns := monitor.noEvidenceRuns
	monitor.mutex.Unlock()
	if noEvidenceRuns != 0 {
		t.Fatalf("evidence progress did not reset no-progress counter: %d", noEvidenceRuns)
	}
}

func TestProgressMonitorDetectsHighImpactCalls(t *testing.T) {
	monitor := DefaultProgressMonitor()
	call := tool.NewCall("call_write", "write_file", json.RawMessage(`{"path":"main.go"}`))
	signals, err := monitor.Observe(ProgressSample{
		Calls: []tool.Call{call}, EvidenceBefore: 0, EvidenceAfter: 0,
		Specs: []tool.Spec{{Name: "write_file", SideEffect: tool.SideEffectWrite, ResourceStrategy: tool.ResourceStrategy{Mode: tool.ResourceModeArguments}}},
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
			_, _ = monitor.Observe(ProgressSample{
				Calls:          []tool.Call{tool.NewCall("call", "read_file", json.RawMessage(`{"path":"README.md"}`))},
				EvidenceBefore: 0, EvidenceAfter: 1,
			})
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
