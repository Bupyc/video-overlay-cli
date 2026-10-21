package activity

import (
	"testing"
	"time"
)

func TestResampleInterpolatesValuesAtRequestedRate(t *testing.T) {
	start := time.Unix(0, 0).UTC()
	a := &Activity{Samples: []Sample{
		{Timestamp: start, Latitude: Value{Number: 10, Valid: true}, Power: Value{Number: 100, Valid: true}},
		{Timestamp: start.Add(time.Second), Latitude: Value{Number: 20, Valid: true}},
	}, Start: start, End: start.Add(time.Second)}
	if err := Resample(a, 2); err != nil {
		t.Fatal(err)
	}
	if len(a.Samples) != 3 {
		t.Fatalf("resampled sample count = %d, want 3", len(a.Samples))
	}
	if got := a.Samples[1].Latitude; got != (Value{Number: 15, Valid: true}) {
		t.Errorf("interpolated latitude = %+v, want 15", got)
	}
	if a.Samples[1].Power.Valid {
		t.Errorf("interpolated missing power = %+v, want invalid", a.Samples[1].Power)
	}
	if a.Samples[1].Timestamp != start.Add(500*time.Millisecond) {
		t.Errorf("interpolated timestamp = %v", a.Samples[1].Timestamp)
	}
}

func TestResampleKeepsSingleSample(t *testing.T) {
	sample := Sample{Timestamp: time.Unix(0, 0).UTC()}
	a := &Activity{Samples: []Sample{sample}}
	if err := Resample(a, 10); err != nil {
		t.Fatal(err)
	}
	if len(a.Samples) != 1 || a.Samples[0] != sample {
		t.Fatalf("single sample changed: %+v", a.Samples)
	}
}
