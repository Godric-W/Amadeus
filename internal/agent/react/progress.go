package react

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/Godric-W/Amadeus/internal/tool"
)

type ProgressSignalKind string

const (
	ProgressRepeatedAction  ProgressSignalKind = "repeated_action"
	ProgressRepeatedError   ProgressSignalKind = "repeated_error"
	ProgressRepeatedOutcome ProgressSignalKind = "repeated_outcome"
	ProgressHighImpact      ProgressSignalKind = "high_impact"
)

type ProgressSignal struct {
	Kind          ProgressSignalKind `json:"kind"`
	Key           string             `json:"key,omitempty"`
	ToolName      string             `json:"tool_name,omitempty"`
	Count         int                `json:"count,omitempty"`
	Reason        string             `json:"reason"`
	RecommendPlan bool               `json:"recommend_plan,omitempty"`
}

type ProgressSample struct {
	Calls    []tool.Call
	Outcomes []ToolOutcome
	Specs    []tool.Spec
}

type ProgressMonitorOptions struct {
	RepeatedActionThreshold  int
	RepeatedErrorThreshold   int
	RepeatedOutcomeThreshold int
}

type ProgressMonitor struct {
	mutex         sync.Mutex
	options       ProgressMonitorOptions
	actionCounts  map[string]int
	errorCounts   map[string]int
	outcomeCounts map[string]int
}

func NewProgressMonitor(options ProgressMonitorOptions) (*ProgressMonitor, error) {
	if options.RepeatedActionThreshold <= 0 || options.RepeatedErrorThreshold <= 0 || options.RepeatedOutcomeThreshold <= 0 {
		return nil, errors.New("progress monitor thresholds must be greater than zero")
	}
	return &ProgressMonitor{
		options:       options,
		actionCounts:  make(map[string]int),
		errorCounts:   make(map[string]int),
		outcomeCounts: make(map[string]int),
	}, nil
}

func DefaultProgressMonitor() *ProgressMonitor {
	monitor, _ := NewProgressMonitor(ProgressMonitorOptions{
		RepeatedActionThreshold:  2,
		RepeatedErrorThreshold:   2,
		RepeatedOutcomeThreshold: 2,
	})
	return monitor
}

func (monitor *ProgressMonitor) Observe(sample ProgressSample) ([]ProgressSignal, error) {
	monitor.mutex.Lock()
	defer monitor.mutex.Unlock()

	signals := make([]ProgressSignal, 0)
	specs, err := indexProgressSpecs(sample.Specs)
	if err != nil {
		return nil, err
	}
	for index, call := range sample.Calls {
		signature, err := toolCallSignature(call)
		if err != nil {
			return nil, fmt.Errorf("progress sample calls[%d]: %w", index, err)
		}
		if progressCallWasAttempted(sample.Outcomes, index) {
			monitor.actionCounts[signature]++
			count := monitor.actionCounts[signature]
			if count >= monitor.options.RepeatedActionThreshold {
				signals = append(signals, ProgressSignal{
					Kind: ProgressRepeatedAction, Key: signature, ToolName: call.Name, Count: count,
					Reason: "the same normalized tool call has been attempted repeatedly", RecommendPlan: true,
				})
			}
		}
		if spec, ok := specs[call.Name]; ok && highImpact(spec) {
			signals = append(signals, ProgressSignal{
				Kind: ProgressHighImpact, Key: signature, ToolName: call.Name, Count: 1,
				Reason: "the proposed tool call has write, execute, network, or exclusive-resource impact", RecommendPlan: true,
			})
		}
	}

	for index, outcome := range sample.Outcomes {
		callKey := strings.TrimSpace(outcome.ToolName)
		if index < len(sample.Calls) {
			signature, signatureErr := toolCallSignature(sample.Calls[index])
			if signatureErr != nil {
				return nil, fmt.Errorf("progress sample outcome call %d: %w", index, signatureErr)
			}
			callKey = signature
		}
		fingerprint := callKey + "\n" + outcomeFingerprint(outcome)
		monitor.outcomeCounts[fingerprint]++
		if count := monitor.outcomeCounts[fingerprint]; count >= monitor.options.RepeatedOutcomeThreshold {
			signals = append(signals, ProgressSignal{Kind: ProgressRepeatedOutcome, Key: fingerprint, ToolName: outcome.ToolName, Count: count, Reason: "the same normalized tool outcome has occurred repeatedly", RecommendPlan: true})
		}
		if outcome.Error == nil || strings.TrimSpace(outcome.Error.Message) == "" {
			continue
		}
		key := callKey + "\n" + normalizedErrorKey(outcome)
		monitor.errorCounts[key]++
		if count := monitor.errorCounts[key]; count >= monitor.options.RepeatedErrorThreshold {
			signals = append(signals, ProgressSignal{Kind: ProgressRepeatedError, Key: key, ToolName: outcome.ToolName, Count: count, Reason: "the same normalized tool error has occurred repeatedly", RecommendPlan: true})
		}
	}
	return signals, nil
}

func progressCallWasAttempted(outcomes []ToolOutcome, index int) bool {
	if index >= len(outcomes) || outcomes[index].Error == nil {
		return true
	}
	return strings.TrimSpace(outcomes[index].Error.Kind) != "permission_required"
}

func (monitor *ProgressMonitor) Reset() {
	monitor.mutex.Lock()
	monitor.actionCounts = make(map[string]int)
	monitor.errorCounts = make(map[string]int)
	monitor.outcomeCounts = make(map[string]int)
	monitor.mutex.Unlock()
}

func indexProgressSpecs(specs []tool.Spec) (map[string]tool.Spec, error) {
	indexed := make(map[string]tool.Spec, len(specs))
	for _, spec := range specs {
		if _, exists := indexed[spec.Name]; exists {
			return nil, fmt.Errorf("progress sample has duplicate tool spec %q", spec.Name)
		}
		indexed[spec.Name] = spec
	}
	return indexed, nil
}

func toolCallSignature(call tool.Call) (string, error) {
	if strings.TrimSpace(call.Name) == "" {
		return "", errors.New("tool call name is empty")
	}
	decoder := json.NewDecoder(bytes.NewReader(call.Arguments))
	decoder.UseNumber()
	var arguments any
	if err := decoder.Decode(&arguments); err != nil {
		return "", fmt.Errorf("tool call arguments are invalid JSON: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return "", errors.New("tool call arguments contain multiple JSON values")
		}
		return "", fmt.Errorf("read trailing tool call arguments: %w", err)
	}
	canonical, err := json.Marshal(arguments)
	if err != nil {
		return "", fmt.Errorf("canonicalize tool call arguments: %w", err)
	}
	return strings.TrimSpace(call.Name) + ":" + string(canonical), nil
}

func normalizedErrorKey(outcome ToolOutcome) string {
	message := strings.ToLower(strings.Join(strings.Fields(outcome.ErrorMessage()), " "))
	return strings.TrimSpace(outcome.ToolName) + ":" + message
}

func outcomeFingerprint(outcome ToolOutcome) string {
	return strings.TrimSpace(outcome.ToolName) + ":" + string(outcome.Status) + ":" + strings.ToLower(strings.Join(strings.Fields(outcomeSummary(outcome)), " "))
}

func highImpact(spec tool.Spec) bool {
	return spec.SideEffect == tool.SideEffectWrite ||
		spec.SideEffect == tool.SideEffectExecute ||
		spec.SideEffect == tool.SideEffectNetwork
}
