package builtin

import (
	"encoding/json"

	"github.com/Godric-W/Amadeus/internal/tool"
)

const readDescription = `Reads a UTF-8 text file from the local filesystem under the current filesystem policy.

Usage:
- path identifies a file, not a directory. Relative paths resolve from the current workspace; absolute paths must be allowed by the current filesystem policy.
- Output uses cat -n style line numbers starting at 1 and is bounded by runtime byte, line, and token limits.
- Use line and limit for a targeted range when you already know the relevant section. For a large file, follow next_line across unchanged, non-overlapping pages until complete_snapshot is true; the runtime accumulates exact line coverage for edit/write safety.
- A gap, changed file, output-truncated page, or individually truncated line does not establish the complete snapshot required by edit or by write on an existing file. A line longer than the configured line limit cannot be made complete by narrower line ranges.
- Use glob to discover files. Use view_image for supported images; read does not process images, PDF files, or notebooks.
- When output is truncated by the overall byte limit, request a narrower line range and continue from next_line.`

const editDescription = `Performs an exact string replacement in an existing text file and returns a structured diff.

Usage:
- You must first use read to obtain a complete, non-truncated snapshot of the file. The edit is rejected when the file was only partially read or changed after the read.
- Preserve the exact tabs, spaces, and text shown after the read line-number prefix. Never include the prefix in old_string or new_string.
- old_string must match. It must be unique unless replace_all is true; add the smallest useful surrounding context when it is ambiguous.
- Use replace_all only when every occurrence should change, such as an intentional rename.
- edit cannot create a file or target a directory. Prefer edit over a complete rewrite when changing an existing file.
- The runtime creates a diff, requests Approval when required, revalidates the target, applies atomically, and verifies the result.`

const writeDescription = `Creates a text file or completely rewrites an existing text file and returns a structured diff.

Usage:
- For an existing file, first use read to obtain a complete, non-truncated snapshot. The write is rejected when the file was only partially read or changed after the read.
- Prefer edit for focused changes to an existing file. Use write for a new file or an intentional complete rewrite.
- path must identify a file, not a directory, and must be allowed by the current filesystem policy.
- Do not create documentation or README files unless the user requests them or the task clearly requires them.
- The runtime creates a diff, requests Approval when required, revalidates the target, writes atomically, and verifies the result.`

func readSpec() tool.ToolSpec {
	return tool.ToolSpec{
		Name: "read", Description: readDescription,
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","minLength":1,"description":"File path to read. Relative paths resolve from the current workspace."},"line":{"type":"integer","minimum":1,"description":"1-based line at which to start. Omit to start at line 1."},"limit":{"type":"integer","minimum":1,"description":"Maximum number of lines to return. Output remains subject to runtime byte and token limits."}},"required":["path"],"additionalProperties":false}`),
		SideEffect:  tool.SideEffectRead, Idempotent: true,
	}
}

func editSpec() tool.ToolSpec {
	return tool.ToolSpec{
		Name: "edit", Description: editDescription,
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","minLength":1,"description":"Existing file to edit after a complete non-truncated read."},"old_string":{"type":"string","minLength":1,"description":"Exact text to replace. Must be unique unless replace_all is true."},"new_string":{"type":"string","description":"Replacement text, preserving the intended indentation."},"replace_all":{"type":"boolean","description":"Replace every occurrence of old_string. Defaults to false."}},"required":["path","old_string","new_string"],"additionalProperties":false}`),
		SideEffect:  tool.SideEffectWrite,
	}
}

func writeSpec() tool.ToolSpec {
	return tool.ToolSpec{
		Name: "write", Description: writeDescription,
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","minLength":1,"description":"File to create or completely rewrite. Existing files require a complete non-truncated read."},"content":{"type":"string","description":"Complete new file content."}},"required":["path","content"],"additionalProperties":false}`),
		SideEffect:  tool.SideEffectWrite,
	}
}
