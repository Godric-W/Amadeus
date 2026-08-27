package agentsmd

import "testing"

func TestDocumentRevisionNormalizesLineEndings(t *testing.T) {
	lf, err := newDocument(SourceProject, "/workspace/AGENTS.md", "/workspace", ".", "first\nsecond\n")
	if err != nil {
		t.Fatal(err)
	}
	crlf, err := newDocument(SourceProject, "/workspace/AGENTS.md", "/workspace", ".", "first\r\nsecond\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if lf.SHA256 != crlf.SHA256 || lf.Content != crlf.Content {
		t.Fatalf("line endings changed document identity: lf=%#v crlf=%#v", lf, crlf)
	}
}
