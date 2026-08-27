package builtin

import (
	"encoding/json"

	"github.com/Godric-W/Amadeus/internal/tool"
)

const globDescription = `Finds files with bounded glob matching and returns stable, path-sorted results.

Usage:
- Use patterns such as **/*.go or internal/**/test_*.go to find files by name.
- path optionally narrows the search root and must be allowed by the current filesystem policy.
- Use include_hidden deliberately. Results remain subject to ignore rules and runtime limits.
- Use glob for file discovery and grep for content search. Narrow pattern or limit when results are truncated.`

const grepDescription = `Searches project text with bounded, stable file-and-line results, using ripgrep when available.

Usage:
- query is literal by default. Set regex=true only when query is a regular expression.
- Use path to narrow the root, glob or type to constrain files, context for nearby lines, and limit to bound results.
- case_sensitive defaults to false. Multiline matching and alternate output modes are not supported.
- Prefer grep over embedding grep or rg in execute_command; use execute_command only when the requested search cannot be represented here.
- Use read before editing a matched file.`

func globSpec() tool.ToolSpec {
	return tool.ToolSpec{
		Name: "glob", Description: globDescription,
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Directory below which to search. Defaults to the current workspace."},"pattern":{"type":"string","minLength":1,"description":"Glob pattern such as **/*.go."},"include_hidden":{"type":"boolean","description":"Include hidden files that are not otherwise ignored. Defaults to false."},"limit":{"type":"integer","minimum":1,"description":"Maximum number of matching paths before output bounding."}},"required":["pattern"],"additionalProperties":false}`),
		SideEffect:  tool.SideEffectRead, Idempotent: true,
	}
}

func grepSpec() tool.ToolSpec {
	return tool.ToolSpec{
		Name: "grep", Description: grepDescription,
		InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","minLength":1,"description":"Literal text by default, or a regular expression when regex is true."},"path":{"type":"string","description":"File or directory to search. Defaults to the current workspace."},"glob":{"type":"string","description":"Glob filter applied to candidate files."},"type":{"type":"string","description":"Ripgrep file type such as go, rust, py, or js."},"regex":{"type":"boolean","description":"Interpret query as a regular expression. Defaults to false."},"case_sensitive":{"type":"boolean","description":"Use case-sensitive matching. Defaults to false."},"context":{"type":"integer","minimum":0,"description":"Number of lines to include before and after each match."},"limit":{"type":"integer","minimum":1,"description":"Maximum number of matches before output bounding."}},"required":["query"],"additionalProperties":false}`),
		SideEffect:  tool.SideEffectRead, Idempotent: true,
	}
}
