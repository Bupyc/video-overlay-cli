package metrics

import (
	"math"
	"testing"
	"time"

	"video-overlay-cli/internal/activity"
)

func TestProcessComputesGPSWindowsAndSportCadence(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	a := &activity.Activity{Samples: make([]activity.Sample, 6)}
	for i := range a.Samples {
		a.Samples[i] = activity.Sample{
			Timestamp: start.Add(time.Duration(i) * time.Second),
			Latitude:  activity.Value{Number: 0, Valid: true},
			Longitude: activity.Value{Number: float64(i) * 0.00001, Valid: true},
			Altitude:  activity.Value{Number: float64(i) * 0.2, Valid: true},
			Speed:     activity.Value{Number: 0.6, Valid: true},
			Cadence:   activity.Value{Number: 90, Valid: true},
			GPSFix:    activity.Value{Number: 3, Valid: true},
		}
	}
	profile := Profile{Sport: SportRunning, ShortWindow: time.Second, LongWindow: 2 * time.Second, ExtraLongWindow: 3 * time.Second}
	if err := Process(a, profile); err != nil {
		t.Fatal(err)
	}
	if !a.Samples[0].CSpeed.Valid || !a.Samples[0].CVerticalSpeed.Valid || math.Abs(a.Samples[0].CVerticalSpeed.Number-0.2) > 1e-9 {
		t.Fatalf("short-window metrics missing or incorrect: %+v", a.Samples[0])
	}
	if !a.Samples[5].CSpeed.Valid || !a.Samples[5].CumulativeDistance.Valid || a.Samples[5].CumulativeDistance.Number <= 0 {
		t.Fatalf("tail metrics or cumulative distance missing: %+v", a.Samples[5])
	}
	if a.Samples[0].RunningCadence.Number != 180 {
		t.Fatalf("running cadence = %+v, want 180 steps/min", a.Samples[0].RunningCadence)
	}
}

func TestProcessUsesFITDistanceForGradient(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	a := &activity.Activity{Samples: make([]activity.Sample, 5)}
	for i := range a.Samples {
		a.Samples[i] = activity.Sample{
			Timestamp: start.Add(time.Duration(i) * time.Second),
			Latitude:  activity.Value{Number: 51, Valid: true},
			Longitude: activity.Value{Number: -2 + float64(i)*0.000001, Valid: true},
			Distance:  activity.Value{Number: float64(i * 5), Valid: true},
			Altitude:  activity.Value{Number: float64(i), Valid: true},
			GPSFix:    activity.Value{Number: 3, Valid: true},
		}
	}
	if err := Process(a, Profile{ShortWindow: time.Second, LongWindow: 2 * time.Second, ExtraLongWindow: 3 * time.Second}); err != nil {
		t.Fatal(err)
	}
	if !a.Samples[0].DistanceGradient.Valid || math.Abs(a.Samples[0].DistanceGradient.Number-20) > 1e-9 {
		t.Fatalf("distance gradient = %+v, want 20%%", a.Samples[0].DistanceGradient)
	}
}

func TestValueAtSelectsTrailWindowsAndCadence(t *testing.T) {
	sample := activity.Sample{
		RawSpeed:        activity.Value{Number: 0.5, Valid: true},
		CSpeed:          activity.Value{Number: 0.9, Valid: true},
		LongSpeed:       activity.Value{Number: 0.6, Valid: true},
		CGradient:       activity.Value{Number: 1, Valid: true},
		LGradient:       activity.Value{Number: 2, Valid: true},
		XLGradient:      activity.Value{Number: 3, Valid: true},
		CVerticalSpeed:  activity.Value{Number: 4, Valid: true},
		LVerticalSpeed:  activity.Value{Number: 5, Valid: true},
		XLVerticalSpeed: activity.Value{Number: 6, Valid: true},
		Cadence:         activity.Value{Number: 90, Valid: true},
		RunningCadence:  activity.Value{Number: 180, Valid: true},
		FrontGear:       activity.Value{Number: 2, Valid: true},
		RearGear:        activity.Value{Number: 7, Valid: true},
	}
	for name, want := range map[string]float64{"pace": 0.6, "gradient": 3, "vertical_speed": 6, "running_cadence": 180, "gear.front": 2, "gear.rear": 7} {
		got, err := ValueAt(sample, name)
		if err != nil || !got.Valid || math.Abs(got.Number-want) > 1e-9 {
			t.Errorf("ValueAt(%q) = %+v, %v; want %v", name, got, err, want)
		}
	}
	fast := sample
	fast.RawSpeed = activity.Value{Number: 0.9, Valid: true}
	for name, want := range map[string]float64{"pace": 0.9, "gradient": 1, "vertical_speed": 4} {
		got, err := ValueAt(fast, name)
		if err != nil || !got.Valid || math.Abs(got.Number-want) > 1e-9 {
			t.Errorf("fast ValueAt(%q) = %+v, %v; want %v", name, got, err, want)
		}
	}
	slow := sample
	slow.RawSpeed = activity.Value{Number: 0.7, Valid: true}
	for name, want := range map[string]float64{"pace": 0.6, "gradient": 2, "vertical_speed": 5} {
		got, err := ValueAt(slow, name)
		if err != nil || !got.Valid || math.Abs(got.Number-want) > 1e-9 {
			t.Errorf("slow ValueAt(%q) = %+v, %v; want %v", name, got, err, want)
		}
	}
	stopped := sample
	stopped.RawSpeed = activity.Value{Number: 0.05, Valid: true}
	if got, err := ValueAt(stopped, "pace"); err != nil || got.Valid {
		t.Errorf("stopped pace = %+v, %v; want invalid", got, err)
	}
}

func TestPaceConversionCapsAt500AndTreatsZeroAsMissing(t *testing.T) {
	if got := PaceMinutesPerKM(activity.Value{Number: 0.01, Valid: true}); !got.Valid || got.Number != 500 {
		t.Errorf("slow pace = %+v, want 500 min/km", got)
	}
	if got := PaceMinutesPerKM(activity.Value{Number: 0, Valid: true}); got.Valid {
		t.Errorf("zero speed pace = %+v, want invalid", got)
	}
}
