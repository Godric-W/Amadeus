package webfetch

import (
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestProjectContentSupportsTextMarkdownJSONAndCharset(t *testing.T) {
	base, _ := url.Parse("https://example.com/data")
	tests := []struct {
		name        string
		contentType string
		content     []byte
		contains    string
	}{
		{name: "text", contentType: "text/plain; charset=iso-8859-1", content: []byte{'c', 'a', 'f', 0xe9}, contains: "café"},
		{name: "markdown", contentType: "text/markdown", content: []byte("# Heading\n\nbody"), contains: "# Heading"},
		{name: "json", contentType: "application/json", content: []byte(`{"value":1}`), contains: "\"value\": 1"},
		{name: "malformed html", contentType: "text/html", content: []byte(`<html><body><main><h2>Recovered<p>Body`), contains: "## Recovered"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document, err := projectContent(base, test.contentType, test.content, false)
			if err != nil || !strings.Contains(document.Markdown, test.contains) {
				t.Fatalf("document=%#v err=%v", document, err)
			}
		})
	}
}

func TestProjectContentRejectsUnsupportedEmptyAndInvalidJSON(t *testing.T) {
	base, _ := url.Parse("https://example.com/data")
	tests := []struct {
		name        string
		contentType string
		content     []byte
		kind        ErrorKind
	}{
		{name: "binary", contentType: "application/pdf", content: []byte("%PDF"), kind: ErrorUnsupportedContent},
		{name: "empty", contentType: "text/html", content: []byte("<html><body><nav>menu</nav></body></html>"), kind: ErrorEmptyContent},
		{name: "json", contentType: "application/json", content: []byte("{"), kind: ErrorInvalidContent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := projectContent(base, test.contentType, test.content, false)
			var fetchError *Error
			if !errors.As(err, &fetchError) || fetchError.Kind != test.kind {
				t.Fatalf("error=%#v want=%s", err, test.kind)
			}
		})
	}
}

func TestProjectContentKeepsTruncatedJSONUsable(t *testing.T) {
	base, _ := url.Parse("https://example.com/data")
	document, err := projectContent(base, "application/json", []byte(`{"items":[1,2`), true)
	if err != nil || !strings.Contains(document.Markdown, `"items"`) {
		t.Fatalf("document=%#v err=%v", document, err)
	}
}
