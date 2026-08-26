package tui

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestResolveWorkspaceBranchHandlesBranchDetachedAndNonRepository(t *testing.T) {
	tests := []struct {
		name      string
		responses map[string]struct {
			output []byte
			err    error
		}
		want string
	}{
		{name: "branch", responses: map[string]struct {
			output []byte
			err    error
		}{"symbolic-ref --quiet --short HEAD": {output: []byte("main\n")}}, want: "main"},
		{name: "detached", responses: map[string]struct {
			output []byte
			err    error
		}{
			"symbolic-ref --quiet --short HEAD": {err: errors.New("detached")},
			"rev-parse --short HEAD":            {output: []byte("abc1234\n")},
		}, want: "detached@abc1234"},
		{name: "not repository", responses: map[string]struct {
			output []byte
			err    error
		}{
			"symbolic-ref --quiet --short HEAD": {err: errors.New("not repository")},
			"rev-parse --short HEAD":            {err: errors.New("not repository")},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var calls []string
			run := func(_ context.Context, root string, arguments ...string) ([]byte, error) {
				if root != "/project" {
					t.Fatalf("root = %q", root)
				}
				key := stringsJoin(arguments)
				calls = append(calls, key)
				response := test.responses[key]
				return response.output, response.err
			}
			if got := resolveWorkspaceBranch(context.Background(), "/project", run); got != test.want {
				t.Fatalf("branch = %q, want %q (calls=%v)", got, test.want, calls)
			}
			if test.name == "branch" && !reflect.DeepEqual(calls, []string{"symbolic-ref --quiet --short HEAD"}) {
				t.Fatalf("calls = %v", calls)
			}
		})
	}
}

func TestResolveWorkspaceBranchRespectsContextDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	branch := resolveWorkspaceBranch(ctx, "/project", func(commandContext context.Context, _ string, _ ...string) ([]byte, error) {
		<-commandContext.Done()
		return nil, commandContext.Err()
	})
	if branch != "" || time.Since(started) > 250*time.Millisecond {
		t.Fatalf("deadline fallback was not bounded: branch=%q elapsed=%s", branch, time.Since(started))
	}
}

func stringsJoin(values []string) string {
	result := ""
	for index, value := range values {
		if index > 0 {
			result += " "
		}
		result += value
	}
	return result
}
