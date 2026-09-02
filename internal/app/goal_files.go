package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/google/uuid"
)

const (
	goalAttachmentDirectory = "attachments"
	goalObjectiveFilename   = "goal-objective.md"
	goalReferencePrefix     = "Read the Amadeus goal objective file at "
	goalReferenceSuffix     = " before continuing."
)

func materializeGoalObjective(amadeusRoot, objective string) (string, func(), error) {
	objective = strings.TrimSpace(objective)
	if err := protocol.ValidateThreadGoalObjective(objective); err == nil {
		return objective, func() {}, nil
	} else if len([]rune(objective)) <= protocol.MaxThreadGoalObjectiveChars {
		return "", nil, err
	}
	if strings.TrimSpace(amadeusRoot) == "" || !filepath.IsAbs(amadeusRoot) || filepath.Clean(amadeusRoot) != amadeusRoot {
		return "", nil, errors.New("Amadeus root must be clean and absolute for Goal attachments")
	}
	directory := filepath.Join(amadeusRoot, goalAttachmentDirectory, uuid.New().String())
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", nil, fmt.Errorf("create Goal attachment directory: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(directory) }
	path := filepath.Join(directory, goalObjectiveFilename)
	if err := os.WriteFile(path, []byte(objective), 0o600); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("write Goal objective: %w", err)
	}
	reference := goalReferencePrefix + path + goalReferenceSuffix
	if err := protocol.ValidateThreadGoalObjective(reference); err != nil {
		cleanup()
		return "", nil, err
	}
	return reference, cleanup, nil
}

func expandGoalObjective(amadeusRoot, objective string) string {
	path := goalObjectiveFile(amadeusRoot, objective)
	if path == "" {
		return objective
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return objective
	}
	return string(content)
}

func cleanupGoalObjectiveFile(amadeusRoot, objective string) {
	path := goalObjectiveFile(amadeusRoot, objective)
	if path != "" {
		_ = os.RemoveAll(filepath.Dir(path))
	}
}

func goalObjectiveFile(amadeusRoot, objective string) string {
	if !strings.HasPrefix(objective, goalReferencePrefix) || !strings.HasSuffix(objective, goalReferenceSuffix) {
		return ""
	}
	path := strings.TrimSuffix(strings.TrimPrefix(objective, goalReferencePrefix), goalReferenceSuffix)
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return ""
	}
	relative, err := filepath.Rel(filepath.Join(amadeusRoot, goalAttachmentDirectory), path)
	if err != nil {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(relative), "/")
	if len(parts) != 2 || parts[1] != goalObjectiveFilename {
		return ""
	}
	if _, err := uuid.Parse(parts[0]); err != nil {
		return ""
	}
	expected := filepath.Join(amadeusRoot, goalAttachmentDirectory, parts[0], goalObjectiveFilename)
	if expected != path {
		return ""
	}
	return path
}
