package modelclient

import (
	"context"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/protocol"
)

func TestResponseRetryBackoffSaturates(t *testing.T) {
	if delay := responseRetryBackoff(1); delay <= 0 {
		t.Fatalf("first backoff = %s", delay)
	}
	if delay := responseRetryBackoff(100); delay != maxResponseRetryBackoff {
		t.Fatalf("saturated backoff = %s, want %s", delay, maxResponseRetryBackoff)
	}
}

func TestPublishStreamFailureRedactsSensitiveDetails(t *testing.T) {
	sink := protocol.NewMemorySink()
	providerError := &llm.ProviderError{
		Kind: llm.ProviderErrorNetwork, Message: "token=secret-value", AdditionalDetails: "Authorization: Bearer secret-value", Retryable: true,
	}
	if err := publishStreamFailure(context.Background(), sink, "mock", providerError, false, providerError.Error()); err != nil {
		t.Fatal(err)
	}
	events := sink.Snapshot()
	streamError, ok := events[0].Msg.(protocol.StreamErrorEvent)
	if !ok || streamError.AdditionalDetails == nil {
		t.Fatalf("stream error = %#v", events)
	}
	combined := streamError.Message + " " + *streamError.AdditionalDetails
	if strings.Contains(combined, "secret-value") || !strings.Contains(combined, "[REDACTED]") {
		t.Fatalf("sensitive provider detail leaked: %q", combined)
	}
}
