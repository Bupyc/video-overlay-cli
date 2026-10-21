package activity

import (
	"fmt"
	"reflect"
	"time"
)

func Resample(a *Activity, rate int) error {
	if a == nil || len(a.Samples) == 0 {
		return fmt.Errorf("activity has no samples")
	}
	if rate <= 0 || rate > int(time.Second) {
		return fmt.Errorf("sample rate must be between 1 and %d", int(time.Second))
	}
	for i := 1; i < len(a.Samples); i++ {
		if !a.Samples[i].Timestamp.After(a.Samples[i-1].Timestamp) {
			return fmt.Errorf("activity samples must be strictly ordered by timestamp")
		}
	}
	if len(a.Samples) == 1 {
		a.Start, a.End = a.Samples[0].Timestamp, a.Samples[0].Timestamp
		return nil
	}
	step := time.Second / time.Duration(rate)
	start, end := a.Samples[0].Timestamp, a.Samples[len(a.Samples)-1].Timestamp
	resampled := make([]Sample, 0)
	index := 1
	for at := start; !at.After(end); at = at.Add(step) {
		for index < len(a.Samples) && a.Samples[index].Timestamp.Before(at) {
			index++
		}
		if index < len(a.Samples) && a.Samples[index].Timestamp.Equal(at) {
			resampled = append(resampled, a.Samples[index])
		} else if index > 0 && index < len(a.Samples) {
			resampled = append(resampled, interpolateSample(a.Samples[index-1], a.Samples[index], at))
		}
	}
	a.Samples = resampled
	a.Start, a.End = resampled[0].Timestamp, resampled[len(resampled)-1].Timestamp
	return nil
}

func interpolateSample(start, end Sample, at time.Time) Sample {
	result := start
	result.Timestamp = at
	position := float64(at.Sub(start.Timestamp)) / float64(end.Timestamp.Sub(start.Timestamp))
	valueType := reflect.TypeOf(Value{})
	source, other, target := reflect.ValueOf(start), reflect.ValueOf(end), reflect.ValueOf(&result).Elem()
	for i := 0; i < target.NumField(); i++ {
		if target.Type().Field(i).Type != valueType {
			continue
		}
		first, second := source.Field(i).Interface().(Value), other.Field(i).Interface().(Value)
		if first.Valid && second.Valid {
			target.Field(i).Set(reflect.ValueOf(Value{Number: first.Number + (second.Number-first.Number)*position, Valid: true}))
		} else {
			target.Field(i).Set(reflect.Zero(valueType))
		}
	}
	return result
}
