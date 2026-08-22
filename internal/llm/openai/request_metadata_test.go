package openai

import (
	"testing"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/testutil"
)

func TestProviderRequestsCarryCanonicalRuntimeIdentityMetadata(t *testing.T) {
	parentID := testutil.ThreadID(1)
	request := llm.NewRequest("test-model", []llm.ResponseItem{llm.UserMessage("hello")})
	request.Metadata = llm.RequestMetadata{
		SessionID: testutil.SessionID(1), ThreadID: testutil.ThreadID(2), TurnID: "turn-1", ParentThreadID: &parentID,
	}
	want := request.Metadata.Values()

	responsesRequest, err := newResponsesRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	if !equalMetadata(responsesRequest.Metadata, want) {
		t.Fatalf("Responses metadata = %#v, want %#v", responsesRequest.Metadata, want)
	}

	chatRequest, err := newChatCompletionsRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	if !equalMetadata(chatRequest.Metadata, want) {
		t.Fatalf("Chat Completions metadata = %#v, want %#v", chatRequest.Metadata, want)
	}
}

func equalMetadata(actual, expected map[string]string) bool {
	if len(actual) != len(expected) {
		return false
	}
	for key, value := range expected {
		if actual[key] != value {
			return false
		}
	}
	return true
}
