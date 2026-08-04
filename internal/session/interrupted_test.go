package session

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestEncodePreviousWorkBoundsAndDeduplicatesPayload(t *testing.T) {
	values := make([]string, 0, maxPreviousWorkItems+10)
	for index := 0; index < maxPreviousWorkItems+10; index++ {
		values = append(values, fmt.Sprintf("path-%d", index), "path-0")
	}
	encoded, err := EncodePreviousWork(PreviousWork{
		Objective: strings.Repeat("objective ", 200), Status: "interrupted", RelevantPaths: values, Evidence: values,
		Usage: json.RawMessage(strings.Repeat("x", maxPreviousWorkUsageBytes+1)),
	})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodePreviousWork(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.RelevantPaths) != maxPreviousWorkItems || len(decoded.Evidence) != maxPreviousWorkItems || len(decoded.Usage) != 0 {
		t.Fatalf("interrupted payload was not bounded: %#v", decoded)
	}
	if !strings.HasSuffix(decoded.Objective, "…") || len(encoded) > MaxPreviousWorkBytes {
		t.Fatalf("interrupted payload did not truncate safely: objective=%q bytes=%d", decoded.Objective, len(encoded))
	}
}

func TestDecodePreviousWorkMigratesLegacyCompletedSteps(t *testing.T) {
	decoded, err := DecodePreviousWork(json.RawMessage(`{"type":"amadeus.interrupted_context.v1","objective":"finish migration","status":"interrupted","completed_steps":["read files","read files","patched config"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.CompletedWork) != 2 || decoded.CompletedWork[0] != "read files" || decoded.CompletedWork[1] != "patched config" || len(decoded.LegacyCompletedSteps) != 0 {
		t.Fatalf("legacy completed steps were not migrated: %#v", decoded)
	}
}
