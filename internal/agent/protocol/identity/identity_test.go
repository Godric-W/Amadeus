package identity

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestNewThreadIDGeneratesCanonicalUUIDv7(t *testing.T) {
	first, err := NewThreadID()
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewThreadID()
	if err != nil {
		t.Fatal(err)
	}
	if first.IsZero() || second.IsZero() || first == second {
		t.Fatalf("generated thread IDs are invalid: %q %q", first, second)
	}
	parsed := uuid.MustParse(first.String())
	if parsed.Version() != 7 || first.String() != strings.ToLower(parsed.String()) {
		t.Fatalf("thread ID is not canonical UUIDv7: %q", first)
	}
}

func TestThreadAndSessionIDCodecRoundTrip(t *testing.T) {
	threadID, err := ParseThreadID("018f47a2-5b4c-7d3e-8a9b-1234567890ab")
	if err != nil {
		t.Fatal(err)
	}
	sessionID := SessionIDFromThreadID(threadID)
	if sessionID.String() != threadID.String() {
		t.Fatalf("root session and thread values differ: %q %q", sessionID, threadID)
	}
	payload, err := json.Marshal(struct {
		SessionID SessionID `json:"session_id"`
		ThreadID  ThreadID  `json:"thread_id"`
	}{SessionID: sessionID, ThreadID: threadID})
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		SessionID SessionID `json:"session_id"`
		ThreadID  ThreadID  `json:"thread_id"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.SessionID != sessionID || decoded.ThreadID != threadID {
		t.Fatalf("identity round trip mismatch: %#v", decoded)
	}
}

func TestIdentityParsersRejectEmptyZeroAndLegacyValues(t *testing.T) {
	for _, value := range []string{"", uuid.Nil.String(), "not-a-uuid"} {
		if _, err := ParseThreadID(value); err == nil {
			t.Fatalf("ParseThreadID(%q) unexpectedly succeeded", value)
		}
		if _, err := ParseSessionID(value); err == nil {
			t.Fatalf("ParseSessionID(%q) unexpectedly succeeded", value)
		}
	}
}
