package prompt

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// SourceFamily identifies the authoritative reference for one model-visible
// prompt or ToolSpec contract.
type SourceFamily string

const (
	SourceCodex       SourceFamily = "codex"
	SourceClaudeCode  SourceFamily = "claude_code"
	SourceAmadeus     SourceFamily = "amadeus"
	referenceSnapshot              = "2026-08-25"
)

type SourceAsset struct {
	ID             string
	Family         SourceFamily
	Snapshot       string
	Path           string
	SHA256         string
	AdaptedSHA256  string
	AdaptationRule string
	Tools          []string
}

var sourceManifest = []SourceAsset{
	{ID: "model.default", Family: SourceCodex, Snapshot: referenceSnapshot, Path: "codex-rs/core/templates/model_instructions/gpt-5.2-codex_instructions_template.md", SHA256: "492a212d8a23be8b03c488177d8986f4db4ee54a34b2e8a60779e5e5c89a1b63", AdaptedSHA256: "1e164bc1615b2cb9136d638789e5eac41b33b95e4221ee41a9f809fa84af3cee", AdaptationRule: "Replace Codex branding, remove the GPT-5 identity claim, and map apply_patch guidance to Amadeus edit/write tools."},
	{ID: "mode.default", Family: SourceCodex, Snapshot: referenceSnapshot, Path: "codex-rs/collaboration-mode-templates/templates/default.md", SHA256: "94a6a72b8aaf0d597eac1aa59926c58a156dbca7573c1bf48f3d5ae8b80558d7", AdaptedSHA256: "8d227b17e54f7ca41edc12fa0a730ed01e0b199a20f476aa71bac15a00eb05bb", AdaptationRule: "Render the known Amadeus modes: Default and Plan."},
	{ID: "mode.plan", Family: SourceCodex, Snapshot: referenceSnapshot, Path: "codex-rs/collaboration-mode-templates/templates/plan.md", SHA256: "d6d46c2d460a9d91ada2167605a8dfc56efde6b2ab61e101444c736c6fd6960a", AdaptedSHA256: "825430cb403d1de260b569d90a232d5ec2be227a477363387d164a8f0b0ae499", AdaptationRule: "Keep the full Plan contract and adapt only unavailable non-mutating Tool examples."},
	{ID: "compact.prompt", Family: SourceCodex, Snapshot: referenceSnapshot, Path: "codex-rs/prompts/templates/compact/prompt.md", SHA256: "ab0c334d4faca17e3afbb9b16967c1b2fdcc7242a9a0880af57949fa236d6d07", AdaptedSHA256: "bab68c28f14288fc320a002bf87e6e53c3bb88288eb047893a9e65c96ef9c35c", AdaptationRule: "No semantic adaptation after whitespace normalization."},
	{ID: "compact.summary_prefix", Family: SourceCodex, Snapshot: referenceSnapshot, Path: "codex-rs/prompts/templates/compact/summary_prefix.md", SHA256: "e9b088e794a6bb9082ac053fcc760bd818d7e720ee4bcdc72c6e480de7b7cb0e", AdaptedSHA256: "e9b088e794a6bb9082ac053fcc760bd818d7e720ee4bcdc72c6e480de7b7cb0e", AdaptationRule: "No semantic adaptation after whitespace normalization."},
	{ID: "multi_agent.role.subagent", Family: SourceAmadeus, Path: "internal/prompt/builtin/templates/agent/subagent.md", AdaptedSHA256: "08edc6eb5e5b86badaec2ff119c1e401a0d3e13a493df137d9b5953ae204b0d4", AdaptationRule: "Amadeus owns the basic read-only explorer role text; Codex defines its ModelMessages.MultiAgent.Role.Subagent ownership and separate developer-fragment lifecycle."},

	{ID: "tool.read", Family: SourceClaudeCode, Snapshot: referenceSnapshot, Path: "packages/builtin-tools/src/tools/FileReadTool/prompt.ts", SHA256: "687417aa839eba00f62e98965058ac992e21ae0ce31fbade5090254aec674264", AdaptationRule: "Text-only workspace read using path, line, and limit; accumulate same-fingerprint paged coverage for complete-read safety; images use view_image; omit PDF and notebook support.", Tools: []string{"read"}},
	{ID: "tool.edit", Family: SourceClaudeCode, Snapshot: referenceSnapshot, Path: "packages/builtin-tools/src/tools/FileEditTool/prompt.ts", SHA256: "c952aedf29e40477b59ac00c2a0549aa853b4a6efc633b3a21c7e4d39e19695c", AdaptationRule: "Require a complete non-truncated read and preserve Amadeus Diff, Approval, stale check, and atomic apply.", Tools: []string{"edit"}},
	{ID: "tool.write", Family: SourceClaudeCode, Snapshot: referenceSnapshot, Path: "packages/builtin-tools/src/tools/FileWriteTool/prompt.ts", SHA256: "3630b415cb21ec6c1a0f01559e8173880df2ff646453baddadb3a9945994937f", AdaptationRule: "Require a complete read for existing files and preserve Amadeus Approval and atomic write.", Tools: []string{"write"}},
	{ID: "tool.glob", Family: SourceClaudeCode, Snapshot: referenceSnapshot, Path: "packages/builtin-tools/src/tools/GlobTool/prompt.ts", SHA256: "132f5ca7389a40cf11bc7c7ebba034fd767eec871c368d77f50edca6862c7979", AdaptationRule: "Use stable path ordering, bounded results, and Amadeus hidden-file semantics; omit Agent fallback guidance.", Tools: []string{"glob"}},
	{ID: "tool.grep", Family: SourceClaudeCode, Snapshot: referenceSnapshot, Path: "packages/builtin-tools/src/tools/GrepTool/prompt.ts", SHA256: "4658f360bb05db5481577f77a13c2bc2dc895c77d3ccc327ef9bd8420ff1c9a1", AdaptationRule: "Literal search by default; regex is explicit; omit multiline, output modes, and pagination that Amadeus does not implement.", Tools: []string{"grep"}},

	{ID: "tool.update_plan", Family: SourceCodex, Snapshot: referenceSnapshot, Path: "codex-rs/core/src/tools/handlers/plan_spec.rs", SHA256: "ecbd36a78158967384269e1fb50f19991fd8f76c8d880cae536b5da105e0b197", AdaptationRule: "Keep Amadeus transient PlanUpdate Event and Plan Mode visibility policy.", Tools: []string{"update_plan"}},
	{ID: "tool.request_user_input", Family: SourceCodex, Snapshot: referenceSnapshot, Path: "codex-rs/core/src/tools/handlers/request_user_input_spec.rs", SHA256: "86ef5b005211ea6cc14c5a99821357a4484cccbb4756c911bbeba3913315d16f", AdaptationRule: "Keep Amadeus multi-select extension and TUI-provided Other option.", Tools: []string{"request_user_input"}},
	{ID: "tool.execute_command", Family: SourceCodex, Snapshot: referenceSnapshot, Path: "codex-rs/core/src/tools/handlers/shell_spec.rs", SHA256: "62712799b6a184c2f2e5540fc83218de3f61063bc885e8e2b3aa9eff2cb66602", AdaptationRule: "Use Amadeus command, cwd, timeout, process identity, and Claude Code-style Approval contract.", Tools: []string{"execute_command"}},
	{ID: "tool.write_stdin", Family: SourceCodex, Snapshot: referenceSnapshot, Path: "codex-rs/core/src/tools/handlers/shell_spec.rs", SHA256: "62712799b6a184c2f2e5540fc83218de3f61063bc885e8e2b3aa9eff2cb66602", AdaptationRule: "Use process_id and origin_call_id with chars, enter, eof, and inherited command Approval.", Tools: []string{"write_stdin"}},
	{ID: "tool.view_image", Family: SourceCodex, Snapshot: referenceSnapshot, Path: "codex-rs/core/src/tools/handlers/view_image_spec.rs", SHA256: "58eecba655498ca81d5acd908cc9ae62622d7dea9004abd487d5d0940f771f83", AdaptationRule: "Keep one local environment and Amadeus bounded PNG, JPEG, WebP, and static GIF preparation.", Tools: []string{"view_image"}},
	{ID: "tool.multi_agent", Family: SourceCodex, Snapshot: referenceSnapshot, Path: "codex-rs/core/src/tools/handlers/multi_agents_spec.rs", SHA256: "3ccedea36cc1c40e846a5c363edf614aba2b923055a86aff36a2473890de1be5", AdaptationRule: "Use the Codex V1 lifecycle with a read-only explorer, depth one, and no model, fork, or worker-write controls.", Tools: []string{"spawn_agent", "send_input", "wait_agent", "close_agent"}},
	{ID: "tool.mcp_resource", Family: SourceCodex, Snapshot: referenceSnapshot, Path: "codex-rs/core/src/tools/handlers/mcp_resource_spec.rs", SHA256: "95587df9d87bb7abd30a506b01015efc89113fce5f25c2ef4281f6ffea29ae90", AdaptationRule: "Require one configured server and omit resource templates not implemented by Amadeus.", Tools: []string{"mcp_list_resources", "mcp_read_resource"}},

	{ID: "tool.web", Family: SourceAmadeus, Path: "internal/tool/builtin/web_search.go + web_fetch.go", AdaptationRule: "Amadeus owns snippet versus full-page evidence, URL safety, redirects, and untrusted-result semantics.", Tools: []string{"web_search", "web_fetch"}},
	{ID: "tool.read_skill", Family: SourceAmadeus, Path: "internal/tool/builtin/read_skill.go", AdaptationRule: "Amadeus owns bounded SKILL.md and references access through the Skill catalog.", Tools: []string{"read_skill"}},
	{ID: "tool.mcp_lazy", Family: SourceAmadeus, Path: "internal/mcp/lazy_tool.go", AdaptationRule: "Amadeus owns lazy server discovery, sanitized schemas, binding revisions, and untrusted external results.", Tools: []string{"mcp_list_tools", "mcp_call"}},
}

func SourceManifest() []SourceAsset {
	result := append([]SourceAsset(nil), sourceManifest...)
	for index := range result {
		result[index].Tools = append([]string(nil), result[index].Tools...)
	}
	return result
}

func ValidateSourceManifest() error {
	seen := make(map[string]struct{}, len(sourceManifest))
	seenTools := make(map[string]string)
	for _, asset := range sourceManifest {
		if strings.TrimSpace(asset.ID) == "" || strings.TrimSpace(asset.Path) == "" || strings.TrimSpace(asset.AdaptationRule) == "" {
			return errors.New("prompt source manifest entry is incomplete")
		}
		if _, exists := seen[asset.ID]; exists {
			return fmt.Errorf("prompt source manifest ID %q is duplicated", asset.ID)
		}
		seen[asset.ID] = struct{}{}
		if strings.HasPrefix(asset.ID, "tool.") != (len(asset.Tools) > 0) {
			return fmt.Errorf("prompt source %q has invalid Tool mapping", asset.ID)
		}
		for _, name := range asset.Tools {
			name = strings.TrimSpace(name)
			if name == "" {
				return fmt.Errorf("prompt source %q has an empty Tool mapping", asset.ID)
			}
			if owner, exists := seenTools[name]; exists {
				return fmt.Errorf("Tool %q is mapped by both %q and %q", name, owner, asset.ID)
			}
			seenTools[name] = asset.ID
		}
		if asset.AdaptedSHA256 != "" {
			if len(asset.AdaptedSHA256) != 64 {
				return fmt.Errorf("adapted prompt source %q has incomplete SHA-256", asset.ID)
			}
			if _, err := hex.DecodeString(asset.AdaptedSHA256); err != nil {
				return fmt.Errorf("adapted prompt source %q has invalid SHA-256: %w", asset.ID, err)
			}
		}
		switch asset.Family {
		case SourceCodex, SourceClaudeCode:
			if asset.Snapshot == "" || len(asset.SHA256) != 64 {
				return fmt.Errorf("external prompt source %q has incomplete provenance", asset.ID)
			}
			if _, err := hex.DecodeString(asset.SHA256); err != nil {
				return fmt.Errorf("external prompt source %q has invalid SHA-256: %w", asset.ID, err)
			}
		case SourceAmadeus:
			if asset.Snapshot != "" || asset.SHA256 != "" {
				return fmt.Errorf("Amadeus-owned prompt source %q must not claim external provenance", asset.ID)
			}
		default:
			return fmt.Errorf("prompt source %q has unknown family %q", asset.ID, asset.Family)
		}
	}
	return nil
}
