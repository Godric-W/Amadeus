package builtin

import (
	"strings"

	"github.com/Godric-W/Amadeus/internal/workspace"
)

func bytesAreBinary(content []byte) bool {
	return !workspace.TextDetector{}.Valid(content)
}

func splitLines(content string) []string {
	if content == "" {
		return nil
	}
	lines := strings.SplitAfter(content, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
