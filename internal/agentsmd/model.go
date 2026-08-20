package agentsmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

type Source string

const (
	SourceUser    Source = "user"
	SourceProject Source = "project"
)

type Document struct {
	Source    Source `json:"source"`
	Path      string `json:"path"`
	Root      string `json:"root,omitempty"`
	Directory string `json:"directory,omitempty"`
	SHA256    string `json:"sha256"`
	Content   string `json:"-"`
}

func newDocument(source Source, path, root, directory, content string) (Document, error) {
	document := Document{
		Source: source, Path: filepath.Clean(path), Root: filepath.Clean(root),
		Directory: filepath.ToSlash(filepath.Clean(directory)), Content: content,
	}
	if source == SourceUser {
		document.Root = ""
		document.Directory = ""
	}
	digest := sha256.Sum256([]byte(content))
	document.SHA256 = hex.EncodeToString(digest[:])
	if err := document.Validate(); err != nil {
		return Document{}, err
	}
	return document, nil
}

func (document Document) Validate() error {
	if document.Source != SourceUser && document.Source != SourceProject {
		return errors.New("AGENTS.md source is invalid")
	}
	if !filepath.IsAbs(document.Path) || filepath.Clean(document.Path) != document.Path {
		return errors.New("AGENTS.md path must be normalized and absolute")
	}
	if document.Source == SourceProject {
		if !filepath.IsAbs(document.Root) || filepath.Clean(document.Root) != document.Root {
			return errors.New("project AGENTS.md root must be normalized and absolute")
		}
		if document.Directory == "" || strings.HasPrefix(document.Directory, "../") || document.Directory == ".." {
			return errors.New("project AGENTS.md directory is invalid")
		}
	}
	if !utf8.ValidString(document.Content) || strings.TrimSpace(document.Content) == "" {
		return errors.New("AGENTS.md content is empty or invalid UTF-8")
	}
	digest := sha256.Sum256([]byte(document.Content))
	if document.SHA256 != hex.EncodeToString(digest[:]) {
		return errors.New("AGENTS.md content hash is invalid")
	}
	return nil
}

type LoadedAgentsMd struct {
	Documents []Document `json:"documents,omitempty"`
	Revision  string     `json:"revision"`
}

func newLoaded(documents []Document) LoadedAgentsMd {
	documents = append([]Document(nil), documents...)
	sort.Slice(documents, func(left, right int) bool {
		if documents[left].Source != documents[right].Source {
			return documents[left].Source == SourceUser
		}
		if documents[left].Root != documents[right].Root {
			return documents[left].Root < documents[right].Root
		}
		leftDepth := strings.Count(documents[left].Directory, "/")
		rightDepth := strings.Count(documents[right].Directory, "/")
		if leftDepth != rightDepth {
			return leftDepth < rightDepth
		}
		return documents[left].Path < documents[right].Path
	})
	type revisionDocument struct {
		Source    Source `json:"source"`
		Path      string `json:"path"`
		Root      string `json:"root,omitempty"`
		Directory string `json:"directory,omitempty"`
		SHA256    string `json:"sha256"`
	}
	revisionPayload := make([]revisionDocument, 0, len(documents))
	for _, document := range documents {
		revisionPayload = append(revisionPayload, revisionDocument{
			Source: document.Source, Path: document.Path, Root: document.Root,
			Directory: document.Directory, SHA256: document.SHA256,
		})
	}
	encoded, _ := json.Marshal(revisionPayload)
	digest := sha256.Sum256(encoded)
	return LoadedAgentsMd{Documents: documents, Revision: hex.EncodeToString(digest[:])}
}

func (loaded LoadedAgentsMd) Clone() LoadedAgentsMd {
	return LoadedAgentsMd{Documents: append([]Document(nil), loaded.Documents...), Revision: loaded.Revision}
}

func (loaded LoadedAgentsMd) Render() string {
	if len(loaded.Documents) == 0 {
		return ""
	}
	metadata, _ := json.Marshal(struct {
		Type      string     `json:"type"`
		Revision  string     `json:"revision"`
		Documents []Document `json:"documents"`
	}{Type: "amadeus.agents_md.v1", Revision: loaded.Revision, Documents: loaded.Documents})
	parts := []string{"## AGENTS.md Instructions", string(metadata)}
	for _, document := range loaded.Documents {
		parts = append(parts, "Instructions from "+document.Path+":\n"+strings.TrimSpace(document.Content))
	}
	return strings.Join(parts, "\n\n")
}
