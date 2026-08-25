package cli

import (
	"fmt"
	"io"
	"sync/atomic"

	"github.com/Godric-W/Amadeus/internal/audit"
	"github.com/Godric-W/Amadeus/internal/bootstrap"
)

func testAgentRootOptions(amadeusRoot, workingDirectory string, terminal bool) RootOptions {
	environment := bootstrap.Environment{
		AmadeusRoot: amadeusRoot, WorkingDirectory: workingDirectory,
		LookupEnv: emptyEnvLookup,
	}
	dependencies := bootstrap.DefaultDependencies(environment)
	dependencies.AuditFactory = func() (audit.Sink, io.Closer, error) {
		return audit.NewMemorySink(), nil, nil
	}
	return RootOptions{
		Environment: environment,
		Bootstrap:   dependencies,
		IsTerminal:  func(io.Reader) bool { return terminal },
	}
}

func testNextID(turnID string) func(string) string {
	var sequence atomic.Uint64
	return func(kind string) string {
		if kind == "turn" {
			return turnID
		}
		return fmt.Sprintf("%s-test-%d", kind, sequence.Add(1))
	}
}
