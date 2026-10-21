package activity

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func Load(path string) (*Activity, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open activity %q: %w", path, err)
	}
	defer f.Close()

	var a *Activity
	switch strings.ToLower(filepath.Ext(path)) {
	case ".fit":
		a, err = loadFIT(f)
	case ".gpx":
		a, err = loadGPX(f)
	default:
		return nil, fmt.Errorf("unsupported activity format %q: use .fit or .gpx", filepath.Ext(path))
	}
	if err != nil {
		return nil, fmt.Errorf("load activity %q: %w", path, err)
	}
	return a, nil
}

func orderedActivity(samples []Sample) (*Activity, error) {
	if len(samples) == 0 {
		return nil, fmt.Errorf("no timestamped GPS samples found")
	}
	sort.SliceStable(samples, func(i, j int) bool { return samples[i].Timestamp.Before(samples[j].Timestamp) })
	ordered := samples[:0]
	for _, sample := range samples {
		if len(ordered) > 0 && ordered[len(ordered)-1].Timestamp.Equal(sample.Timestamp) {
			ordered[len(ordered)-1] = sample
		} else {
			ordered = append(ordered, sample)
		}
	}
	return &Activity{Samples: ordered, Start: ordered[0].Timestamp, End: ordered[len(ordered)-1].Timestamp}, nil
}

func value(number float64) Value { return Value{Number: number, Valid: true} }

func setValue(dst *Value, number float64) { *dst = value(number) }
