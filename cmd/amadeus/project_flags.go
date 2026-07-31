package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/spf13/cobra"
)

const flagProject = "project"

type projectFlags struct {
	path string
}

func (flags *projectFlags) bind(command *cobra.Command) {
	command.PersistentFlags().StringVar(&flags.path, flagProject, "", "use an explicit target project directory")
}

func (flags *projectFlags) resolve(command *cobra.Command, runtime commandRuntime) (project.Root, error) {
	explicit := command.Root().PersistentFlags().Changed(flagProject)
	return resolveProjectRoot(runtime.workingDirectory, runtime.workingDirectoryErr, flags.path, explicit)
}

func resolveProjectRoot(startupWorkingDirectory string, startupWorkingDirectoryErr error, configuredPath string, explicit bool) (project.Root, error) {
	path := configuredPath
	if !explicit {
		if startupWorkingDirectoryErr != nil {
			return project.Root{}, fmt.Errorf("resolve startup working directory: %w", startupWorkingDirectoryErr)
		}
		path = startupWorkingDirectory
	} else if strings.TrimSpace(path) == "" {
		return project.Root{}, errors.New("explicit project path is empty")
	} else if !filepath.IsAbs(path) {
		if startupWorkingDirectoryErr != nil {
			return project.Root{}, fmt.Errorf("resolve relative project path without startup working directory: %w", startupWorkingDirectoryErr)
		}
		if startupWorkingDirectory == "" {
			return project.Root{}, errors.New("startup working directory is empty")
		}
		path = filepath.Join(startupWorkingDirectory, path)
	}

	root, err := project.NewRoot(path)
	if err != nil {
		return project.Root{}, fmt.Errorf("resolve target project: %w", err)
	}
	return root, nil
}
