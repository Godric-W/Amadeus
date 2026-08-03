package session

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestEncodeInterruptedContextBoundsAndDeduplicatesPayload(t *testing.T) {
	values := make([]string, 0, maxInterruptedItems+10)
	for index := 0; index < maxInterruptedItems+10; index++ {
		values = append(values, fmt.Sprintf("path-%d", index), "path-0")
	}
	encoded, err := EncodeInterruptedContext(InterruptedContextV1{
		Objective: strings.Repeat("objective ", 200), Status: "interrupted", RelevantPaths: values, Evidence: values,
		Usage: json.RawMessage(strings.Repeat("x", maxInterruptedUsageBytes+1)),
	})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeInterruptedContext(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.RelevantPaths) != maxInterruptedItems || len(decoded.Evidence) != maxInterruptedItems || len(decoded.Usage) != 0 {
		t.Fatalf("interrupted payload was not bounded: %#v", decoded)
	}
	if !strings.HasSuffix(decoded.Objective, "…") || len(encoded) > MaxInterruptedContextBytes {
		t.Fatalf("interrupted payload did not truncate safely: objective=%q bytes=%d", decoded.Objective, len(encoded))
	}
}
