package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Godric-W/Amadeus/internal/bootstrap"
	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/spf13/cobra"
)

const (
	flagCD     = "cd"
	flagAddDir = "add-dir"
)

type projectFlags struct {
	path    string
	addDirs []string
}

func (flags *projectFlags) bind(command *cobra.Command) {
	command.PersistentFlags().StringVarP(&flags.path, flagCD, "C", "", "use the specified directory as the working root")
	command.PersistentFlags().StringArrayVar(&flags.addDirs, flagAddDir, nil, "add an additional writable directory (repeatable)")
}

func (flags *projectFlags) resolveAdditional(environment bootstrap.Environment, primary project.Root) ([]string, error) {
	result := make([]string, 0, len(flags.addDirs))
	seen := map[string]struct{}{primary.Path(): {}}
	for _, configured := range flags.addDirs {
		candidate := strings.TrimSpace(configured)
		if candidate == "" {
			return nil, errors.New("--add-dir path is empty")
		}
		if !filepath.IsAbs(candidate) {
			if environment.WorkingDirectoryErr != nil {
				return nil, fmt.Errorf("resolve relative --add-dir without startup working directory: %w", environment.WorkingDirectoryErr)
			}
			candidate = filepath.Join(environment.WorkingDirectory, candidate)
		}
		root, err := project.NewRoot(candidate)
		if err != nil {
			return nil, fmt.Errorf("resolve --add-dir %q: %w", configured, err)
		}
		if _, duplicate := seen[root.Path()]; duplicate {
			continue
		}
		seen[root.Path()] = struct{}{}
		result = append(result, root.Path())
	}
	return result, nil
}

func (flags *projectFlags) resolve(command *cobra.Command, environment bootstrap.Environment) (project.Root, error) {
	explicit := command.Root().PersistentFlags().Changed(flagCD)
	return resolveProjectRoot(environment.WorkingDirectory, environment.WorkingDirectoryErr, flags.path, explicit)
}

func resolveProjectRoot(startupWorkingDirectory string, startupWorkingDirectoryErr error, configuredPath string, explicit bool) (project.Root, error) {
	path := configuredPath
	if !explicit {
		if startupWorkingDirectoryErr != nil {
			return project.Root{}, fmt.Errorf("resolve startup working directory: %w", startupWorkingDirectoryErr)
		}
		path = startupWorkingDirectory
	} else if strings.TrimSpace(path) == "" {
		return project.Root{}, errors.New("explicit working root is empty")
	} else if !filepath.IsAbs(path) {
		if startupWorkingDirectoryErr != nil {
			return project.Root{}, fmt.Errorf("resolve relative working root without startup working directory: %w", startupWorkingDirectoryErr)
		}
		if startupWorkingDirectory == "" {
			return project.Root{}, errors.New("startup working directory is empty")
		}
		path = filepath.Join(startupWorkingDirectory, path)
	}

	root, err := project.NewRoot(path)
	if err != nil {
		return project.Root{}, fmt.Errorf("resolve working root: %w", err)
	}
	return root, nil
}
