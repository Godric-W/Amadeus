package agentcontext

import "testing"

func TestConservativeEstimatorUsesCodexByteRatio(t *testing.T) {
	estimator := ConservativeEstimator{}

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
