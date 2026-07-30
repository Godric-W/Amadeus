package llm

import (
	"context"
	"errors"
	"io"
	"reflect"
	"testing"
)

var _ Client = (*fakeClient)(nil)
var _ Stream = (*fakeStream)(nil)

type fakeClient struct {
	response     Response
	stream       Stream
	model        ModelInfo
	capabilities Capabilities
	request      Request
}

func (client *fakeClient) Complete(_ context.Context, request Request) (Response, error) {
	client.request = request
	return client.response, nil
}

func (client *fakeClient) Stream(_ context.Context, request Request) (Stream, error) {
	client.request = request
	return client.stream, nil
}

func (client *fakeClient) Model() ModelInfo {
	return client.model
}

func (client *fakeClient) Capabilities() Capabilities {
	return client.capabilities
}

type fakeStream struct {
	chunks []StreamChunk
	next   int
	closed bool
}

func (stream *fakeStream) Recv() (StreamChunk, error) {
	if stream.next >= len(stream.chunks) {
		return StreamChunk{}, io.EOF
	}
	chunk := stream.chunks[stream.next]
	stream.next++
	return chunk, nil
}

func (stream *fakeStream) Close() error {
	stream.closed = true
	return nil
}

func TestFakeClientImplementsCompleteContract(t *testing.T) {
	expected := Response{
		ID:           "response-1",
		Message:      AssistantMessage("done"),
		FinishReason: FinishReasonStop,
	}
	client := &fakeClient{
		response: expected,
		model: ModelInfo{
			Provider: "fake",
			Name:     "fake-model",
		},
		capabilities: Capabilities{
			SupportsStreaming: true,
			SupportsReasoning: true,
		},
	}
	request := NewRequest("fake-model", []Message{UserMessage("hello")})

	response, err := client.Complete(context.Background(), request)
	if err != nil {
		t.Fatalf("complete request: %v", err)
	}
	if !reflect.DeepEqual(response, expected) {
		t.Fatalf("unexpected response: got %#v, want %#v", response, expected)
	}
	if client.request.Model != "fake-model" || client.request.Messages[0].Content != "hello" {
		t.Fatalf("fake client did not receive request: %#v", client.request)
	}
	if client.Model().Provider != "fake" || client.Model().Name != "fake-model" {
		t.Fatalf("unexpected model info: %#v", client.Model())
	}
	if !client.Capabilities().SupportsStreaming || !client.Capabilities().SupportsReasoning {
		t.Fatalf("unexpected capabilities: %#v", client.Capabilities())
	}
}

func TestFakeStreamImplementsRecvContract(t *testing.T) {
	usage := Usage{InputTokens: 3, OutputTokens: 2, TotalTokens: 5}
	stream := &fakeStream{chunks: []StreamChunk{
		{ContentDelta: "hel"},
		{ContentDelta: "lo"},
		{FinishReason: FinishReasonStop, Usage: &usage},
	}}
	client := &fakeClient{stream: stream}

	opened, err := client.Stream(context.Background(), NewRequest("fake-model", nil))
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}

	first, err := opened.Recv()
	if err != nil || first.ContentDelta != "hel" || first.Completed() {
		t.Fatalf("unexpected first chunk: %#v, err=%v", first, err)
	}
	second, err := opened.Recv()
	if err != nil || second.ContentDelta != "lo" || second.Completed() {
		t.Fatalf("unexpected second chunk: %#v, err=%v", second, err)
	}
	completed, err := opened.Recv()
	if err != nil || !completed.Completed() || completed.Usage == nil || completed.Usage.TotalTokens != 5 {
		t.Fatalf("unexpected completion chunk: %#v, err=%v", completed, err)
	}
	_, err = opened.Recv()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("unexpected terminal stream error: %v", err)
	}
	if err := opened.Close(); err != nil {
		t.Fatalf("close stream: %v", err)
	}
	if !stream.closed {
		t.Fatal("fake stream was not closed")
	}
}
