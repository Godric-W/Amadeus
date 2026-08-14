package builtin

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Godric-W/Amadeus/internal/policy"
	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/tool"
	"github.com/Godric-W/Amadeus/internal/workspace"
	_ "golang.org/x/image/webp"
)

type ViewImageOptions struct {
	MaxBytes         int64
	MaxDimension     int
	MaxPixels        int64
	FileSystemPolicy *project.FileSystemPolicy
}

type ViewImage struct {
	reader  *workspace.Reader
	options ViewImageOptions
}

type viewImageArguments struct {
	Path string `json:"path"`
}

type preparedImage struct {
	arguments viewImageArguments
	resolved  project.ResolvedPath
}

func NewViewImage(root project.Root, options ViewImageOptions) (*ViewImage, error) {
	if root.Path() == "" {
		return nil, errors.New("view_image project root is empty")
	}
	if options.MaxBytes <= 0 {
		options.MaxBytes = 20 << 20
	}
	if options.MaxDimension <= 0 {
		options.MaxDimension = 16_384
	}
	if options.MaxPixels <= 0 {
		options.MaxPixels = 64_000_000
	}
	reader, err := workspace.NewReaderWithPolicy(root, options.FileSystemPolicy)
	if options.FileSystemPolicy == nil {
		reader, err = workspace.NewReader(root)
	}
	if err != nil {
		return nil, err
	}
	return &ViewImage{reader: reader, options: options}, nil
}

func (viewImage *ViewImage) Spec() tool.ToolSpec { return viewImageSpec() }

func (viewImage *ViewImage) SupportsParallelToolCalls() bool { return true }

func (viewImage *ViewImage) ValidateInput(_ tool.ToolUseContext, invocation tool.Invocation) error {
	var arguments viewImageArguments
	if err := decodeArguments(invocation.Call.Payload, &arguments); err != nil {
		return err
	}
	if strings.TrimSpace(arguments.Path) == "" {
		return errors.New("view_image path is empty")
	}
	return nil
}

func (viewImage *ViewImage) Prepare(_ tool.ToolUseContext, invocation tool.Invocation) (tool.PreparedToolUse, error) {
	var arguments viewImageArguments
	if err := decodeArguments(invocation.Call.Payload, &arguments); err != nil {
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
	return tool.PreparedToolUse{Invocation: invocation, Input: arguments, State: preparedImage{arguments: arguments, resolved: resolved}, Permission: permission}, nil
}

func (viewImage *ViewImage) Execute(toolContext tool.ToolUseContext, prepared tool.PreparedToolUse) (tool.ToolResult, error) {
	state, ok := prepared.State.(preparedImage)
	if !ok {
		return tool.ToolResult{}, errors.New("view_image preparation state is invalid")
	}
	arguments, resolved := state.arguments, state.resolved
	file, err := os.Open(resolved.Canonical)
	if err != nil {
		return tool.ToolResult{}, fmt.Errorf("open image %q: %w", arguments.Path, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return tool.ToolResult{}, err
	}
	if info.Size() > viewImage.options.MaxBytes {
		return tool.ToolResult{}, fmt.Errorf("view_image size %d exceeds limit %d", info.Size(), viewImage.options.MaxBytes)
	}
	content, err := io.ReadAll(io.LimitReader(file, viewImage.options.MaxBytes+1))
	if err != nil {
		return tool.ToolResult{}, err
	}
	if int64(len(content)) > viewImage.options.MaxBytes {
		return tool.ToolResult{}, fmt.Errorf("view_image size exceeds limit %d", viewImage.options.MaxBytes)
	}
	if err := toolContext.Context.Err(); err != nil {
		return tool.ToolResult{}, err
	}
	configuration, format, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil {
		return tool.ToolResult{}, fmt.Errorf("decode image %q: %w", arguments.Path, err)
	}
	mediaType, err := supportedImageMediaType(format, filepath.Ext(arguments.Path))
	if err != nil {
		return tool.ToolResult{}, err
	}
	if configuration.Width <= 0 || configuration.Height <= 0 || configuration.Width > viewImage.options.MaxDimension || configuration.Height > viewImage.options.MaxDimension || int64(configuration.Width)*int64(configuration.Height) > viewImage.options.MaxPixels {
		return tool.ToolResult{}, fmt.Errorf("view_image dimensions %dx%d exceed limits", configuration.Width, configuration.Height)
	}
	if format == "gif" {
		decoded, err := gif.DecodeAll(bytes.NewReader(content))
		if err != nil {
			return tool.ToolResult{}, fmt.Errorf("decode GIF %q: %w", arguments.Path, err)
		}
		if len(decoded.Image) != 1 {
			return tool.ToolResult{}, fmt.Errorf("view_image only supports static GIF; %q has %d frames", arguments.Path, len(decoded.Image))
		}
	}
	encoded := base64.StdEncoding.EncodeToString(content)
	media := map[string]any{"path": resolved.Canonical, "media_type": mediaType, "width": configuration.Width, "height": configuration.Height, "bytes": len(content)}
	return tool.ToolResult{
		ToolName: "view_image", Text: fmt.Sprintf("viewed %s (%dx%d, %s)", arguments.Path, configuration.Width, configuration.Height, mediaType),
		Parts: []tool.ContentPart{{Kind: tool.ContentImage, MediaType: mediaType, Data: encoded}},
		Data:  media, Display: tool.ToolDisplayResult{Kind: tool.ToolDisplayMedia, Title: arguments.Path, Summary: fmt.Sprintf("%dx%d %s", configuration.Width, configuration.Height, mediaType), Data: media},
		Metadata: map[string]any{"path": arguments.Path, "media_type": mediaType, "width": configuration.Width, "height": configuration.Height, "bytes": len(content)},
	}, nil
}

func supportedImageMediaType(format, extension string) (string, error) {
	switch strings.ToLower(format) {
	case "png":
		return "image/png", nil
	case "jpeg":
		return "image/jpeg", nil
	case "gif":
		return "image/gif", nil
	case "webp":
		return "image/webp", nil
	default:
		return "", fmt.Errorf("view_image format %q (%s) is unsupported", format, extension)
	}
}

func viewImageSpec() tool.ToolSpec {
	return tool.ToolSpec{
		Name: "view_image", Description: "Read a bounded PNG, JPEG, WebP, or static GIF from the project and return it as a real image content part.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","minLength":1}},"required":["path"],"additionalProperties":false}`),
		SideEffect:  tool.SideEffectRead, Idempotent: true,
	}
}

var _ tool.ToolDefinition = (*ViewImage)(nil)
