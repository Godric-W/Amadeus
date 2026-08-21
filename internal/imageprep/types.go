package imageprep

import "fmt"

type Detail string

const (
	DetailHigh     Detail = "high"
	DetailOriginal Detail = "original"
)

func (detail Detail) Valid() bool {
	return detail == DetailHigh || detail == DetailOriginal
}

type SourceLimits struct {
	MaxBytes     int64
	MaxDimension int
	MaxPixels    int64
}

type PreparationLimits struct {
	MaxDimension int
	MaxPatches   int
}

type Options struct {
	Source   SourceLimits
	High     PreparationLimits
	Original PreparationLimits
}

type Processor struct {
	options Options
}

func NewProcessor(options Options) *Processor {
	return &Processor{options: options.normalized()}
}

func (processor *Processor) SourceLimits() SourceLimits {
	if processor == nil {
		return DefaultOptions().Source
	}
	return processor.options.Source
}

func DefaultOptions() Options {
	return Options{
		Source:   SourceLimits{MaxBytes: 20 << 20, MaxDimension: 16_384, MaxPixels: 64_000_000},
		High:     PreparationLimits{MaxDimension: 2_048, MaxPatches: 2_500},
		Original: PreparationLimits{MaxDimension: 6_000, MaxPatches: 10_000},
	}
}

func (options Options) normalized() Options {
	defaults := DefaultOptions()
	if options.Source.MaxBytes <= 0 {
		options.Source.MaxBytes = defaults.Source.MaxBytes
	}
	if options.Source.MaxDimension <= 0 {
		options.Source.MaxDimension = defaults.Source.MaxDimension
	}
	if options.Source.MaxPixels <= 0 {
		options.Source.MaxPixels = defaults.Source.MaxPixels
	}
	if options.High.MaxDimension <= 0 {
		options.High.MaxDimension = defaults.High.MaxDimension
	}
	if options.High.MaxPatches <= 0 {
		options.High.MaxPatches = defaults.High.MaxPatches
	}
	if options.Original.MaxDimension <= 0 {
		options.Original.MaxDimension = defaults.Original.MaxDimension
	}
	if options.Original.MaxPatches <= 0 {
		options.Original.MaxPatches = defaults.Original.MaxPatches
	}
	return options
}

type PreparedImage struct {
	Detail            Detail
	SourceMediaType   string
	PreparedMediaType string
	SourceWidth       int
	SourceHeight      int
	PreparedWidth     int
	PreparedHeight    int
	SourceBytes       int
	PreparedBytes     int
	Bytes             []byte
	Base64            string
}

func (image PreparedImage) Summary() string {
	return fmt.Sprintf("%dx%d → %dx%d, %s", image.SourceWidth, image.SourceHeight, image.PreparedWidth, image.PreparedHeight, image.PreparedMediaType)
}
