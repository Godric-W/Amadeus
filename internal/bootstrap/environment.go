package bootstrap

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/Godric-W/Amadeus/internal/config"
)

const EnvAmadeusHome = "AMADEUS_HOME"

type Environment struct {
	AmadeusRoot         string
	AmadeusRootError    error
	WorkingDirectory    string
	WorkingDirectoryErr error
	LookupEnv           config.EnvLookup
}

func CurrentEnvironment() Environment {
	lookupEnv := config.EnvLookup(os.LookupEnv)
	root, rootErr := ResolveAmadeusRoot(lookupEnv, os.Executable, filepath.EvalSymlinks)
	workingDirectory, workingDirectoryErr := os.Getwd()
	return Environment{
		AmadeusRoot: root, AmadeusRootError: rootErr,
		WorkingDirectory: workingDirectory, WorkingDirectoryErr: workingDirectoryErr,
		LookupEnv: lookupEnv,
	}
}

func ResolveAmadeusRoot(
	lookupEnv config.EnvLookup,
	executable func() (string, error),
	evalSymlinks func(string) (string, error),
) (string, error) {
	if lookupEnv != nil {
		if root, ok := lookupEnv(EnvAmadeusHome); ok && strings.TrimSpace(root) != "" {
			return root, nil
		}
	}
	if executable == nil {
		return "", errors.New("executable path resolver is nil")
	}
	executablePath, err := executable()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(executablePath) == "" {
		return "", errors.New("executable path is empty")
	}
	if evalSymlinks != nil {
		if resolvedPath, resolveErr := evalSymlinks(executablePath); resolveErr == nil {
			executablePath = resolvedPath
		}
	}
	return filepath.Dir(executablePath), nil
}
