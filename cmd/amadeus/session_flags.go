package main

import (
	"errors"
	"strings"

	"github.com/Godric-W/Amadeus/internal/thread"
	"github.com/spf13/cobra"
)

const (
	flagContinue       = "continue"
	flagResume         = "resume"
	resumeSelectorFlag = "__amadeus_select_session__"
)

type sessionStartMode string

const (
	sessionStartDraft    sessionStartMode = "draft"
	sessionStartContinue sessionStartMode = "continue"
	sessionStartResume   sessionStartMode = "resume"
	sessionStartSelect   sessionStartMode = "select"
)

type sessionFlags struct {
	continueLatest bool
	resume         string
}

func (flags *sessionFlags) bind(command *cobra.Command) {
	command.Flags().BoolVar(&flags.continueLatest, flagContinue, false, "continue the latest session for the current project")
	command.Flags().StringVar(&flags.resume, flagResume, "", "resume a session by ID, or select one when no ID is provided")
	command.Flags().Lookup(flagResume).NoOptDefVal = resumeSelectorFlag
}

func (flags *sessionFlags) resolve(command *cobra.Command) (sessionStartMode, thread.ID, error) {
	if command == nil {
		return "", "", errors.New("session flags command is nil")
	}
	resumeChanged := command.Flags().Changed(flagResume)
	if flags.continueLatest && resumeChanged {
		return "", "", errors.New("--continue and --resume cannot be used together")
	}
	if flags.continueLatest {
		return sessionStartContinue, "", nil
	}
	if !resumeChanged {
		return sessionStartDraft, "", nil
	}
	value := strings.TrimSpace(flags.resume)
	if value == resumeSelectorFlag || value == "" {
		return sessionStartSelect, "", nil
	}
	return sessionStartResume, thread.ID(value), nil
}
