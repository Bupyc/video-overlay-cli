package encode

import (
	"fmt"
	"math"
	"math/big"
	"time"
)

func smpteTimecode(start time.Time, frameRate string) (string, error) {
	rate, ok := new(big.Rat).SetString(frameRate)
	if !ok || rate.Sign() <= 0 {
		return "", fmt.Errorf("invalid frame rate %q", frameRate)
	}
	fps, _ := rate.Float64()
	if math.IsInf(fps, 0) || fps > 1_000_000 {
		return "", fmt.Errorf("frame rate %q is too high for SMPTE timecode", frameRate)
	}
	nominal := int(math.Round(fps))
	if nominal < 1 {
		return "", fmt.Errorf("SMPTE timecode requires a frame rate of at least 1 fps")
	}
	dropFrames := 0
	if nominal%30 == 0 {
		ntscRate := big.NewRat(int64(nominal*1000), 1001)
		difference := new(big.Rat).Sub(rate, ntscRate)
		difference.Abs(difference)
		// Treat rounded decimal rates like 29.97 as NTSC drop-frame rates.
		if difference.Cmp(big.NewRat(1, 1000)) < 0 {
			dropFrames = nominal / 30 * 2
		}
	}

	start = start.UTC()
	frame := int(math.Round(float64(start.Nanosecond()) * fps / float64(time.Second)))
	if frame >= nominal {
		start = start.Add(time.Second - time.Duration(start.Nanosecond()))
		frame = 0
	}
	hour, minute, second := start.Clock()
	separator := ":"
	if dropFrames > 0 {
		separator = ";"
		if second == 0 && minute%10 != 0 && frame < dropFrames {
			frame = dropFrames
		}
	}
	return fmt.Sprintf("%02d:%02d:%02d%s%02d", hour, minute, second, separator, frame), nil
}
