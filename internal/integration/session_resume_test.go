package integration

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/cli"
	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/llm"
)

func TestSessionPersistsAcrossTUILaunchesAndContinueReplaysHistory(t *testing.T) {
	home := t.TempDir()
	projectDirectory := t.TempDir()
	writeCodingCommandConfig(t, home)
	if err := os.WriteFile(filepath.Join(projectDirectory, "README.md"), []byte("project readme\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	firstClient := &codingCommandClient{}
	firstOptions := testAgentRootOptions(home, projectDirectory, false)
	firstOptions.Bootstrap.ClientFactory = func(string, string, config.ModelProviderInfo) (llm.Client, error) { return firstClient, nil }
	first := cli.NewRootCommand(firstOptions)
	first.SetIn(strings.NewReader(""))
	first.SetOut(io.Discard)
	first.SetErr(io.Discard)
	first.SetArgs([]string{"first task"})
	if err := first.Execute(); err != nil {
		t.Fatal(err)
	}

	secondClient := &codingCommandClient{}
	secondOptions := testAgentRootOptions(home, projectDirectory, false)
	secondOptions.Bootstrap.ClientFactory = func(string, string, config.ModelProviderInfo) (llm.Client, error) { return secondClient, nil }
	second := cli.NewRootCommand(secondOptions)
	second.SetIn(strings.NewReader(""))
	second.SetOut(io.Discard)
	second.SetErr(io.Discard)
	second.SetArgs([]string{"--continue", "second task"})
	if err := second.Execute(); err != nil {
		t.Fatal(err)
	}
	if len(secondClient.streamRequests) == 0 {
		t.Fatal("continued thread made no provider request")
	}
	contents := messageContents(secondClient.streamRequests[0].Prompt.Input)
	if !strings.Contains(contents, "first task") || !strings.Contains(contents, "second task") {
		t.Fatalf("continued request omitted canonical history: %s", contents)
	}
}
