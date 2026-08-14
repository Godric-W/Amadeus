package builtin

import "github.com/Godric-W/Amadeus/internal/tool"

func boundRenderedItems[T any](items []T, render func([]T) string, maxBytes, maxTokens int) ([]T, string, bool, tool.TruncationReason) {
	if maxBytes <= 0 && maxTokens <= 0 {
		text := render(items)
		return items, text, false, tool.TruncationNone
	}
	for count := len(items); count >= 0; count-- {
		bounded := items[:count]
		text := render(bounded)
		bytes := len(text)
		tokens := (bytes + 3) / 4
		byteExceeded := maxBytes > 0 && bytes > maxBytes
		tokenExceeded := maxTokens > 0 && tokens > maxTokens
		if !byteExceeded && !tokenExceeded {
			if count == len(items) {
				return bounded, text, false, tool.TruncationNone
			}
			reason := tool.TruncationTokenLimit
			if maxBytes > 0 && len(render(items[:count+1])) > maxBytes {
				reason = tool.TruncationByteLimit
			}
			return bounded, text, true, reason
		}
	}
	return nil, "", len(items) > 0, tool.TruncationByteLimit
}
