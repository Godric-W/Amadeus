package agentcontext

import (
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/llm"
)

func TestApproxTokenEstimatorUsesCodexByteRatio(t *testing.T) {
	estimator := ApproxTokenEstimator{}

	for _, test := range []struct {
		name  string
		bytes int
		want  int64
	}{
		{name: "empty", bytes: 0, want: 0},
		{name: "one token", bytes: 1, want: 1},
		{name: "four bytes", bytes: 4, want: 1},
		{name: "five bytes rounds up", bytes: 5, want: 2},
		{name: "eight bytes", bytes: 8, want: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			content := make([]byte, test.bytes)
			if got := estimator.EstimateText(string(content)); got != test.want {
				t.Fatalf("EstimateText(%d bytes) = %d, want %d", test.bytes, got, test.want)
			}
		})
	}
}

func TestApproxTokenEstimatorUsesRuneFloorForChinese(t *testing.T) {
	estimator := ApproxTokenEstimator{}
	if got := estimator.EstimateText("修改上下文"); got != 5 {
		t.Fatalf("Chinese estimate = %d, want 5", got)
	}
}

func TestResponseItemEstimateDoesNotChargeImageBase64AsText(t *testing.T) {
	estimator := ApproxTokenEstimator{}
	content := `{"ok":true,"status":"succeeded","metadata":{"prepared_width":32,"prepared_height":32}}`
	small := llm.ToolResultMessageWithParts("call-1", content, llm.ImagePartWithDetail("image/png", "AAAA", "high"))
	large := llm.ToolResultMessageWithParts("call-1", content, llm.ImagePartWithDetail("image/png", strings.Repeat("A", 1_000_000), "high"))
	if smallEstimate, largeEstimate := estimateResponseItem(small, estimator), estimateResponseItem(large, estimator); smallEstimate != largeEstimate {
		t.Fatalf("image estimates small=%d large=%d", smallEstimate, largeEstimate)
	}
}
