package protocol

import (
	"errors"
	"fmt"
	"strings"
)

type UserMessageAdmissionKind string

const (
	UserMessageAdmissionStarted UserMessageAdmissionKind = "started"
	UserMessageAdmissionSteered UserMessageAdmissionKind = "steered"
)

func (kind UserMessageAdmissionKind) Valid() bool {
	return kind == UserMessageAdmissionStarted || kind == UserMessageAdmissionSteered
}

type UserMessageAdmission struct {
	Kind   UserMessageAdmissionKind
	TurnID TurnID
}

func (admission UserMessageAdmission) Validate() error {
	if !admission.Kind.Valid() {
		return fmt.Errorf("user message admission kind %q is invalid", admission.Kind)
	}
	if strings.TrimSpace(string(admission.TurnID)) == "" {
		return errors.New("user message admission turn ID is empty")
	}
	return nil
}
