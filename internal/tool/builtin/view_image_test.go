package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/tool"
)

func TestViewImageReturnsRealImagePartAndMetadata(t *testing.T) {
	rootPath := t.TempDir()
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 3, 2))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootPath, "image.png"), encoded.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	root, _ := project.NewRoot(rootPath)
	viewer, err := NewViewImage(root, ViewImageOptions{MaxBytes: 1024, MaxDimension: 10, MaxPixels: 100})
	if err != nil {
		t.Fatal(err)
	}
	result, err := executePreparedTool(t, context.Background(), viewer, json.RawMessage(`{"path":"image.png"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Parts) != 1 || result.Parts[0].Kind != tool.ContentImage || result.Parts[0].MediaType != "image/png" || result.Parts[0].Data == "" || result.Metadata["width"] != 3 || result.Metadata["height"] != 2 {
		t.Fatalf("unexpected image result: %#v", result)
	}
}

func TestViewImageRejectsAnimatedGIFAndEscape(t *testing.T) {
	rootPath := t.TempDir()
	palette := color.Palette{color.Black, color.White}
	animation := &gif.GIF{Image: []*image.Paletted{
		image.NewPaletted(image.Rect(0, 0, 1, 1), palette), image.NewPaletted(image.Rect(0, 0, 1, 1), palette),
	}, Delay: []int{0, 0}}
	var encoded bytes.Buffer
	if err := gif.EncodeAll(&encoded, animation); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootPath, "animated.gif"), encoded.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	root, _ := project.NewRoot(rootPath)
	viewer, err := NewViewImage(root, ViewImageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []json.RawMessage{json.RawMessage(`{"path":"animated.gif"}`), json.RawMessage(`{"path":"../outside.png"}`)} {
		if _, err := executePreparedTool(t, context.Background(), viewer, input); err == nil {
			t.Fatalf("expected rejection for %s", input)
		}
	}
}
