package protocol

import "testing"

func TestUserMessageAdmissionValidate(t *testing.T) {
	for _, admission := range []UserMessageAdmission{
		{Kind: UserMessageAdmissionStarted, TurnID: "turn-1"},
		{Kind: UserMessageAdmissionSteered, TurnID: "turn-1"},
	} {
		if err := admission.Validate(); err != nil {
			t.Fatalf("validate %#v: %v", admission, err)
		}
	}
	for _, admission := range []UserMessageAdmission{
		{TurnID: "turn-1"},
		{Kind: UserMessageAdmissionStarted},
	} {
		if err := admission.Validate(); err == nil {
			t.Fatalf("invalid admission accepted: %#v", admission)
		}
	}
}
