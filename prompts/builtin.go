package prompts

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"strings"
)

type ID string

const (
	Base              ID = "base.md"
	EngineProtocol    ID = "engine_protocol.md"
	Approval          ID = "approval.md"
	Tools             ID = "tools.md"
	RuntimeContext    ID = "runtime_context.md"
	Instructions      ID = "instructions.md"
	Skills            ID = "skills.md"
	ContextManagement ID = "context_management.md"
	Handoff           ID = "handoff.md"
	EngineRetry       ID = "engine/retry.md"
	TaskReflection    ID = "reflect/task.md"
)

var agentLayers = []ID{
	Base,
	EngineProtocol,
	Approval,
	Tools,
	RuntimeContext,
	Instructions,
	Skills,
	ContextManagement,
	Handoff,
}

var all = []ID{
	Base,
	EngineProtocol,
	Approval,
	Tools,
	RuntimeContext,
	Instructions,
	Skills,
	ContextManagement,
	Handoff,
	EngineRetry,
	TaskReflection,
}

var known = func() map[ID]struct{} {
	result := make(map[ID]struct{}, len(all))
	for _, id := range all {
		result[id] = struct{}{}
	}
	return result
}()

//go:embed *.md engine/*.md reflect/*.md
var embedded embed.FS

func AgentLayers() []ID {
	return append([]ID(nil), agentLayers...)
}

func All() []ID {
	return append([]ID(nil), all...)
}

func Read(id ID) (string, error) {
	if _, ok := known[id]; !ok {
		return "", fmt.Errorf("unknown built-in prompt %q", id)
	}
	content, err := embedded.ReadFile(string(id))
	if err != nil {
		return "", fmt.Errorf("read built-in prompt %q: %w", id, err)
	}
	trimmed := strings.TrimSpace(string(content))
	if trimmed == "" {
		return "", fmt.Errorf("built-in prompt %q is empty", id)
	}
	return trimmed, nil
}

func AgentSystem() string {
	parts := make([]string, len(agentLayers))
	for index, id := range agentLayers {
		parts[index] = mustRead(id)
	}
	return strings.Join(parts, "\n\n")
}

func RetryProtocol() string {
	return mustRead(EngineRetry)
}

func ReflectionProtocol() string {
	return mustRead(TaskReflection)
}

func Embedded() fs.FS {
	return embedded
}

func mustRead(id ID) string {
	content, err := Read(id)
	if err != nil {
		panic(err)
	}
	return content
}

func Validate() error {
	if len(agentLayers) == 0 {
		return errors.New("built-in Agent prompt layers are empty")
	}
	for _, id := range all {
		if _, err := Read(id); err != nil {
			return err
		}
	}
	return nil
}
