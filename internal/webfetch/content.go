package webfetch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/html/charset"
)

func projectContent(finalURL *url.URL, contentType string, content []byte, partial bool) (Document, error) {
	mediaType := normalizedMediaType(contentType, content)
	document := Document{ContentType: mediaType}
	if finalURL != nil {
		document.URL = finalURL.String()
	}

	switch mediaType {
	case "text/html":
		decoded, err := decodeText(content, contentType)
		if err != nil {
			return Document{}, &Error{Kind: ErrorInvalidContent, URL: document.URL, ContentType: mediaType, Err: fmt.Errorf("decode HTML content: %w", err)}
		}
		title, markdown, err := htmlToMarkdown(decoded, finalURL)
		if err != nil {
			return Document{}, &Error{Kind: ErrorInvalidContent, URL: document.URL, ContentType: mediaType, Err: fmt.Errorf("parse HTML content: %w", err)}
		}
		document.Title = title
		document.Markdown = markdown
	case "text/markdown", "text/plain":
		decoded, err := decodeText(content, contentType)
		if err != nil {
			return Document{}, &Error{Kind: ErrorInvalidContent, URL: document.URL, ContentType: mediaType, Err: fmt.Errorf("decode text content: %w", err)}
		}
		document.Markdown = normalizeTextDocument(decoded)
	case "application/json":
		decoded, err := decodeText(content, contentType)
		if err != nil {
			return Document{}, &Error{Kind: ErrorInvalidContent, URL: document.URL, ContentType: mediaType, Err: fmt.Errorf("decode JSON content: %w", err)}
		}
		var formatted bytes.Buffer
		if err := json.Indent(&formatted, []byte(strings.TrimSpace(decoded)), "", "  "); err != nil && !partial {
			return Document{}, &Error{Kind: ErrorInvalidContent, URL: document.URL, ContentType: mediaType, Err: fmt.Errorf("parse JSON content: %w", err)}
		}
		jsonContent := formatted.String()
		if jsonContent == "" {
			jsonContent = strings.TrimSpace(decoded)
		}
		document.Markdown = "```json\n" + jsonContent + "\n```"
	default:
		return Document{}, &Error{Kind: ErrorUnsupportedContent, URL: document.URL, ContentType: mediaType, Err: fmt.Errorf("web content type %q is not supported", mediaType)}
	}
	if strings.TrimSpace(document.Markdown) == "" {
		return Document{}, &Error{Kind: ErrorEmptyContent, URL: document.URL, ContentType: mediaType, Err: fmt.Errorf("web page contains no readable content")}
	}
	return document, nil
}

func normalizedMediaType(contentType string, content []byte) string {
	if parsed, _, err := mime.ParseMediaType(contentType); err == nil && parsed != "" {
		return strings.ToLower(parsed)
	}
	detected, _, err := mime.ParseMediaType(http.DetectContentType(content))
	if err != nil {
		return "application/octet-stream"
	}
	return strings.ToLower(detected)
}

func decodeText(content []byte, contentType string) (string, error) {
	reader, err := charset.NewReader(bytes.NewReader(content), contentType)
	if err != nil {
		return "", err
	}
	decoded, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(string(decoded), "\r\n", "\n"), nil
}

func normalizeTextDocument(content string) string {
	content = strings.ReplaceAll(content, "\r", "\n")
	lines := strings.Split(content, "\n")
	for index := range lines {
		lines[index] = strings.TrimRight(lines[index], " \t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
