package buildinfo

import "testing"

func TestCurrentDefaults(t *testing.T) {
	info := Current()

	if info.Version != defaultVersion {
		t.Fatalf("unexpected default version: got %q, want %q", info.Version, defaultVersion)
	}
	if info.Commit != defaultCommit {
		t.Fatalf("unexpected default commit: got %q, want %q", info.Commit, defaultCommit)
	}
	if info.BuildTime != defaultBuildTime {
		t.Fatalf("unexpected default build time: got %q, want %q", info.BuildTime, defaultBuildTime)
	}
}

func TestValueOrDefault(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		fallback string
		expected string
	}{
		{name: "value", value: "v1.0.0", fallback: "dev", expected: "v1.0.0"},
		{name: "fallback", fallback: "unknown", expected: "unknown"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if actual := valueOrDefault(test.value, test.fallback); actual != test.expected {
				t.Fatalf("unexpected value: got %q, want %q", actual, test.expected)
			}
		})
	}
}
