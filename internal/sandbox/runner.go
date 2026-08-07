package sandbox

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Godric-W/Amadeus/internal/project"
)

type IsolationMode string

const (
	IsolationSandboxed   IsolationMode = "sandboxed"
	IsolationUnsandboxed IsolationMode = "unsandboxed"
)

type Launch struct {
	Executable string
	Arguments  []string
	Directory  string
	Mode       IsolationMode
}

type Runner struct {
	mode       IsolationMode
	bwrap      string
	policy     *project.FileSystemPolicy
	diagnostic string
}

type Options struct {
	LookPath func(string) (string, error)
	Probe    func(string) error
	GOOS     string
}

func NewRunner(policy *project.FileSystemPolicy) (*Runner, error) {
	return NewRunnerWithOptions(policy, Options{})
}

func NewRunnerWithOptions(policy *project.FileSystemPolicy, options Options) (*Runner, error) {
	if policy == nil {
		return nil, errors.New("sandbox filesystem policy is nil")
	}
	if options.LookPath == nil {
		options.LookPath = exec.LookPath
	}
	if options.Probe == nil {
		options.Probe = probeBubblewrap
	}
	if options.GOOS == "" {
		options.GOOS = runtime.GOOS
	}
	runner := &Runner{mode: IsolationUnsandboxed, policy: policy}
	if options.GOOS != "linux" {
		runner.diagnostic = "OS sandbox is not implemented on " + options.GOOS
		return runner, nil
	}
	path, err := options.LookPath("bwrap")
	if err != nil {
		runner.diagnostic = "bubblewrap is not installed"
		return runner, nil
	}
	if err := options.Probe(path); err != nil {
		runner.diagnostic = "bubblewrap probe failed: " + err.Error()
		return runner, nil
	}
	runner.mode = IsolationSandboxed
	runner.bwrap = path
	return runner, nil
}

func (runner *Runner) Mode() IsolationMode {
	if runner == nil {
		return IsolationUnsandboxed
	}
	return runner.mode
}

func (runner *Runner) Diagnostic() string {
	if runner == nil {
		return "sandbox runner is nil"
	}
	return runner.diagnostic
}

func (runner *Runner) Prepare(shell, command, cwd string) (Launch, error) {
	if runner == nil || runner.policy == nil {
		return Launch{}, errors.New("sandbox runner is nil")
	}
	if strings.TrimSpace(shell) == "" || strings.TrimSpace(command) == "" || strings.TrimSpace(cwd) == "" {
		return Launch{}, errors.New("sandbox command is incomplete")
	}
	if runner.mode != IsolationSandboxed {
		return Launch{Executable: shell, Arguments: []string{"-c", command}, Directory: cwd, Mode: IsolationUnsandboxed}, nil
	}
	arguments := []string{
		"--die-with-parent", "--new-session", "--unshare-pid", "--unshare-ipc", "--unshare-uts", "--share-net",
		"--ro-bind", "/", "/", "--dev", "/dev", "--proc", "/proc",
	}
	for _, root := range runner.policy.WritableRoots() {
		arguments = append(arguments, "--bind", root, root)
	}
	for _, denied := range runner.policy.DeniedRoots() {
		if denied == "/dev" || denied == "/proc" {
			continue
		}
		info, err := os.Stat(denied)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return Launch{}, fmt.Errorf("inspect denied sandbox path %q: %w", denied, err)
		}
		if info.IsDir() {
			arguments = append(arguments, "--tmpfs", denied)
		} else {
			arguments = append(arguments, "--ro-bind", "/dev/null", denied)
		}
	}
	for _, readOnly := range runner.policy.ReadOnlyRoots() {
		arguments = append(arguments, "--ro-bind", readOnly, readOnly)
	}
	arguments = append(arguments, "--chdir", filepath.Clean(cwd), "--", shell, "-c", command)
	return Launch{Executable: runner.bwrap, Arguments: arguments, Directory: string(filepath.Separator), Mode: IsolationSandboxed}, nil
}

func probeBubblewrap(path string) error {
	command := exec.Command(path,
		"--die-with-parent", "--new-session", "--unshare-pid", "--share-net",
		"--ro-bind", "/", "/", "--dev", "/dev", "--proc", "/proc", "--", "/bin/true",
	)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
