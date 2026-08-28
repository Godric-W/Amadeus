package tui

import (
	"bytes"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

type markdownParseAnalysis struct {
	LastTopLevelBlockStart int
	HasReferences          bool
	SourceTransform        bool
}

func (renderer markdownRenderer) Analyze(source string) (analysis markdownParseAnalysis) {
	defer func() {
		if recover() != nil {
			analysis = markdownParseAnalysis{}
		}
	}()
	if len(source) > maxMarkdownRenderBytes {
		return markdownParseAnalysis{}
	}
	parseSource := markdownParseSource(source)
	if parseSource == "" {
		return markdownParseAnalysis{}
	}
	context := parser.NewContext()
	document := renderer.parser.Parse(text.NewReader([]byte(parseSource)), parser.WithContext(context))
	last := document.LastChild()
	start := markdownTopLevelNodeStart([]byte(parseSource), last)
	if markdownTopLevelBlockStableThroughEnd(source, last) {
		start = len(source)
	}
	start = minInt(maxInt(0, start), len(source))
	return markdownParseAnalysis{
		LastTopLevelBlockStart: start,
		HasReferences:          len(context.References()) > 0,
		SourceTransform:        unwrapMarkdownTableFences(parseSource) != parseSource,
	}
}

func markdownTopLevelBlockStableThroughEnd(source string, node ast.Node) bool {
	switch node.(type) {
	case *ast.Heading, *ast.ThematicBreak:
		return strings.HasSuffix(source, "\n")
	case *ast.Paragraph:
		return strings.HasSuffix(source, "\n\n") || strings.HasSuffix(source, "\n\r\n")
	default:
		return false
	}
}

func markdownTopLevelNodeStart(source []byte, node ast.Node) int {
	if node == nil {
		return 0
	}
	if fence, ok := node.(*ast.FencedCodeBlock); ok {
		if fence.Info != nil {
			return markdownSourceLineStart(source, fence.Info.Segment.Start)
		}
		if fence.Lines().Len() > 0 {
			contentStart := markdownSourceLineStart(source, fence.Lines().At(0).Start)
			if contentStart > 0 {
				return markdownSourceLineStart(source, contentStart-1)
			}
		}
		// Goldmark does not retain delimiter offsets for an empty unlabelled
		// fence. Keeping the whole source mutable is conservative and correct.
		return 0
	}

	start := len(source)
	_ = ast.Walk(node, func(current ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || current.Type() != ast.TypeBlock {
			return ast.WalkContinue, nil
		}
		lines := current.Lines()
		for index := 0; index < lines.Len(); index++ {
			if candidate := lines.At(index).Start; candidate < start {
				start = candidate
			}
		}
		return ast.WalkContinue, nil
	})
	if start == len(source) {
		return 0
	}
	return markdownSourceLineStart(source, start)
}

func markdownSourceLineStart(source []byte, offset int) int {
	offset = minInt(maxInt(0, offset), len(source))
	if index := bytes.LastIndexByte(source[:offset], '\n'); index >= 0 {
		return index + 1
	}
	return 0
}
