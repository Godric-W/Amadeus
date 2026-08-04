package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Godric-W/Amadeus/internal/audit"
	"github.com/Godric-W/Amadeus/internal/config"
)

func TestCodingAgentProviderMockE2E(t *testing.T) {
	for _, api := range []config.APIMode{config.APIResponses, config.APIChatCompletions} {
		t.Run(string(api), func(t *testing.T) {
			var mutex sync.Mutex
			var bodies []map[string]any
			requestIndex := 0
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
					t.Errorf("decode Provider request: %v", err)
				}
				mutex.Lock()
				bodies = append(bodies, body)
				requestIndex++
				currentRequest := requestIndex
				mutex.Unlock()
				writer.Header().Set("Content-Type", "text/event-stream")
				switch currentRequest {
				case 1:
					fmt.Fprint(writer, providerTextFixture(api, "PLAN\n- Inspect README and report"))
				case 4:
					fmt.Fprint(writer, providerTextFixture(api, "COMPLETE\nprovider mock complete"))
				default:
					fmt.Fprint(writer, providerAgentFixture(api, currentRequest-1))
				}
			}))
			defer server.Close()

			amadeusHome := t.TempDir()
			projectDirectory := t.TempDir()
			dialect := config.DialectStandard
			if api == config.APIResponses {
				dialect = config.DialectOpenAI
			}
			writeCommandConfig(t, filepath.Join(amadeusHome, "config.yaml"), fmt.Sprintf(`
default_provider: mock
providers:
  mock:
    api: %s
    dialect: %s
    api_key: provider-secret
    base_url: %s/v1
    model: mock-model
    max_retries: 0
agent:
  max_iterations: 8
  max_tool_calls: 8
  max_input_tokens: 10000
  max_output_tokens: 4000
  max_duration: 1m
  max_parallel_tools: 2
`, api, dialect, server.URL))
			if err := os.WriteFile(filepath.Join(projectDirectory, "README.md"), []byte("provider e2e readme\n"), 0o600); err != nil {
				t.Fatalf("write Provider E2E fixture: %v", err)
			}
			auditSink := audit.NewMemorySink()
			runtime := commandRuntime{
				amadeusRoot: amadeusHome, workingDirectory: projectDirectory, lookupEnv: emptyEnvLookup,
				terminalDetector: func(io.Reader) bool { return false }, agentCommandFactory: defaultAgentCommandFactory,
				auditSinkFactory: func() (audit.Sink, io.Closer, error) { return auditSink, nil, nil },
				runIDFactory:     func() string { return "provider-" + string(api) },
				agentContextFactory: func(parent context.Context) (context.Context, context.CancelFunc) {
					return context.WithCancel(parent)
				},
			}
			command := newRootCommandWithRuntime(&configFlags{}, runtime)
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			command.SetIn(strings.NewReader(""))
			command.SetOut(&stdout)
			command.SetErr(&stderr)
			command.SetArgs([]string{"/plan Read README and report"})
			if err := command.Execute(); err != nil {
				t.Fatalf("execute %s Provider Agent: %v\nstderr=%s", api, err, stderr.String())
			}

			mutex.Lock()
			captured := append([]map[string]any(nil), bodies...)
			mutex.Unlock()
			if len(captured) != 4 || stdout.String() != "provider mock complete\n" || len(auditSink.Snapshot()) != 1 {
				t.Fatalf("unexpected %s Provider trace: requests=%d stdout=%q audit=%d", api, len(captured), stdout.String(), len(auditSink.Snapshot()))
			}
			if !providerRequestContainsToolResult(api, captured[2]) {
				t.Fatalf("%s task follow-up request did not replay tool output: %#v", api, captured[2])
			}
			if strings.Contains(stdout.String()+stderr.String(), "provider-secret") || !strings.Contains(stderr.String(), "result: completed") {
				t.Fatalf("unsafe or incomplete %s output: stdout=%q stderr=%q", api, stdout.String(), stderr.String())
			}
		})
	}
}

func TestCodingAgentProviderMockE2EReplansBeforeCompletion(t *testing.T) {
	for _, api := range []config.APIMode{config.APIResponses, config.APIChatCompletions} {
		t.Run(string(api), func(t *testing.T) {
			var mutex sync.Mutex
			var bodies []map[string]any
			requestIndex := 0
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
					t.Errorf("decode Provider request: %v", err)
				}
				mutex.Lock()
				bodies = append(bodies, body)
				requestIndex++
				currentRequest := requestIndex
				mutex.Unlock()
				writer.Header().Set("Content-Type", "text/event-stream")
				switch currentRequest {
				case 1:
					fmt.Fprint(writer, providerTextFixture(api, "PLAN\n- Inspect README"))
				case 2:
					fmt.Fprint(writer, providerAgentFixture(api, 1))
				case 3, 5:
					fmt.Fprint(writer, providerAgentFixture(api, 2))
				case 4:
					fmt.Fprint(writer, providerTextFixture(api, "REPLAN\n- Summarize the README"))
				case 6:
					fmt.Fprint(writer, providerTextFixture(api, "COMPLETE\nprovider replan complete"))
				default:
					t.Errorf("unexpected Provider request %d", currentRequest)
				}
			}))
			defer server.Close()

			amadeusHome := t.TempDir()
			projectDirectory := t.TempDir()
			dialect := config.DialectStandard
			if api == config.APIResponses {
				dialect = config.DialectOpenAI
			}
			writeCommandConfig(t, filepath.Join(amadeusHome, "config.yaml"), fmt.Sprintf(`
default_provider: mock
providers:
  mock:
    api: %s
    dialect: %s
    api_key: provider-secret
    base_url: %s/v1
    model: mock-model
    max_retries: 0
agent:
  max_iterations: 8
  max_tool_calls: 8
  max_input_tokens: 10000
  max_output_tokens: 4000
  max_duration: 1m
  max_parallel_tools: 2
`, api, dialect, server.URL))
			if err := os.WriteFile(filepath.Join(projectDirectory, "README.md"), []byte("provider replan readme\n"), 0o600); err != nil {
				t.Fatalf("write Provider E2E fixture: %v", err)
			}
			runtime := commandRuntime{
				amadeusRoot: amadeusHome, workingDirectory: projectDirectory, lookupEnv: emptyEnvLookup,
				terminalDetector: func(io.Reader) bool { return false }, agentCommandFactory: defaultAgentCommandFactory,
				auditSinkFactory: func() (audit.Sink, io.Closer, error) { return audit.NewMemorySink(), nil, nil },
				runIDFactory:     func() string { return "provider-replan-" + string(api) },
			}
			command := newRootCommandWithRuntime(&configFlags{}, runtime)
			var stdout, stderr bytes.Buffer
			command.SetIn(strings.NewReader(""))
			command.SetOut(&stdout)
			command.SetErr(&stderr)
			command.SetArgs([]string{"/plan Read README and summarize it"})
			if err := command.Execute(); err != nil {
				t.Fatalf("execute %s replan Provider Agent: %v\nstderr=%s", api, err, stderr.String())
			}

			mutex.Lock()
			captured := append([]map[string]any(nil), bodies...)
			mutex.Unlock()
			if len(captured) != 6 || stdout.String() != "provider replan complete\n" {
				t.Fatalf("unexpected %s replan trace: requests=%d stdout=%q stderr=%q", api, len(captured), stdout.String(), stderr.String())
			}
			if !providerRequestContainsToolResult(api, captured[2]) || !isReplannerRequest(captured[3]) {
				t.Fatalf("%s replan trace omitted task evidence or replan payload: %#v", api, captured)
			}
		})
	}
}

func providerAgentFixture(api config.APIMode, requestIndex int) string {
	if api == config.APIResponses {
		switch requestIndex {
		case 1:
			return strings.Join([]string{
				`event: response.created`, `data: {"type":"response.created","sequence_number":0,"response":{"id":"resp_tool","status":"in_progress"}}`, ``,
				`event: response.output_item.added`, `data: {"type":"response.output_item.added","sequence_number":1,"output_index":0,"item":{"id":"item_1","type":"function_call","call_id":"read-1","name":"read_file","arguments":"","status":"in_progress"}}`, ``,
				`event: response.function_call_arguments.done`, `data: {"type":"response.function_call_arguments.done","sequence_number":2,"item_id":"item_1","output_index":0,"name":"read_file","arguments":"{\"path\":\"README.md\"}"}`, ``,
				`event: response.completed`, `data: {"type":"response.completed","sequence_number":3,"response":{"id":"resp_tool","status":"completed","usage":{"input_tokens":2,"input_tokens_details":{"cached_tokens":0},"output_tokens":2,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":4}}}`, ``, `data: [DONE]`, ``,
			}, "\n")
		case 2:
			return responsesTextFixture("resp_final", "provider mock complete")
		default:
			return responsesTextFixture("resp_reflect", `{"scope":"task","verdict":"accept"}`)
		}
	}
	if requestIndex == 1 {
		return strings.Join([]string{
			`data: {"id":"chat_tool","object":"chat.completion.chunk","created":0,"model":"mock-model","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"read-1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"README.md\"}"}}]},"finish_reason":null}]}`, ``,
			`data: {"id":"chat_tool","object":"chat.completion.chunk","created":0,"model":"mock-model","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`, ``, `data: [DONE]`, ``,
		}, "\n")
	}
	text := "provider mock complete"
	if requestIndex > 2 {
		text = `{"scope":"task","verdict":"accept"}`
	}
	return strings.Join([]string{
		fmt.Sprintf(`data: {"id":"chat_text","object":"chat.completion.chunk","created":0,"model":"mock-model","choices":[{"index":0,"delta":{"content":%q},"finish_reason":null}]}`, text), ``,
		`data: {"id":"chat_text","object":"chat.completion.chunk","created":0,"model":"mock-model","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`, ``, `data: [DONE]`, ``,
	}, "\n")
}

func providerTextFixture(api config.APIMode, text string) string {
	if api == config.APIResponses {
		return responsesTextFixture("planner_or_replanner", text)
	}
	return chatTextFixture("planner_or_replanner", text)
}

func requestContainsPayloadField(body map[string]any, field string) bool {
	encoded, _ := json.Marshal(body)
	return strings.Contains(string(encoded), field)
}

func isPlannerRequest(body map[string]any) bool {
	return requestContainsPayloadField(body, "workspace")
}

func isReplannerRequest(body map[string]any) bool {
	return requestContainsPayloadField(body, "graph")
}

func responsesTextFixture(id, text string) string {
	return strings.Join([]string{
		`event: response.created`, fmt.Sprintf(`data: {"type":"response.created","sequence_number":0,"response":{"id":%q,"status":"in_progress"}}`, id), ``,
		`event: response.output_text.delta`, fmt.Sprintf(`data: {"type":"response.output_text.delta","sequence_number":1,"item_id":"message_1","output_index":0,"content_index":0,"delta":%q}`, text), ``,
		`event: response.completed`, fmt.Sprintf(`data: {"type":"response.completed","sequence_number":2,"response":{"id":%q,"status":"completed","usage":{"input_tokens":2,"input_tokens_details":{"cached_tokens":0},"output_tokens":2,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":4}}}`, id), ``, `data: [DONE]`, ``,
	}, "\n")
}

func providerRequestContainsToolResult(api config.APIMode, body map[string]any) bool {
	key := "messages"
	if api == config.APIResponses {
		key = "input"
	}
	encoded, _ := json.Marshal(body[key])
	return bytes.Contains(encoded, []byte("readme"))
}
