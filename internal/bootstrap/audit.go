package bootstrap

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Godric-W/Amadeus/internal/audit"
	"github.com/Godric-W/Amadeus/internal/config"
)

const EnvXDGStateHome = "XDG_STATE_HOME"

type AuditFactory func() (audit.Sink, io.Closer, error)

func DefaultAuditFactory(lookupEnv config.EnvLookup, userHomeDir func() (string, error)) AuditFactory {
	return func() (audit.Sink, io.Closer, error) {
		path, err := ResolveAuditPath(lookupEnv, userHomeDir)
		if err != nil {
			return nil, nil, err
		}
		file, err := audit.OpenJSONLFile(path)
		if err != nil {
			return nil, nil, err
		}
		return file, file, nil
	}
}

func ResolveAuditPath(lookupEnv config.EnvLookup, userHomeDir func() (string, error)) (string, error) {
	if lookupEnv != nil {
		if stateHome, ok := lookupEnv(EnvXDGStateHome); ok && strings.TrimSpace(stateHome) != "" {
			return filepath.Join(stateHome, "amadeus", "audit", "audit.jsonl"), nil
		}
	}
	if userHomeDir == nil {
		return "", errors.New("user home resolver is nil")
	}
	home, err := userHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home for audit log: %w", err)
	}
	if strings.TrimSpace(home) == "" {
		return "", errors.New("user home for audit log is empty")
	}
	return filepath.Join(home, ".local", "state", "amadeus", "audit", "audit.jsonl"), nil
}

func defaultAuditFactory(lookupEnv config.EnvLookup) AuditFactory {
	return DefaultAuditFactory(lookupEnv, os.UserHomeDir)
}
