package session

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Godric-W/Amadeus/internal/tool"
)

const maximumActionTextRunes = 240

var (
	presentationSensitiveAssignment = regexp.MustCompile(`(?i)(authorization|proxy-authorization|api[_-]?key|access[_-]?token|refresh[_-]?token|password|client[_-]?secret|cookie|set-cookie)(\s*[:=]\s*)([^\s,;}]+)`)
	presentationBearerCredential    = regexp.MustCompile(`(?i)bearer\s+[^\s,;}]+`)
)

type callPresentation struct {
	ActionSummary string
	Detail        string
}

func presentToolCall(spec tool.ToolSpec, call tool.ToolCall) callPresentation {
	arguments := map[string]any{}
	_ = json.Unmarshal(call.Payload, &arguments)
	value := func(key string) string {
		candidate, _ := arguments[key].(string)
		return sanitizeActionText(candidate)
	}
	path := value("path")
	switch call.Name {
	case "read":
		return callPresentation{ActionSummary: joinAction("Read", path)}
	case "glob":
		return callPresentation{ActionSummary: joinAction("Find", value("pattern"))}
	case "grep":
		summary := joinAction("Search", value("query"))
		if path != "" {
			summary += " in " + path
		}
		return callPresentation{ActionSummary: summary}
	case "write":
		return callPresentation{ActionSummary: joinAction("Create", path)}
	case "edit":
		return callPresentation{ActionSummary: joinAction("Update", path)}
	case "read_skill":
		if path != "" {
			return callPresentation{ActionSummary: joinAction("Read skill reference", path)}
		}
		return callPresentation{ActionSummary: joinAction("Read skill", value("name"))}
	case "execute_command":
		command := value("command")
		if command == "" {
			return callPresentation{ActionSummary: "Ran command"}
		}
		return callPresentation{ActionSummary: joinAction("Ran", command)}
	case "web_search":
		return callPresentation{ActionSummary: "Searched web", Detail: value("query")}
	case "web_fetch":
		return callPresentation{ActionSummary: joinAction("Fetched", safeURLHost(value("url")))}
	case "view_image":
		return callPresentation{ActionSummary: joinAction("View image", path), Detail: path}
	case "spawn_agent":
		return callPresentation{ActionSummary: "Spawning agent", Detail: value("message")}
	case "send_input":
		return callPresentation{ActionSummary: joinAction("Sending input to", value("id")), Detail: value("message")}
	case "wait_agent":
		var ids []string
		_ = json.Unmarshal(call.Payload, &struct {
			IDs *[]string `json:"ids"`
		}{IDs: &ids})
		if len(ids) == 1 {
			return callPresentation{ActionSummary: joinAction("Waiting for", ids[0])}
		}
		return callPresentation{ActionSummary: "Waiting for agents"}
	case "close_agent":
		return callPresentation{ActionSummary: joinAction("Closing", value("id"))}
	case "mcp_list_tools":
		return callPresentation{ActionSummary: joinAction("Listed MCP tools", value("server"))}
	case "mcp_call":
		server, remoteTool := value("server"), value("tool")
		if remoteTool == "" {
			remoteTool = value("name")
		}
		detail := strings.Trim(strings.Join([]string{server, remoteTool}, " · "), " ·")
		return callPresentation{ActionSummary: "Called MCP tool", Detail: detail}
	case "mcp_list_resources":
		return callPresentation{ActionSummary: joinAction("Listed MCP resources", value("server"))}
	case "mcp_read_resource":
		return callPresentation{ActionSummary: joinAction("Read MCP resource", value("server")), Detail: value("uri")}
	}
	switch spec.SideEffect {
	case tool.SideEffectNone, tool.SideEffectRead:
		return callPresentation{ActionSummary: joinAction("Explored", call.Name)}
	case tool.SideEffectNetwork:
		return callPresentation{ActionSummary: joinAction("Called network tool", call.Name)}
	default:
		return callPresentation{ActionSummary: joinAction("Ran tool", call.Name)}
	}
}

func joinAction(action, subject string) string {
	action = strings.TrimSpace(action)
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return action
	}
	return action + " " + subject
}

func safeURLHost(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Hostname() == "" {
		return "web page"
	}
	return parsed.Hostname()
}

func sanitizeActionText(value string) string {
	value = strings.Map(func(character rune) rune {
		if character == '\n' || character == '\r' || character == '\t' {
			return ' '
		}
		if unicode.IsControl(character) {
			return -1
		}
		return character
	}, value)
	value = strings.Join(strings.Fields(value), " ")
	value = presentationBearerCredential.ReplaceAllString(value, "Bearer [REDACTED]")
	value = presentationSensitiveAssignment.ReplaceAllString(value, "$1$2[REDACTED]")
	if utf8.RuneCountInString(value) <= maximumActionTextRunes {
		return value
	}
	runes := []rune(value)
	return string(runes[:maximumActionTextRunes-1]) + "…"
}
