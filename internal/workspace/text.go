package workspace

import (
	"strings"
	"unicode/utf8"
)

type TextDetector struct{}

func (TextDetector) Valid(content []byte) bool {
	return utf8.Valid(content) && !strings.ContainsRune(string(content), 0)
}

type OutputLimiter struct {
	MaxBytes int
}

func (limiter OutputLimiter) Append(buffer *strings.Builder, value string) (written string, truncated bool) {
	if limiter.MaxBytes <= 0 {
		buffer.WriteString(value)
		return value, false
	}
	remaining := limiter.MaxBytes - buffer.Len()
	if remaining <= 0 {
		return "", value != ""
	}
	if len(value) <= remaining {
		buffer.WriteString(value)
		return value, false
	}
	end := remaining
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	value = value[:end]
	buffer.WriteString(value)
	return value, true
}

func EstimateTokens(byteCount int) int {
	if byteCount <= 0 {
		return 0
	}
	return (byteCount + 3) / 4
}
