package tui

import (
	"sync"

	"github.com/alecthomas/chroma/v2"
	chromastyles "github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
)

const (
	markdownThemeDark  = "amadeus-dark"
	markdownThemeLight = "amadeus-light"
)

var registerMarkdownThemes sync.Once

func newMarkdownRenderer(width int, palette terminalPalette) (*glamour.TermRenderer, error) {
	style := styles.DarkStyleConfig
	if !palette.Dark {
		style = styles.LightStyleConfig
	}
	configureMarkdownStyle(&style, palette)
	return glamour.NewTermRenderer(
		glamour.WithStyles(style),
		glamour.WithColorProfile(palette.colorProfile()),
		glamour.WithChromaFormatter(palette.chromaFormatter()),
		glamour.WithWordWrap(width),
	)
}

func configureMarkdownStyle(style *ansi.StyleConfig, palette terminalPalette) {
	zero := uint(0)
	if palette.NoColor || palette.Level == colorLevelNone {
		*style = ansi.StyleConfig{}
		style.Document.Margin = &zero
		style.Document.Indent = &zero
		style.BlockQuote.Indent = uintPointer(1)
		style.BlockQuote.IndentToken = stringPointer("│ ")
		style.List.LevelIndent = 2
		style.Item.BlockPrefix = "• "
		style.Enumeration.BlockPrefix = ". "
		style.Task.Ticked = "[✓] "
		style.Task.Unticked = "[ ] "
		style.CodeBlock.Margin = &zero
		style.CodeBlock.Indent = &zero
		return
	}
	bold := true
	italic := true
	underline := true
	crossedOut := true
	falseValue := false

	style.Document.Margin = &zero
	style.Document.Indent = &zero
	style.Document.BlockPrefix = ""
	style.Document.BlockSuffix = ""
	style.Document.Color = nil
	style.Text.Color = nil
	style.Paragraph.Color = nil

	style.Heading.StylePrimitive = ansi.StylePrimitive{BlockSuffix: "\n"}
	style.H1.StylePrimitive = ansi.StylePrimitive{Bold: &bold, Underline: &underline}
	style.H2.StylePrimitive = ansi.StylePrimitive{Bold: &bold}
	style.H3.StylePrimitive = ansi.StylePrimitive{Bold: &bold, Italic: &italic}
	style.H4.StylePrimitive = ansi.StylePrimitive{Italic: &italic}
	style.H5.StylePrimitive = ansi.StylePrimitive{Italic: &italic}
	style.H6.StylePrimitive = ansi.StylePrimitive{Italic: &italic}

	style.Emph = ansi.StylePrimitive{Italic: &italic}
	style.Strong = ansi.StylePrimitive{Bold: &bold}
	style.Strikethrough = ansi.StylePrimitive{CrossedOut: &crossedOut}

	accent := palette.markdownAccent()
	style.Code.StylePrimitive = ansi.StylePrimitive{}
	if accent != "" {
		style.Code.Color = &accent
	} else if palette.Level == colorLevelANSI16 {
		style.Code.Bold = &bold
	}
	style.Code.BackgroundColor = nil
	style.Code.Prefix = ""
	style.Code.Suffix = ""

	style.Link = ansi.StylePrimitive{Underline: &underline}
	style.LinkText = ansi.StylePrimitive{Underline: &underline}
	if accent != "" {
		style.Link.Color = &accent
		style.LinkText.Color = &accent
	} else if palette.Level == colorLevelANSI16 {
		style.Link.Bold = &bold
		style.LinkText.Bold = &bold
	}

	quoteColor := palette.markdownColor(terminalRGB{0, 135, 0})
	style.BlockQuote.StylePrimitive = ansi.StylePrimitive{}
	if quoteColor != "" {
		style.BlockQuote.Color = &quoteColor
	} else if palette.Level == colorLevelANSI16 {
		green := "2"
		style.BlockQuote.Color = &green
	}
	style.BlockQuote.Indent = uintPointer(1)
	style.BlockQuote.IndentToken = stringPointer("│ ")

	style.Item.Color = nil
	orderedColor := palette.markdownColor(terminalRGB{95, 175, 255})
	if orderedColor != "" {
		style.Enumeration.Color = &orderedColor
	} else {
		style.Enumeration.Color = nil
	}

	style.CodeBlock.Margin = &zero
	style.CodeBlock.Indent = &zero
	style.CodeBlock.BackgroundColor = nil
	style.CodeBlock.Bold = &falseValue
	style.CodeBlock.Chroma = nil
	style.CodeBlock.Theme = ""
	if palette.Level == colorLevelTrueColor || palette.Level == colorLevelANSI256 || palette.Level == colorLevelANSI16 {
		style.CodeBlock.Theme = markdownTheme(palette.Dark)
	}
}

func markdownTheme(dark bool) string {
	registerMarkdownThemes.Do(func() {
		chromastyles.Register(chroma.MustNewStyle(markdownThemeDark, markdownThemeEntries(true)))
		chromastyles.Register(chroma.MustNewStyle(markdownThemeLight, markdownThemeEntries(false)))
	})
	if dark {
		return markdownThemeDark
	}
	return markdownThemeLight
}

func markdownThemeEntries(dark bool) chroma.StyleEntries {
	if dark {
		return chroma.StyleEntries{
			chroma.Text:                "#C4C4C4",
			chroma.Error:               "#F05B5B",
			chroma.Comment:             "#7C7C7C italic",
			chroma.CommentPreproc:      "#FF875F",
			chroma.Keyword:             "#00AAFF",
			chroma.KeywordReserved:     "#FF5FD2",
			chroma.KeywordNamespace:    "#FF5F87",
			chroma.KeywordType:         "#8A8AE6",
			chroma.Operator:            "#EF8080",
			chroma.Punctuation:         "#E8E8A8",
			chroma.Name:                "#C4C4C4",
			chroma.NameBuiltin:         "#FF8EC7",
			chroma.NameTag:             "#B083EA",
			chroma.NameAttribute:       "#8A8AE6",
			chroma.NameClass:           "#F1F1F1 bold",
			chroma.NameFunction:        "#00D787",
			chroma.LiteralNumber:       "#6EEFC0",
			chroma.LiteralString:       "#D7A875",
			chroma.LiteralStringEscape: "#AFFFD7",
			chroma.GenericDeleted:      "#FD5B5B",
			chroma.GenericInserted:     "#00D787",
			chroma.GenericStrong:       "bold",
			chroma.GenericEmph:         "italic",
		}
	}
	return chroma.StyleEntries{
		chroma.Text:                "#2A2A2A",
		chroma.Error:               "#C62828",
		chroma.Comment:             "#6B7280 italic",
		chroma.CommentPreproc:      "#B45309",
		chroma.Keyword:             "#005F87",
		chroma.KeywordReserved:     "#9D174D",
		chroma.KeywordNamespace:    "#BE123C",
		chroma.KeywordType:         "#5B21B6",
		chroma.Operator:            "#B91C1C",
		chroma.Punctuation:         "#92400E",
		chroma.Name:                "#2A2A2A",
		chroma.NameBuiltin:         "#1E40AF",
		chroma.NameTag:             "#6B21A8",
		chroma.NameAttribute:       "#5B21B6",
		chroma.NameClass:           "#111827 bold",
		chroma.NameFunction:        "#047857",
		chroma.LiteralNumber:       "#0F766E",
		chroma.LiteralString:       "#7C2D12",
		chroma.LiteralStringEscape: "#0E7490",
		chroma.GenericDeleted:      "#B91C1C",
		chroma.GenericInserted:     "#047857",
		chroma.GenericStrong:       "bold",
		chroma.GenericEmph:         "italic",
	}
}

func uintPointer(value uint) *uint       { return &value }
func stringPointer(value string) *string { return &value }
