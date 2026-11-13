package encode

import (
	"fmt"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Profile string

const (
	AlphaProRes Profile = "alpha-prores"
	AlphaHEVC   Profile = "alpha-hevc"
	AlphaVP9    Profile = "alpha-vp9"
	BlackH264   Profile = "black-h264"
)

type FFmpegProfile struct {
	Input  []string
	Output []string
	Filter string
}

type Options struct {
	FFmpeg        string
	Output        string
	Video         string
	VideoOffset   float64
	Width         int
	Height        int
	FPS           float64
	OutputFPS     string
	Profile       Profile
	CreationTime  time.Time
	smpteTimecode string
	FFmpegProfile FFmpegProfile
	FFmpegArgs    []string
	Transparent   bool
}

func (o Options) validate() (Options, error) {
	if o.FFmpeg == "" {
		o.FFmpeg = "ffmpeg"
	}
	if o.Output == "" {
		return o, fmt.Errorf("output path is required")
	}
	if o.Width <= 0 || o.Height <= 0 {
		return o, fmt.Errorf("frame size must be positive")
	}
	maxInt := int(^uint(0) >> 1)
	if o.Width > maxInt/o.Height/4 {
		return o, fmt.Errorf("frame size is too large")
	}
	if o.FPS <= 0 || math.IsNaN(o.FPS) || math.IsInf(o.FPS, 0) {
		return o, fmt.Errorf("render frame rate must be finite and positive")
	}
	if err := ValidateOutputFPS(o.OutputFPS); err != nil {
		return o, err
	}
	if !o.CreationTime.IsZero() {
		frameRate := o.OutputFPS
		if frameRate == "" {
			frameRate = strconv.FormatFloat(o.FPS, 'f', -1, 64)
		}
		timecode, err := smpteTimecode(o.CreationTime, frameRate)
		if err != nil {
			return o, fmt.Errorf("generate SMPTE timecode: %w", err)
		}
		o.smpteTimecode = timecode
	}
	if math.IsNaN(o.VideoOffset) || math.IsInf(o.VideoOffset, 0) {
		return o, fmt.Errorf("video offset must be finite")
	}
	if o.Video == "" && o.VideoOffset != 0 {
		return o, fmt.Errorf("video offset requires a source video")
	}
	if o.Video != "" {
		videoInfo, err := os.Stat(o.Video)
		if err != nil {
			return o, fmt.Errorf("open source video %q: %w", o.Video, err)
		}
		if !videoInfo.Mode().IsRegular() {
			return o, fmt.Errorf("source video %q is not a regular file", o.Video)
		}
		outputAbs, err := filepath.Abs(o.Output)
		if err != nil {
			return o, fmt.Errorf("resolve output path %q: %w", o.Output, err)
		}
		videoAbs, err := filepath.Abs(o.Video)
		if err != nil {
			return o, fmt.Errorf("resolve source video path %q: %w", o.Video, err)
		}
		outputInfo, statErr := os.Stat(outputAbs)
		if outputAbs == videoAbs || statErr == nil && os.SameFile(outputInfo, videoInfo) {
			return o, fmt.Errorf("output path must differ from source video")
		}
		if !o.Transparent {
			return o, fmt.Errorf("video composition requires a transparent overlay background")
		}
	}
	if o.Profile == "" {
		if o.Transparent {
			o.Profile = AlphaProRes
		} else {
			o.Profile = BlackH264
		}
	}
	switch o.Profile {
	case AlphaProRes, AlphaHEVC, AlphaVP9:
		if !o.Transparent {
			return o, fmt.Errorf("%s requires a transparent background", o.Profile)
		}
	case BlackH264:
		if o.Transparent && o.Video == "" {
			return o, fmt.Errorf("black-h264 cannot preserve a transparent background")
		}
		if (o.Width%2 != 0 || o.Height%2 != 0) && !hasPixelFormatOverride(o.FFmpegProfile.Output) && !hasPixelFormatOverride(o.FFmpegArgs) {
			return o, fmt.Errorf("black-h264 requires even frame dimensions for yuv420p output")
		}
	default:
		return o, fmt.Errorf("unknown encoding profile %q", o.Profile)
	}
	return o, nil
}

func ValidateOutputFPS(value string) error {
	if value == "" {
		return nil
	}
	rate, ok := new(big.Rat).SetString(value)
	if !ok || rate.Sign() <= 0 {
		return fmt.Errorf("output frame rate must be positive (for example 29.97 or 30000/1001)")
	}
	return nil
}

func hasPixelFormatOverride(args []string) bool {
	for _, arg := range args {
		if arg == "-pix_fmt" || strings.HasPrefix(arg, "-pix_fmt=") || strings.HasPrefix(arg, "-pix_fmt:") {
			return true
		}
	}
	return false
}
