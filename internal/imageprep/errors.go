package imageprep

import "errors"

var (
	ErrAnimatedGIF      = errors.New("animated GIF is unsupported")
	ErrUnsupportedImage = errors.New("unsupported image format")
)
