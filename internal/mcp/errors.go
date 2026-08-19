package mcp

import "fmt"

type StaleBindingError struct {
	Server   string
	Resource string
	Name     string
	Expected string
	Current  string
}

func (err *StaleBindingError) Error() string {
	if err == nil {
		return "MCP binding is stale"
	}
	kind := "tool"
	identity := err.Name
	if err.Resource != "" {
		kind = "resource"
		identity = err.Resource
	}
	return fmt.Sprintf("stale_mcp_binding: %s %q changed for server %q (expected %s, current %s)", kind, identity, err.Server, err.Expected, err.Current)
}

func (err *StaleBindingError) ToolErrorKind() string { return "stale_mcp_binding" }

type BindingRevisionError struct {
	Expected string
	Current  string
}

func (err *BindingRevisionError) Error() string {
	if err == nil {
		return "MCP binding changed since model sampling"
	}
	return fmt.Sprintf("MCP binding changed since model sampling: expected %s, current %s", err.Expected, err.Current)
}

func (err *BindingRevisionError) ToolErrorKind() string { return "stale_mcp_binding" }
