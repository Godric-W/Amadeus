package builtin

import (
	"bytes"
	"context"
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

func (viewImage *ViewImage) Call(ctx context.Context, invocation tool.Invocation) (tool.Output, error) {
	call := invocation.Call
	var arguments viewImageArguments
	if err := decodeArguments(call.Payload, &arguments); err != nil {
		return tool.Output{}, err
	}
	if strings.TrimSpace(arguments.Path) == "" {
		return tool.Output{}, errors.New("view_image path is empty")
	}
	resolved, err := viewImage.reader.ResolveExistingTarget(arguments.Path, project.PathFile)
	if err != nil {
		return tool.Output{}, err
	}
	file, err := os.Open(resolved.Canonical)
	if err != nil {
		return tool.Output{}, fmt.Errorf("open image %q: %w", arguments.Path, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return tool.Output{}, err
	}
	if info.Size() > viewImage.options.MaxBytes {
		return tool.Output{}, fmt.Errorf("view_image size %d exceeds limit %d", info.Size(), viewImage.options.MaxBytes)
	}
	content, err := io.ReadAll(io.LimitReader(file, viewImage.options.MaxBytes+1))
	if err != nil {
		return tool.Output{}, err
	}
	if int64(len(content)) > viewImage.options.MaxBytes {
		return tool.Output{}, fmt.Errorf("view_image size exceeds limit %d", viewImage.options.MaxBytes)
	}
	if err := ctx.Err(); err != nil {
		return tool.Output{}, err
	}
	configuration, format, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil {
		return tool.Output{}, fmt.Errorf("decode image %q: %w", arguments.Path, err)
	}
	mediaType, err := supportedImageMediaType(format, filepath.Ext(arguments.Path))
	if err != nil {
		return tool.Output{}, err
	}
	if configuration.Width <= 0 || configuration.Height <= 0 || configuration.Width > viewImage.options.MaxDimension || configuration.Height > viewImage.options.MaxDimension || int64(configuration.Width)*int64(configuration.Height) > viewImage.options.MaxPixels {
		return tool.Output{}, fmt.Errorf("view_image dimensions %dx%d exceed limits", configuration.Width, configuration.Height)
	}
	if format == "gif" {
		decoded, err := gif.DecodeAll(bytes.NewReader(content))
		if err != nil {
			return tool.Output{}, fmt.Errorf("decode GIF %q: %w", arguments.Path, err)
		}
		if len(decoded.Image) != 1 {
			return tool.Output{}, fmt.Errorf("view_image only supports static GIF; %q has %d frames", arguments.Path, len(decoded.Image))
		}
	}
	encoded := base64.StdEncoding.EncodeToString(content)
	return tool.Output{
		ToolName: "view_image", Text: fmt.Sprintf("viewed %s (%dx%d, %s)", arguments.Path, configuration.Width, configuration.Height, mediaType),
		Parts:    []tool.ContentPart{{Kind: tool.ContentImage, MediaType: mediaType, Data: encoded}},
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

var _ tool.Tool = (*ViewImage)(nil)
