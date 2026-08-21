package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Godric-W/Amadeus/internal/imageprep"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/policy"
	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/tool"
	"github.com/Godric-W/Amadeus/internal/workspace"
)

type ViewImageOptions struct {
	FileSystemPolicy *project.FileSystemPolicy
	ModelInfo        llm.ModelInfo
	ImagePreparation imageprep.Options
}

type ViewImage struct {
	reader    *workspace.Reader
	modelInfo llm.ModelInfo
	processor *imageprep.Processor
	spec      tool.ToolSpec
}

type viewImageArguments struct {
	Path   string           `json:"path"`
	Detail imageprep.Detail `json:"detail,omitempty"`
}

type preparedViewImage struct {
	arguments viewImageArguments
	resolved  project.ResolvedPath
}

func NewViewImage(root project.Root, options ViewImageOptions) (*ViewImage, error) {
	if root.Path() == "" {
		return nil, errors.New("view_image project root is empty")
	}
	modelInfo := options.ModelInfo.Normalized()
	reader, err := workspace.NewReaderWithPolicy(root, options.FileSystemPolicy)
	if options.FileSystemPolicy == nil {
		reader, err = workspace.NewReader(root)
	}
	if err != nil {
		return nil, err
	}
	return &ViewImage{
		reader: reader, modelInfo: modelInfo,
		processor: imageprep.NewProcessor(options.ImagePreparation),
		spec:      viewImageSpec(modelInfo.SupportsOriginalImageDetail),
	}, nil
}

func (viewImage *ViewImage) Spec() tool.ToolSpec { return viewImage.spec.Clone() }

func (viewImage *ViewImage) SupportsParallelToolCalls() bool { return true }

func (viewImage *ViewImage) ValidateInput(_ tool.ToolUseContext, invocation tool.Invocation) error {
	if !viewImage.modelInfo.SupportsInput(llm.InputModalityImage) {
		return errors.New("view_image is unavailable because the current model does not support image input")
	}
	arguments, err := decodeViewImageArguments(invocation.Call.Payload)
	if err != nil {
		return err
	}
	if arguments.Detail == imageprep.DetailOriginal && !viewImage.modelInfo.SupportsOriginalImageDetail {
		return errors.New("view_image detail original is unsupported by the current model")
	}
	return nil
}

func (viewImage *ViewImage) Prepare(_ tool.ToolUseContext, invocation tool.Invocation) (tool.PreparedToolUse, error) {
	arguments, err := decodeViewImageArguments(invocation.Call.Payload)
	if err != nil {
		return tool.PreparedToolUse{}, err
	}
	resolved, err := viewImage.reader.ResolveExistingTarget(arguments.Path, project.PathFile)
	if err != nil {
		return tool.PreparedToolUse{}, err
	}
	permission := tool.AllowPermission()
	if resolved.RootSource == project.RootSourceHost {
		grant := policy.ReadDirectoryGrant(filepath.Dir(resolved.Canonical))
		request, requestErr := policy.NewApprovalRequestForPurpose(invocation.Call.ID, invocation.Call.Name, invocation.Call.Payload, policy.ApprovalPurposePermission, policy.CommandRiskModerate, policy.ApprovalCause{Kind: policy.ApprovalCauseFilesystemRead, Code: "outside_workspace", Detail: resolved.Canonical})
		if requestErr != nil {
			return tool.PreparedToolUse{}, requestErr
		}
		request.Path = resolved.Canonical
		request.Presentation = policy.ReadDirectoryApprovalPresentation("Read image", "image", resolved.Canonical)
		permission = tool.PermissionEvaluation{Decision: tool.PermissionAsk, Request: &request, Grant: grant}
	}
	return tool.PreparedToolUse{Invocation: invocation, Input: arguments, State: preparedViewImage{arguments: arguments, resolved: resolved}, Permission: permission}, nil
}

func (viewImage *ViewImage) Execute(toolContext tool.ToolUseContext, prepared tool.PreparedToolUse) (tool.ToolResult, error) {
	if !viewImage.modelInfo.SupportsInput(llm.InputModalityImage) {
		return tool.ToolResult{}, errors.New("view_image is unavailable because the current model does not support image input")
	}
	state, ok := prepared.State.(preparedViewImage)
	if !ok {
		return tool.ToolResult{}, errors.New("view_image preparation state is invalid")
	}
	if state.arguments.Detail == imageprep.DetailOriginal && !viewImage.modelInfo.SupportsOriginalImageDetail {
		return tool.ToolResult{}, errors.New("view_image detail original is unsupported by the current model")
	}
	resolved, err := viewImage.reader.ResolveExistingTarget(state.arguments.Path, project.PathFile)
	if err != nil {
		return tool.ToolResult{}, err
	}
	if resolved.Canonical != state.resolved.Canonical || resolved.RootSource != state.resolved.RootSource {
		return tool.ToolResult{}, errors.New("view_image target changed after permission evaluation")
	}

	content, err := readBoundedImage(toolContext.Context, resolved.Canonical, viewImage.processor.SourceLimits().MaxBytes)
	if err != nil {
		return tool.ToolResult{}, fmt.Errorf("read image %q: %w", state.arguments.Path, err)
	}
	preparedImage, err := viewImage.processor.Prepare(toolContext.Context, content, state.arguments.Detail)
	if err != nil {
		return tool.ToolResult{}, fmt.Errorf("prepare image %q: %w", state.arguments.Path, err)
	}
	metadata := viewImageMetadata(state.arguments.Path, resolved.Canonical, preparedImage)
	return tool.ToolResult{
		ToolName: "view_image",
		Text:     fmt.Sprintf("Viewed %s (%s, detail=%s).", state.arguments.Path, preparedImage.Summary(), preparedImage.Detail),
		Parts:    []tool.ContentPart{{Kind: tool.ContentImage, MediaType: preparedImage.PreparedMediaType, Data: preparedImage.Base64, Detail: string(preparedImage.Detail)}},
		Data:     metadata,
		Metadata: metadata,
		Display: tool.ToolDisplayResult{
			Kind: tool.ToolDisplayMedia, Title: state.arguments.Path,
			Summary: preparedImage.Summary(), Data: metadata,
		},
	}, nil
}

func decodeViewImageArguments(payload json.RawMessage) (viewImageArguments, error) {
	var arguments viewImageArguments
	if err := decodeArguments(payload, &arguments); err != nil {
		return viewImageArguments{}, err
	}
	arguments.Path = strings.TrimSpace(arguments.Path)
	if arguments.Path == "" {
		return viewImageArguments{}, errors.New("view_image path is empty")
	}
	if arguments.Detail == "" {
		arguments.Detail = imageprep.DetailHigh
	}
	if !arguments.Detail.Valid() {
		return viewImageArguments{}, fmt.Errorf("view_image detail %q is invalid", arguments.Detail)
	}
	return arguments, nil
}

func readBoundedImage(ctx context.Context, path string, maximum int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("image target is not a regular file")
	}
	if info.Size() > maximum {
		return nil, fmt.Errorf("image size %d exceeds limit %d", info.Size(), maximum)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	content, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > maximum {
		return nil, fmt.Errorf("image size exceeds limit %d", maximum)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return content, nil
}

func viewImageMetadata(displayPath, canonicalPath string, image imageprep.PreparedImage) map[string]any {
	return map[string]any{
		"path": displayPath, "canonical_path": canonicalPath, "detail": image.Detail,
		"source_media_type": image.SourceMediaType, "prepared_media_type": image.PreparedMediaType,
		"source_width": image.SourceWidth, "source_height": image.SourceHeight,
		"prepared_width": image.PreparedWidth, "prepared_height": image.PreparedHeight,
		"source_bytes": image.SourceBytes, "prepared_bytes": image.PreparedBytes,
	}
}

func viewImageSpec(supportsOriginal bool) tool.ToolSpec {
	detail := `{"type":"string","enum":["high"],"default":"high"}`
	if supportsOriginal {
		detail = `{"type":"string","enum":["high","original"],"default":"high"}`
	}
	return tool.ToolSpec{
		Name:        "view_image",
		Description: "Read and prepare a bounded PNG, JPEG, WebP, or static GIF from the local filesystem for image-capable model input.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","minLength":1},"detail":` + detail + `},"required":["path"],"additionalProperties":false}`),
		SideEffect:  tool.SideEffectRead,
		Idempotent:  true,
	}
}

var _ tool.ToolDefinition = (*ViewImage)(nil)
