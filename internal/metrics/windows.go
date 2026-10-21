package metrics

import (
	"math"
	"sort"
	"time"

	"video-overlay-cli/internal/activity"
)

const (
	DefaultShortWindow      = 3 * time.Second
	DefaultLongWindow       = 30 * time.Second
	DefaultExtraLongWindow  = 120 * time.Second
	SlowPaceSpeedMPS        = 1000.0 / (20 * 60)
	VerySlowPaceSpeedMPS    = 1000.0 / (30 * 60)
	StopSpeedMPS            = 0.05
	MaxGradientPercent      = 80.0
	MinGradientDistanceM    = 1.0
	DistanceGradientWindowM = 10.0
)

func windowPair(samples []activity.Sample, index int, window time.Duration) (int, int, bool) {
	if window <= 0 {
		return 0, 0, false
	}
	forwardAt := samples[index].Timestamp.Add(window)
	forward := sort.Search(len(samples), func(i int) bool { return !samples[i].Timestamp.Before(forwardAt) })
	if forward < len(samples) {
		return index, forward, true
	}
	backwardAt := samples[index].Timestamp.Add(-window)
	backward := sort.Search(len(samples), func(i int) bool { return samples[i].Timestamp.After(backwardAt) }) - 1
	if backward >= 0 && backward < index {
		return backward, index, true
	}
	return 0, 0, false
}

func distanceAndBearing(a, b activity.Sample) (float64, float64, bool) {
	if !gpsFixed2D(a) || !gpsFixed2D(b) {
		return 0, 0, false
	}
	const earthRadiusM = 6371008.8
	lat1 := a.Latitude.Number * math.Pi / 180
	lat2 := b.Latitude.Number * math.Pi / 180
	dLat := lat2 - lat1
	dLon := (b.Longitude.Number - a.Longitude.Number) * math.Pi / 180
	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1)*math.Cos(lat2)*math.Sin(dLon/2)*math.Sin(dLon/2)
	h = math.Max(0, math.Min(1, h))
	distance := 2 * earthRadiusM * math.Atan2(math.Sqrt(h), math.Sqrt(1-h))
	y := math.Sin(dLon) * math.Cos(lat2)
	x := math.Cos(lat1)*math.Sin(lat2) - math.Sin(lat1)*math.Cos(lat2)*math.Cos(dLon)
	bearing := math.Atan2(y, x) * 180 / math.Pi
	return distance, bearing, true
}

func gpsFixed2D(sample activity.Sample) bool {
	return sample.Latitude.Valid && sample.Longitude.Valid && sample.GPSFix.Valid && (sample.GPSFix.Number == 2 || sample.GPSFix.Number == 3)
}

func gpsFixed3D(sample activity.Sample) bool {
	return gpsFixed2D(sample) && sample.GPSFix.Number == 3
}

func gradientBetween(a, b activity.Sample, distance float64) activity.Value {
	if !gpsFixed3D(a) || !gpsFixed3D(b) || !a.Altitude.Valid || !b.Altitude.Valid || distance <= MinGradientDistanceM {
		return activity.Value{}
	}
	gradient := (b.Altitude.Number - a.Altitude.Number) / distance * 100
	if math.Abs(gradient) > MaxGradientPercent {
		return activity.Value{}
	}
	return activity.Value{Number: gradient, Valid: true}
}
