package cli

import (
	"testing"

	"github.com/Godric-W/Amadeus/internal/testutil"
	"github.com/spf13/cobra"
)

func TestSessionFlagsParseResumeThreadIDAtCLIBoundary(t *testing.T) {
	flags := &sessionFlags{}
	command := &cobra.Command{Use: "test"}
	flags.bind(command)
	threadID := testutil.ThreadID(1)
	if err := command.ParseFlags([]string{"--resume=" + threadID.String()}); err != nil {
		t.Fatal(err)
	}
	mode, parsed, err := flags.resolve(command)
	if err != nil || mode != sessionStartResume || parsed != threadID {
		t.Fatalf("resume flags = mode %q id %q err %v", mode, parsed, err)
	}
}

func TestSessionFlagsRejectInvalidResumeThreadID(t *testing.T) {
	flags := &sessionFlags{}
	command := &cobra.Command{Use: "test"}
	flags.bind(command)
	if err := command.ParseFlags([]string{"--resume=not-a-uuid"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := flags.resolve(command); err == nil {
		t.Fatal("invalid resume thread ID was accepted")
	}
}
