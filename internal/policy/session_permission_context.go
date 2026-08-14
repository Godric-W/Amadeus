package policy

import (
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// CommandApprovalKey identifies one exact command in one canonical working
// directory. It is intentionally narrower than a shell parser or glob rule.
type CommandApprovalKey struct {
	CWD     string
	Command string
}

func NewCommandApprovalKey(command, cwd string) (CommandApprovalKey, bool) {
	if strings.ContainsRune(command, '\x00') {
		return CommandApprovalKey{}, false
	}
	cwd = filepath.Clean(strings.TrimSpace(cwd))
	command = strings.ReplaceAll(command, "\r\n", "\n")
	if cwd == "." || strings.TrimSpace(command) == "" || !filepath.IsAbs(cwd) {
		return CommandApprovalKey{}, false
	}
	return CommandApprovalKey{CWD: cwd, Command: command}, true
}

// SessionPermissionContext contains the in-memory "allow for this session"
// rules. It is owned by one Session and is discarded when that Session closes.
// The rule shape remains tool-specific: commands match an exact CWD/command,
// file edits match a canonical directory, and external tools match a key.
type SessionPermissionContext struct {
	mu              sync.RWMutex
	commands        map[CommandApprovalKey]struct{}
	readDirectories []string
	editDirectories []string
	rules           map[string]struct{}
}

func (context *SessionPermissionContext) Match(grant PermissionGrant) bool {
	if context == nil || !grant.Valid() {
		return false
	}
	switch grant.Kind {
	case GrantReadDirectory:
		return context.MatchReadDirectory(grant.Directory)
	case GrantEditDirectory:
		return context.MatchEditDirectory(grant.Directory)
	case GrantCommandExact:
		return context.MatchCommand(*grant.Command)
	case GrantExternalKey:
		return context.MatchExternal(grant.Key)
	default:
		return false
	}
}

func (context *SessionPermissionContext) ApplyGrant(grant PermissionGrant) {
	if context == nil || !grant.Valid() {
		return
	}
	switch grant.Kind {
	case GrantReadDirectory:
		context.ApplyReadDirectoryGrant(grant.Directory)
	case GrantEditDirectory:
		context.ApplyEditDirectoryGrant(grant.Directory)
	case GrantCommandExact:
		context.ApplyCommandGrant(*grant.Command)
	case GrantExternalKey:
		context.ApplyExternalGrant(grant.Key)
	}
}

func NewSessionPermissionContext() *SessionPermissionContext {
	return &SessionPermissionContext{
		commands: make(map[CommandApprovalKey]struct{}),
		rules:    make(map[string]struct{}),
	}
}

func (context *SessionPermissionContext) MatchCommand(key CommandApprovalKey) bool {
	if context == nil {
		return false
	}
	context.mu.RLock()
	_, ok := context.commands[key]
	context.mu.RUnlock()
	return ok
}

func (context *SessionPermissionContext) ApplyCommandGrant(key CommandApprovalKey) {
	if context == nil {
		return
	}
	context.mu.Lock()
	context.commands[key] = struct{}{}
	context.mu.Unlock()
}

func (context *SessionPermissionContext) MatchReadDirectory(path string) bool {
	return context.matchDirectory(context.readDirectories, path)
}

func (context *SessionPermissionContext) MatchEditDirectory(path string) bool {
	return context.matchDirectory(context.editDirectories, path)
}

func (context *SessionPermissionContext) matchDirectory(directories []string, path string) bool {
	if context == nil {
		return false
	}
	canonical := filepath.Clean(path)
	context.mu.RLock()
	defer context.mu.RUnlock()
	for _, root := range directories {
		if canonical == root || strings.HasPrefix(canonical, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func (context *SessionPermissionContext) ApplyReadDirectoryGrant(path string) {
	context.applyDirectory(&context.readDirectories, path)
}

func (context *SessionPermissionContext) ApplyEditDirectoryGrant(path string) {
	context.applyDirectory(&context.editDirectories, path)
}

func (context *SessionPermissionContext) applyDirectory(directories *[]string, path string) {
	if context == nil || strings.TrimSpace(path) == "" {
		return
	}
	canonical := filepath.Clean(path)
	context.mu.Lock()
	defer context.mu.Unlock()
	for _, existing := range *directories {
		if canonical == existing || strings.HasPrefix(canonical, existing+string(filepath.Separator)) {
			return
		}
	}
	filtered := (*directories)[:0]
	for _, existing := range *directories {
		if !strings.HasPrefix(existing, canonical+string(filepath.Separator)) {
			filtered = append(filtered, existing)
		}
	}
	*directories = append(filtered, canonical)
	sort.Slice(*directories, func(i, j int) bool { return len((*directories)[i]) > len((*directories)[j]) })
}

func (context *SessionPermissionContext) MatchExternal(key string) bool {
	if context == nil {
		return false
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	context.mu.RLock()
	_, ok := context.rules[key]
	context.mu.RUnlock()
	return ok
}

func (context *SessionPermissionContext) ApplyExternalGrant(key string) {
	if context == nil {
		return
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	context.mu.Lock()
	context.rules[key] = struct{}{}
	context.mu.Unlock()
}

func (context *SessionPermissionContext) Clear() {
	if context == nil {
		return
	}
	context.mu.Lock()
	context.commands = make(map[CommandApprovalKey]struct{})
	context.readDirectories = nil
	context.editDirectories = nil
	context.rules = make(map[string]struct{})
	context.mu.Unlock()
}

func (context *SessionPermissionContext) GrantCount() int {
	if context == nil {
		return 0
	}
	context.mu.RLock()
	count := len(context.commands) + len(context.readDirectories) + len(context.editDirectories) + len(context.rules)
	context.mu.RUnlock()
	return count
}

func MCPApprovalKey(server, name string) string {
	return "mcp:" + strings.TrimSpace(server) + "/" + strings.TrimSpace(name)
}

func WebHostApprovalKey(host string) string {
	return "web:" + strings.ToLower(strings.TrimSpace(host))
}
