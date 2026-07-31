package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/project"
)

const maxRevalidationCommandBytes = 32 << 10

func revalidateInterruptedWorkspace(ctx context.Context, root project.Root, paths []string) agentcontext.WorkspaceRevalidation {
	result := agentcontext.WorkspaceRevalidation{TestsRequireRerun: true}
	seen := make(map[string]struct{}, len(paths))
	for _, candidate := range paths {
		candidate = filepath.ToSlash(strings.TrimSpace(candidate))
		if candidate == "" {
			continue
		}
		if _, duplicate := seen[candidate]; duplicate {
			continue
		}
		seen[candidate] = struct{}{}
		state := agentcontext.WorkspacePathState{Path: candidate}
		absolute, err := root.Resolve(filepath.FromSlash(candidate))
		if err != nil {
			state.Kind = "invalid"
			result.Paths = append(result.Paths, state)
			continue
		}
		info, err := os.Lstat(absolute)
		if errors.Is(err, os.ErrNotExist) {
			result.Paths = append(result.Paths, state)
			continue
		}
		if err != nil {
			state.Kind = "unreadable"
			result.Paths = append(result.Paths, state)
			continue
		}
		state.Exists = true
		state.Size = info.Size()
		switch {
		case info.Mode().IsRegular():
			state.Kind = "file"
			if file, err := os.Open(absolute); err == nil {
				digest := sha256.New()
				_, copyErr := io.CopyN(digest, file, 4<<20)
				_ = file.Close()
				if copyErr == nil || errors.Is(copyErr, io.EOF) {
					state.SHA256 = hex.EncodeToString(digest.Sum(nil))
				}
			}
		case info.IsDir():
			state.Kind = "directory"
		case info.Mode()&os.ModeSymlink != 0:
			state.Kind = "symlink"
		default:
			state.Kind = "other"
		}
		result.Paths = append(result.Paths, state)
	}
	result.GitStatus = boundedGitOutput(ctx, root.Path(), "status", "--short", "--untracked-files=normal")
	result.DiffStat = boundedGitOutput(ctx, root.Path(), "-c", "diff.external=", "-c", "core.pager=cat", "diff", "--no-ext-diff", "--no-textconv", "--stat", "--", ".")
	return result
}

func boundedGitOutput(ctx context.Context, directory string, arguments ...string) string {
	command := exec.CommandContext(ctx, "git", arguments...)
	command.Dir = directory
	output, err := command.Output()
	if err != nil {
		return "unavailable"
	}
	if len(output) > maxRevalidationCommandBytes {
		output = output[:maxRevalidationCommandBytes]
	}
	value := strings.TrimSpace(string(output))
	if value == "" {
		return "clean"
	}
	return value
}
