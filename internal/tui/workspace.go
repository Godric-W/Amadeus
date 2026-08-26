package tui

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

type workspaceCommand func(context.Context, string, ...string) ([]byte, error)

func ResolveWorkspaceBranch(ctx context.Context, root string) string {
	return resolveWorkspaceBranch(ctx, root, runWorkspaceCommand)
}

func resolveWorkspaceBranch(ctx context.Context, root string, run workspaceCommand) string {
	if ctx == nil || strings.TrimSpace(root) == "" || run == nil {
		return ""
	}
	metadataCtx, cancel := context.WithTimeout(ctx, 750*time.Millisecond)
	defer cancel()
	output, err := run(metadataCtx, root, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err == nil {
		return sanitizeWorkspaceMetadata(string(output), 80)
	}
	if errors.Is(metadataCtx.Err(), context.DeadlineExceeded) {
		return ""
	}
	output, err = run(metadataCtx, root, "rev-parse", "--short", "HEAD")
	if err != nil {
		return ""
	}
	commit := sanitizeWorkspaceMetadata(string(output), 20)
	if commit == "" {
		return ""
	}
	return "detached@" + commit
}

func runWorkspaceCommand(ctx context.Context, root string, arguments ...string) ([]byte, error) {
	commandArguments := append([]string{"-C", root}, arguments...)
	return exec.CommandContext(ctx, "git", commandArguments...).Output()
}

func sanitizeWorkspaceMetadata(value string, limit int) string {
	value = strings.TrimSpace(strings.Map(func(character rune) rune {
		if character < 0x20 || character == 0x7f {
			return -1
		}
		return character
	}, value))
	if limit <= 0 || len([]rune(value)) <= limit {
		return value
	}
	return string([]rune(value)[:limit-1]) + "…"
}
