package tool

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maximumActionTextRunes = 240

var (
	presentationSensitiveAssignment = regexp.MustCompile(`(?i)(authorization|proxy-authorization|api[_-]?key|access[_-]?token|refresh[_-]?token|password|client[_-]?secret|cookie|set-cookie)(\s*[:=]\s*)([^\s,;}]+)`)
	presentationBearerCredential    = regexp.MustCompile(`(?i)bearer\s+[^\s,;}]+`)
)

type CallPresentation struct {
	ActionSummary string
	Detail        string
}

func PresentCall(spec ToolSpec, call ToolCall) CallPresentation {
	arguments := map[string]any{}
	_ = json.Unmarshal(call.Payload, &arguments)
	value := func(key string) string {
		candidate, _ := arguments[key].(string)
		return sanitizeActionText(candidate)
	}
	path := value("path")
	switch call.Name {
	case "read":
		return CallPresentation{ActionSummary: joinAction("Read", path)}
	case "glob":
		return CallPresentation{ActionSummary: joinAction("Find", value("pattern"))}
	case "grep":
		summary := joinAction("Search", value("query"))
		if path != "" {
			summary += " in " + path
		}
		return CallPresentation{ActionSummary: summary}
	case "write":
		return CallPresentation{ActionSummary: joinAction("Create", path)}
	case "edit":
		return CallPresentation{ActionSummary: joinAction("Update", path)}
	case "read_skill":
		if path != "" {
			return CallPresentation{ActionSummary: joinAction("Read skill reference", path)}
		}
		return CallPresentation{ActionSummary: joinAction("Read skill", value("name"))}
	case "execute_command":
		command := value("command")
		if command == "" {
			return CallPresentation{ActionSummary: "Ran command"}
		}
		return CallPresentation{ActionSummary: joinAction("Ran", command)}
	case "web_search":
		return CallPresentation{ActionSummary: "Searched web", Detail: value("query")}
	case "web_fetch":
		return CallPresentation{ActionSummary: joinAction("Fetched", safeURLHost(value("url")))}
	case "mcp_list_tools":
		return CallPresentation{ActionSummary: joinAction("Listed MCP tools", value("server"))}
	case "mcp_call":
		server, remoteTool := value("server"), value("tool")
		if remoteTool == "" {
			remoteTool = value("name")
		}
		detail := strings.Trim(strings.Join([]string{server, remoteTool}, " · "), " ·")
		return CallPresentation{ActionSummary: "Called MCP tool", Detail: detail}
	case "mcp_list_resources":
		return CallPresentation{ActionSummary: joinAction("Listed MCP resources", value("server"))}
	case "mcp_read_resource":
		return CallPresentation{ActionSummary: joinAction("Read MCP resource", value("server")), Detail: value("uri")}
	}
	switch spec.SideEffect {
	case SideEffectNone, SideEffectRead:
		return CallPresentation{ActionSummary: joinAction("Explored", call.Name)}
	case SideEffectNetwork:
		return CallPresentation{ActionSummary: joinAction("Called network tool", call.Name)}
	default:
		return CallPresentation{ActionSummary: joinAction("Ran tool", call.Name)}
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
