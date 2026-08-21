package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/imageprep"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/policy"
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
	viewer, err := NewViewImage(root, ViewImageOptions{
		ModelInfo:        imageModel(false),
		ImagePreparation: imageprep.Options{Source: imageprep.SourceLimits{MaxBytes: 1024, MaxDimension: 10, MaxPixels: 100}},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := executePreparedTool(t, context.Background(), viewer, json.RawMessage(`{"path":"image.png"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Parts) != 1 || result.Parts[0].Kind != tool.ContentImage || result.Parts[0].MediaType != "image/png" || result.Parts[0].Detail != "high" || result.Parts[0].Data == "" {
		t.Fatalf("unexpected image part: %#v", result.Parts)
	}
	if result.Metadata["source_width"] != 3 || result.Metadata["source_height"] != 2 || result.Metadata["prepared_width"] != 3 || result.Metadata["prepared_height"] != 2 {
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
	viewer, err := NewViewImage(root, ViewImageOptions{ModelInfo: imageModel(false)})
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []json.RawMessage{json.RawMessage(`{"path":"animated.gif"}`), json.RawMessage(`{"path":"../outside.png"}`)} {
		if _, err := executePreparedTool(t, context.Background(), viewer, input); err == nil {
			t.Fatalf("expected rejection for %s", input)
		}
	}
}

func TestViewImageSchemaAndExecutionFollowModelCapabilities(t *testing.T) {
	root, err := project.NewRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	textOnly, err := NewViewImage(root, ViewImageOptions{ModelInfo: llm.ModelInfo{InputModalities: []llm.InputModality{llm.InputModalityText}}})
	if err != nil {
		t.Fatal(err)
	}
	invocation := tool.Invocation{Call: tool.NewCall("call", "view_image", json.RawMessage(`{"path":"image.png"}`))}
	if err := textOnly.ValidateInput(tool.ToolUseContext{}, invocation); err == nil || !strings.Contains(err.Error(), "does not support image") {
		t.Fatalf("unexpected text-only validation: %v", err)
	}
	if strings.Contains(string(textOnly.Spec().InputSchema), "original") {
		t.Fatalf("text-only schema exposes original detail: %s", textOnly.Spec().InputSchema)
	}

	highOnly, err := NewViewImage(root, ViewImageOptions{ModelInfo: imageModel(false)})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(highOnly.Spec().InputSchema), "original") {
		t.Fatalf("high-only schema exposes original detail: %s", highOnly.Spec().InputSchema)
	}
	unsupportedOriginal := tool.Invocation{Call: tool.NewCall("call", "view_image", json.RawMessage(`{"path":"image.png","detail":"original"}`))}
	if err := highOnly.ValidateInput(tool.ToolUseContext{}, unsupportedOriginal); err == nil || !strings.Contains(err.Error(), "original") {
		t.Fatalf("unexpected original detail validation: %v", err)
	}

	original, err := NewViewImage(root, ViewImageOptions{ModelInfo: imageModel(true)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(original.Spec().InputSchema), `"original"`) {
		t.Fatalf("original-capable schema omitted original detail: %s", original.Spec().InputSchema)
	}
}

func TestViewImageDetectsContentAndNormalizesStaticGIF(t *testing.T) {
	rootPath := t.TempDir()
	var pngBytes bytes.Buffer
	if err := png.Encode(&pngBytes, image.NewRGBA(image.Rect(0, 0, 2, 1))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootPath, "disguised.jpg"), pngBytes.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	palette := color.Palette{color.Black, color.White}
	var gifBytes bytes.Buffer
	if err := gif.Encode(&gifBytes, image.NewPaletted(image.Rect(0, 0, 2, 1), palette), nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootPath, "static.gif"), gifBytes.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	root, _ := project.NewRoot(rootPath)
	viewer, err := NewViewImage(root, ViewImageOptions{ModelInfo: imageModel(false)})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		path       string
		mediaType  string
		sourceType string
	}{
		{path: "disguised.jpg", mediaType: "image/png", sourceType: "image/png"},
		{path: "static.gif", mediaType: "image/png", sourceType: "image/gif"},
	} {
		result, executeErr := executePreparedTool(t, context.Background(), viewer, json.RawMessage(`{"path":"`+test.path+`"}`))
		if executeErr != nil {
			t.Fatal(executeErr)
		}
		if result.Parts[0].MediaType != test.mediaType || result.Metadata["source_media_type"] != test.sourceType {
			t.Fatalf("unexpected prepared format for %s: %#v", test.path, result)
		}
	}
}

func TestViewImageHonorsCancellationAndSourceLimits(t *testing.T) {
	rootPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootPath, "too-large.png"), []byte("not-an-image"), 0o644); err != nil {
		t.Fatal(err)
	}
	root, _ := project.NewRoot(rootPath)
	viewer, err := NewViewImage(root, ViewImageOptions{ModelInfo: imageModel(false), ImagePreparation: imageprep.Options{Source: imageprep.SourceLimits{MaxBytes: 4}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := executePreparedTool(t, context.Background(), viewer, json.RawMessage(`{"path":"too-large.png"}`)); err == nil || !strings.Contains(err.Error(), "exceeds limit") {
		t.Fatalf("unexpected source limit error: %v", err)
	}

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootPath, "cancel.png"), encoded.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	cancellableViewer, err := NewViewImage(root, ViewImageOptions{ModelInfo: imageModel(false)})
	if err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := executePreparedTool(t, cancelled, cancellableViewer, json.RawMessage(`{"path":"cancel.png"}`)); !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected cancellation error: %v", err)
	}
}

func TestViewImageExternalReadUsesDirectoryApprovalAndSessionGrant(t *testing.T) {
	parent := t.TempDir()
	workspacePath := filepath.Join(parent, "workspace")
	externalPath := filepath.Join(parent, "external")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(externalPath, 0o755); err != nil {
		t.Fatal(err)
	}
	encoded := encodeTestPNG(t, 2, 2)
	for _, name := range []string{"one.png", "two.png"} {
		if err := os.WriteFile(filepath.Join(externalPath, name), encoded, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	root, _ := project.NewRoot(workspacePath)
	filesystem, err := project.NewFileSystemPolicy(project.FileSystemPolicyOptions{CWD: workspacePath, Profile: project.PermissionProfile{ReadHost: true, WorkspaceRoots: []string{workspacePath}}})
	if err != nil {
		t.Fatal(err)
	}
	viewer, err := NewViewImage(root, ViewImageOptions{ModelInfo: imageModel(false), FileSystemPolicy: filesystem})
	if err != nil {
		t.Fatal(err)
	}
	approvals := &testApprovalPort{decision: policy.ApprovalDecision{Outcome: policy.ApprovalAllow, Scope: policy.ApprovalSession, Source: policy.ApprovalSourceUser, Reason: "trusted image directory"}}
	coordinator, err := policy.NewApprovalCoordinator(approvals)
	if err != nil {
		t.Fatal(err)
	}
	ctx := withTestPermissions(withTestApprovalCoordinator(context.Background(), coordinator), policy.NewSessionPermissionContext())
	for _, name := range []string{"one.png", "two.png"} {
		payload, _ := json.Marshal(map[string]any{"path": filepath.Join(externalPath, name)})
		if _, err := executePreparedTool(t, ctx, viewer, payload); err != nil {
			t.Fatalf("view external image %s: %v", name, err)
		}
	}
	if approvals.calls != 1 || len(approvals.requests) != 1 || approvals.requests[0].Path != filepath.Join(externalPath, "one.png") {
		t.Fatalf("unexpected external image approvals: %#v", approvals.requests)
	}
}

func TestViewImageRevalidatesTargetBeforeOpen(t *testing.T) {
	rootPath := t.TempDir()
	imagePath := filepath.Join(rootPath, "image.png")
	if err := os.WriteFile(imagePath, encodeTestPNG(t, 1, 1), 0o644); err != nil {
		t.Fatal(err)
	}
	root, _ := project.NewRoot(rootPath)
	viewer, err := NewViewImage(root, ViewImageOptions{ModelInfo: imageModel(false)})
	if err != nil {
		t.Fatal(err)
	}
	invocation := tool.Invocation{Call: tool.NewCall("call", "view_image", json.RawMessage(`{"path":"image.png"}`))}
	prepared, err := viewer.Prepare(tool.ToolUseContext{}, invocation)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(imagePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(imagePath, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := viewer.Execute(tool.ToolUseContext{Context: context.Background(), Invocation: invocation}, prepared); err == nil {
		t.Fatal("view_image opened a target that changed from file to directory")
	}
}

func imageModel(original bool) llm.ModelInfo {
	return llm.ModelInfo{
		InputModalities:             []llm.InputModality{llm.InputModalityText, llm.InputModalityImage},
		SupportsOriginalImageDetail: original,
	}
}

func encodeTestPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, width, height))); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}
