package builtin

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"strings"
)

type ID string

const (
	AgentBase               ID = "templates/agent/base.md"
	AgentExecution          ID = "templates/agent/execution.md"
	AgentHandoff            ID = "templates/agent/handoff.md"
	AgentSubagent           ID = "templates/agent/subagent.md"
	ModeExecute             ID = "templates/modes/execute.md"
	ModePlan                ID = "templates/modes/plan.md"
	ToolUpdatePlan          ID = "templates/tools/update_plan.md"
	ToolExecuteCommand      ID = "templates/tools/execute_command.md"
	ToolRead                ID = "templates/tools/read.md"
	ToolEdit                ID = "templates/tools/edit.md"
	ToolWrite               ID = "templates/tools/write.md"
	ToolGlob                ID = "templates/tools/glob.md"
	ToolGrep                ID = "templates/tools/grep.md"
	ToolWriteStdin          ID = "templates/tools/write_stdin.md"
	ContextCompaction       ID = "templates/context/compaction.md"
	ContextCompactionPrefix ID = "templates/context/compaction_prefix.md"
)

var agentSystemLayers = []ID{AgentBase, AgentExecution, AgentHandoff}

var all = []ID{
	AgentBase,
	AgentExecution,
	AgentHandoff,
	AgentSubagent,
	ModeExecute,
	ModePlan,
	ToolUpdatePlan,
	ToolExecuteCommand,
	ToolRead,
	ToolEdit,
	ToolWrite,
	ToolGlob,
	ToolGrep,
	ToolWriteStdin,
	ContextCompaction,
	ContextCompactionPrefix,
}

var known = func() map[ID]struct{} {
	result := make(map[ID]struct{}, len(all))
	for _, id := range all {
		result[id] = struct{}{}
	}
	return result
}()

//go:embed templates/**/*.md
var embedded embed.FS

func AgentSystemLayers() []ID { return append([]ID(nil), agentSystemLayers...) }
func All() []ID               { return append([]ID(nil), all...) }

type ToolPrompt struct {
	Name   string
	Prompt ID
}

func ToolPromptOrder() []ToolPrompt {
	return []ToolPrompt{
		{Name: "read", Prompt: ToolRead},
		{Name: "edit", Prompt: ToolEdit},
		{Name: "write", Prompt: ToolWrite},
		{Name: "glob", Prompt: ToolGlob},
		{Name: "grep", Prompt: ToolGrep},
		{Name: "execute_command", Prompt: ToolExecuteCommand},
		{Name: "write_stdin", Prompt: ToolWriteStdin},
		{Name: "update_plan", Prompt: ToolUpdatePlan},
	}
}

func Paths(ids []ID) []string {
	result := make([]string, len(ids))
	for index, id := range ids {
		result[index] = string(id)
	}
	return result
}

func Read(id ID) (string, error) {
	if _, ok := known[id]; !ok {
		return "", fmt.Errorf("unknown built-in Prompt %q", id)
	}
	content, err := embedded.ReadFile(string(id))
	if err != nil {
		return "", fmt.Errorf("read built-in Prompt %q: %w", id, err)
	}
	contentValue := strings.TrimSpace(string(content))
	if contentValue == "" {
		return "", fmt.Errorf("built-in Prompt %q is empty", id)
	}
	return contentValue, nil
}

func Embedded() fs.FS { return embedded }

func Revision() string {
	hash := sha256.New()
	for _, id := range all {
		content, err := embedded.ReadFile(string(id))
		if err != nil {
			continue
		}
		_, _ = hash.Write([]byte(id))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(content)
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func Validate() error {
	if len(agentSystemLayers) == 0 {
		return errors.New("built-in Agent Prompt layers are empty")
	}
	seen := make(map[ID]struct{}, len(all))
	for _, id := range all {
		if _, ok := seen[id]; ok {
			return fmt.Errorf("built-in Prompt %q is duplicated", id)
		}
		seen[id] = struct{}{}
		if _, err := Read(id); err != nil {
			return err
		}
	}
	return nil
}
