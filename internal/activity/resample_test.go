package activity

import (
	"reflect"
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
	if cap(a.Samples) != len(a.Samples) {
		t.Errorf("resampled capacity = %d, want exact length %d", cap(a.Samples), len(a.Samples))
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

func TestInterpolateSampleInterpolatesEveryValueFieldWithoutAllocating(t *testing.T) {
	start := time.Unix(0, 0).UTC()
	first := Sample{Timestamp: start}
	second := Sample{Timestamp: start.Add(time.Second)}
	firstFields := reflect.ValueOf(&first).Elem()
	secondFields := reflect.ValueOf(&second).Elem()
	valueType := reflect.TypeOf(Value{})
	valueFields := 0
	for i := 0; i < firstFields.NumField(); i++ {
		if firstFields.Type().Field(i).Type != valueType {
			continue
		}
		valueFields++
		firstFields.Field(i).Set(reflect.ValueOf(Value{Number: float64(i * 2), Valid: true}))
		secondFields.Field(i).Set(reflect.ValueOf(Value{Number: float64(i*2 + 2), Valid: true}))
	}

	at := start.Add(500 * time.Millisecond)
	got := interpolateSample(first, second, at)
	gotFields := reflect.ValueOf(got)
	for i := 0; i < gotFields.NumField(); i++ {
		if gotFields.Type().Field(i).Type != valueType {
			continue
		}
		want := Value{Number: float64(i*2 + 1), Valid: true}
		if field := gotFields.Field(i).Interface().(Value); field != want {
			t.Errorf("%s = %+v, want %+v", gotFields.Type().Field(i).Name, field, want)
		}
	}
	if valueFields == 0 || got.Timestamp != at {
		t.Fatalf("interpolated timestamp/field count = %v/%d", got.Timestamp, valueFields)
	}

	allocs := testing.AllocsPerRun(100, func() {
		got = interpolateSample(first, second, at)
	})
	if allocs != 0 {
		t.Errorf("interpolateSample allocates %g objects per sample, want 0", allocs)
	}
}

func TestResampleKeepsSingleSample(t *testing.T) {
	sample := Sample{Timestamp: time.Unix(0, 0).UTC()}
	a := &Activity{Samples: []Sample{sample}}
	if err := Resample(a, 10); err != nil {
		t.Fatal(err)
	}
	if len(a.Samples) != 1 || !reflect.DeepEqual(a.Samples[0], sample) {
		t.Fatalf("single sample changed: %+v", a.Samples)
	}
}
