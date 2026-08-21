package imageprep

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"testing"
)

func TestPreparePreservesBoundedPNG(t *testing.T) {
	source := encodePNG(t, image.NewRGBA(image.Rect(0, 0, 64, 32)))
	prepared, err := Prepare(context.Background(), source, DetailHigh, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.PreparedMediaType != "image/png" || prepared.SourceWidth != 64 || prepared.PreparedWidth != 64 || !bytes.Equal(prepared.Bytes, source) || prepared.Base64 == "" {
		t.Fatalf("unexpected prepared PNG: %#v", prepared)
	}
}

func TestPrepareResizesToDimensionAndPatchBudgets(t *testing.T) {
	var source bytes.Buffer
	if err := jpeg.Encode(&source, image.NewRGBA(image.Rect(0, 0, 4000, 2000)), &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	prepared, err := Prepare(context.Background(), source.Bytes(), DetailHigh, Options{Source: SourceLimits{MaxBytes: 64 << 20, MaxDimension: 5000, MaxPixels: 10_000_000}})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.PreparedWidth > 2048 || prepared.PreparedHeight > 2048 || PatchCount(prepared.PreparedWidth, prepared.PreparedHeight) > 2500 {
		t.Fatalf("prepared dimensions exceed high limits: %#v", prepared)
	}
	if prepared.PreparedWidth*prepared.SourceHeight != prepared.PreparedHeight*prepared.SourceWidth {
		t.Fatalf("aspect ratio changed: %#v", prepared)
	}
}

func TestPrepareOriginalUsesLargerBudget(t *testing.T) {
	source := encodePNG(t, image.NewRGBA(image.Rect(0, 0, 3000, 1000)))
	high, err := Prepare(context.Background(), source, DetailHigh, Options{Source: SourceLimits{MaxBytes: 64 << 20, MaxDimension: 5000, MaxPixels: 10_000_000}})
	if err != nil {
		t.Fatal(err)
	}
	original, err := Prepare(context.Background(), source, DetailOriginal, Options{Source: SourceLimits{MaxBytes: 64 << 20, MaxDimension: 5000, MaxPixels: 10_000_000}})
	if err != nil {
		t.Fatal(err)
	}
	if original.PreparedWidth <= high.PreparedWidth || original.PreparedWidth != 3000 || original.PreparedHeight != 1000 {
		t.Fatalf("original detail did not retain larger bounded image: high=%#v original=%#v", high, original)
	}
}

func TestPrepareNormalizesStaticGIFAndRejectsAnimation(t *testing.T) {
	palette := color.Palette{color.Black, color.White}
	frame := image.NewPaletted(image.Rect(0, 0, 2, 2), palette)
	var static bytes.Buffer
	if err := gif.Encode(&static, frame, nil); err != nil {
		t.Fatal(err)
	}
	prepared, err := Prepare(context.Background(), static.Bytes(), DetailHigh, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.SourceMediaType != "image/gif" || prepared.PreparedMediaType != "image/png" {
		t.Fatalf("unexpected static GIF result: %#v", prepared)
	}

	var animated bytes.Buffer
	if err := gif.EncodeAll(&animated, &gif.GIF{Image: []*image.Paletted{frame, frame}, Delay: []int{0, 0}}); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(context.Background(), animated.Bytes(), DetailHigh, Options{}); !errors.Is(err, ErrAnimatedGIF) {
		t.Fatalf("unexpected animated GIF error: %v", err)
	}
}

func TestOutputDimensionsStayWithinPatchBudget(t *testing.T) {
	width, height := OutputDimensions(16_000, 100, PreparationLimits{MaxDimension: 2048, MaxPatches: 2500})
	if width > 2048 || height > 2048 || PatchCount(width, height) > 2500 || width <= 0 || height <= 0 {
		t.Fatalf("invalid output dimensions: %dx%d (%d patches)", width, height, PatchCount(width, height))
	}
}

func TestPrepareSupportsWebPContent(t *testing.T) {
	source, err := os.ReadFile("../../docs/Amadeus_logo.webp")
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := Prepare(context.Background(), source, DetailHigh, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.SourceMediaType != "image/webp" || prepared.PreparedMediaType == "" || prepared.PreparedWidth <= 0 || prepared.PreparedHeight <= 0 {
		t.Fatalf("unexpected WebP preparation: %#v", prepared)
	}
}

func TestPrepareRejectsSourceDimensionAndPixelLimits(t *testing.T) {
	source := encodePNG(t, image.NewRGBA(image.Rect(0, 0, 10, 10)))
	for _, test := range []struct {
		name    string
		options Options
	}{
		{name: "dimension", options: Options{Source: SourceLimits{MaxBytes: 1 << 20, MaxDimension: 9, MaxPixels: 1000}}},
		{name: "pixels", options: Options{Source: SourceLimits{MaxBytes: 1 << 20, MaxDimension: 100, MaxPixels: 99}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Prepare(context.Background(), source, DetailHigh, test.options); err == nil {
				t.Fatal("expected source limit rejection")
			}
		})
	}
}

func encodePNG(t *testing.T, source image.Image) []byte {
	t.Helper()
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}
