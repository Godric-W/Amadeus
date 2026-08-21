package imageprep

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strings"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

func Prepare(ctx context.Context, source []byte, detail Detail, options Options) (PreparedImage, error) {
	if ctx == nil {
		return PreparedImage{}, errors.New("image preparation context is nil")
	}
	if !detail.Valid() {
		return PreparedImage{}, fmt.Errorf("image detail %q is invalid", detail)
	}
	options = options.normalized()
	if int64(len(source)) > options.Source.MaxBytes {
		return PreparedImage{}, fmt.Errorf("image size %d exceeds limit %d", len(source), options.Source.MaxBytes)
	}
	if err := ctx.Err(); err != nil {
		return PreparedImage{}, err
	}

	configuration, format, err := image.DecodeConfig(bytes.NewReader(source))
	if err != nil {
		return PreparedImage{}, fmt.Errorf("decode image configuration: %w", err)
	}
	mediaType, err := mediaTypeForFormat(format)
	if err != nil {
		return PreparedImage{}, err
	}
	if configuration.Width <= 0 || configuration.Height <= 0 || configuration.Width > options.Source.MaxDimension || configuration.Height > options.Source.MaxDimension || int64(configuration.Width)*int64(configuration.Height) > options.Source.MaxPixels {
		return PreparedImage{}, fmt.Errorf("image dimensions %dx%d exceed source limits", configuration.Width, configuration.Height)
	}

	var decoded image.Image
	if format == "gif" {
		animated, decodeErr := gif.DecodeAll(bytes.NewReader(source))
		if decodeErr != nil {
			return PreparedImage{}, fmt.Errorf("decode GIF: %w", decodeErr)
		}
		if len(animated.Image) != 1 {
			return PreparedImage{}, fmt.Errorf("%w: %d frames", ErrAnimatedGIF, len(animated.Image))
		}
		decoded = animated.Image[0]
	}

	limits := options.High
	if detail == DetailOriginal {
		limits = options.Original
	}
	preparedWidth, preparedHeight := OutputDimensions(configuration.Width, configuration.Height, limits)
	needsResize := preparedWidth != configuration.Width || preparedHeight != configuration.Height
	canPreserve := !needsResize && (format == "png" || format == "jpeg" || format == "webp")
	preparedBytes := source
	preparedMediaType := mediaType
	if !canPreserve {
		if decoded == nil {
			decoded, _, err = image.Decode(bytes.NewReader(source))
			if err != nil {
				return PreparedImage{}, fmt.Errorf("decode image: %w", err)
			}
		}
		if err := ctx.Err(); err != nil {
			return PreparedImage{}, err
		}
		if needsResize {
			resized := image.NewNRGBA(image.Rect(0, 0, preparedWidth, preparedHeight))
			draw.ApproxBiLinear.Scale(resized, resized.Bounds(), decoded, decoded.Bounds(), draw.Over, nil)
			decoded = resized
		}
		preparedBytes, preparedMediaType, err = encodePrepared(decoded, format)
		if err != nil {
			return PreparedImage{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return PreparedImage{}, err
	}
	return PreparedImage{
		Detail: detail, SourceMediaType: mediaType, PreparedMediaType: preparedMediaType,
		SourceWidth: configuration.Width, SourceHeight: configuration.Height,
		PreparedWidth: preparedWidth, PreparedHeight: preparedHeight,
		SourceBytes: len(source), PreparedBytes: len(preparedBytes),
		Bytes: append([]byte(nil), preparedBytes...), Base64: base64.StdEncoding.EncodeToString(preparedBytes),
	}, nil
}

func (processor *Processor) Prepare(ctx context.Context, source []byte, detail Detail) (PreparedImage, error) {
	if processor == nil {
		return PreparedImage{}, errors.New("image processor is nil")
	}
	return Prepare(ctx, source, detail, processor.options)
}

func mediaTypeForFormat(format string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "png":
		return "image/png", nil
	case "jpeg":
		return "image/jpeg", nil
	case "gif":
		return "image/gif", nil
	case "webp":
		return "image/webp", nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnsupportedImage, format)
	}
}

func encodePrepared(source image.Image, sourceFormat string) ([]byte, string, error) {
	var output bytes.Buffer
	if sourceFormat == "jpeg" {
		if err := jpeg.Encode(&output, source, &jpeg.Options{Quality: 90}); err != nil {
			return nil, "", fmt.Errorf("encode JPEG: %w", err)
		}
		return output.Bytes(), "image/jpeg", nil
	}
	if err := png.Encode(&output, source); err != nil {
		return nil, "", fmt.Errorf("encode PNG: %w", err)
	}
	return output.Bytes(), "image/png", nil
}
