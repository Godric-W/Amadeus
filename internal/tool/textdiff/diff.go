package textdiff

import (
	"bytes"
	"fmt"
	"strings"
	"unicode/utf8"
)

type Stats struct {
	Insertions int
	Deletions  int
}

func ContentDiff(operation, path, destination string, before, after []byte) string {
	oldPath, newPath := path, path
	switch operation {
	case "add", "write-new":
		oldPath = "/dev/null"
	case "delete":
		newPath = "/dev/null"
	}
	if destination != "" {
		newPath = destination
	}

	oldLines, oldFinal := splitLines(before)
	newLines, newFinal := splitLines(after)
	var builder strings.Builder
	fmt.Fprintf(&builder, "--- %s\n+++ %s\n", oldPath, newPath)
	if !validText(before) || !validText(after) {
		builder.WriteString("Binary files differ\n")
		return builder.String()
	}
	oldStart, newStart := 1, 1
	if len(oldLines) == 0 {
		oldStart = 0
	}
	if len(newLines) == 0 {
		newStart = 0
	}
	fmt.Fprintf(&builder, "@@ -%d,%d +%d,%d @@\n", oldStart, len(oldLines), newStart, len(newLines))
	writeLines(&builder, '-', oldLines, oldFinal)
	writeLines(&builder, '+', newLines, newFinal)
	return builder.String()
}

func ChangedLineStats(before, after []byte) Stats {
	oldLines, _ := splitLines(before)
	newLines, _ := splitLines(after)
	prefix := 0
	for prefix < len(oldLines) && prefix < len(newLines) && oldLines[prefix] == newLines[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(oldLines)-prefix && suffix < len(newLines)-prefix && oldLines[len(oldLines)-1-suffix] == newLines[len(newLines)-1-suffix] {
		suffix++
	}
	oldChanged := len(oldLines) - prefix - suffix
	newChanged := len(newLines) - prefix - suffix
	return Stats{Insertions: max(newChanged, 0), Deletions: max(oldChanged, 0)}
}

func splitLines(content []byte) ([]string, bool) {
	if len(content) == 0 {
		return nil, false
	}
	finalNewline := content[len(content)-1] == '\n'
	lines := strings.Split(string(content), "\n")
	if finalNewline {
		lines = lines[:len(lines)-1]
	}
	return lines, finalNewline
}

func writeLines(builder *strings.Builder, prefix byte, lines []string, finalNewline bool) {
	for _, line := range lines {
		builder.WriteByte(prefix)
		builder.WriteString(line)
		builder.WriteByte('\n')
	}
	if len(lines) > 0 && !finalNewline {
		builder.WriteString("\\ No newline at end of file\n")
	}
}

func validText(content []byte) bool {
	return utf8.Valid(content) && !bytes.Contains(content, []byte{0})
}

func max(left, right int) int {
	if left > right {
		return left
	}
	return right
}
