package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/Godric-W/Amadeus/internal/tool"
)

func TestWaitAgentSpecAndValidationShareTimeoutBounds(t *testing.T) {
	spec := multiAgentSpec("wait_agent")
	var schema struct {
		Properties map[string]struct {
			Minimum int `json:"minimum"`
			Maximum int `json:"maximum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(spec.InputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	timeout := schema.Properties["timeout_ms"]
	if timeout.Minimum != 10_000 || timeout.Maximum != 3_600_000 {
		t.Fatalf("timeout schema = %#v", timeout)
	}
	definition := multiAgentTool{kind: "wait_agent"}
	for _, payload := range []string{
		`{"ids":["child"],"timeout_ms":9999}`,
		`{"ids":["child"],"timeout_ms":3600001}`,
	} {
		invocation := tool.Invocation{Call: tool.NewCall("call", "wait_agent", []byte(payload))}
		if err := definition.ValidateInput(tool.ToolUseContext{Context: context.Background()}, invocation); err == nil {
			t.Fatalf("accepted timeout payload %s", payload)
		}
	}
	for _, timeoutMS := range []int{10_000, 3_600_000} {
		payload := []byte(fmt.Sprintf(`{"ids":["child"],"timeout_ms":%d}`, timeoutMS))
		invocation := tool.Invocation{Call: tool.NewCall("call", "wait_agent", payload)}
		if err := definition.ValidateInput(tool.ToolUseContext{Context: context.Background()}, invocation); err != nil {
			t.Fatalf("rejected boundary timeout %d: %v", timeoutMS, err)
		}
	}
}
