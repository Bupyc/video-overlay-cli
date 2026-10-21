package metrics

import (
	"fmt"
	"sort"
	"time"

	"video-overlay-cli/internal/activity"
)

type Sport string

const (
	SportAuto    Sport = "auto"
	SportRunning Sport = "running"
	SportCycling Sport = "cycling"
)

type Profile struct {
	Sport           Sport
	ShortWindow     time.Duration
	LongWindow      time.Duration
	ExtraLongWindow time.Duration
}

func Process(a *activity.Activity, profile Profile) error {
	if a == nil || len(a.Samples) == 0 {
		return fmt.Errorf("activity has no samples")
	}
	profile, err := normalizedProfile(profile)
	if err != nil {
		return err
	}
	if !sort.SliceIsSorted(a.Samples, func(i, j int) bool { return a.Samples[i].Timestamp.Before(a.Samples[j].Timestamp) }) {
		return fmt.Errorf("activity samples must be ordered by timestamp")
	}
	for i := 1; i < len(a.Samples); i++ {
		if a.Samples[i].Timestamp.Equal(a.Samples[i-1].Timestamp) {
			return fmt.Errorf("activity samples contain duplicate timestamp %s", a.Samples[i].Timestamp)
		}
	}
	if a.Start.IsZero() {
		a.Start = a.Samples[0].Timestamp
	}
	if a.End.IsZero() {
		a.End = a.Samples[len(a.Samples)-1].Timestamp
	}

	smoothPositions(a.Samples)
	processCadence(a.Samples, profile.Sport)
	processDistance(a.Samples)
	processSpeeds(a.Samples, profile)
	processVerticalSpeed(a.Samples, profile)
	processGradients(a.Samples, profile)
	processAcceleration(a.Samples, profile.ShortWindow)
	return nil
}

func normalizedProfile(profile Profile) (Profile, error) {
	if profile.Sport == "" {
		profile.Sport = SportAuto
	}
	switch profile.Sport {
	case SportAuto, SportRunning, SportCycling:
	default:
		return Profile{}, fmt.Errorf("unsupported sport %q", profile.Sport)
	}
	if profile.ShortWindow <= 0 {
		profile.ShortWindow = DefaultShortWindow
	}
	if profile.LongWindow <= 0 {
		profile.LongWindow = DefaultLongWindow
	}
	if profile.ExtraLongWindow <= 0 {
		profile.ExtraLongWindow = DefaultExtraLongWindow
	}
	return profile, nil
}

func smoothPositions(samples []activity.Sample) {
	var previousLat, forecastLat, previousLon, forecastLon float64
	seen := false
	for i := range samples {
		sample := &samples[i]
		if !gpsFixed2D(*sample) {
			continue
		}
		lat, lon := sample.Latitude.Number, sample.Longitude.Number
		if !seen {
			previousLat, forecastLat = lat, lat
			previousLon, forecastLon = lon, lon
			seen = true
		} else {
			forecastLat = 0.45*previousLat + 0.55*forecastLat
			forecastLon = 0.45*previousLon + 0.55*forecastLon
			previousLat, previousLon = lat, lon
		}
		sample.Latitude.Number = forecastLat
		sample.Longitude.Number = forecastLon
	}
}

func processCadence(samples []activity.Sample, sport Sport) {
	if sport == SportCycling {
		return
	}
	for i := range samples {
		if !samples[i].RunningCadence.Valid {
			samples[i].RunningCadence = RunningCadenceStepsPerMinute(samples[i].Cadence)
		}
	}
}

func processDistance(samples []activity.Sample) {
	cumulative := 0.0
	for i := range samples {
		sample := &samples[i]
		if i == 0 {
			if gpsFixed2D(*sample) {
				sample.CumulativeDistance = activity.Value{Number: 0, Valid: true}
			}
		} else if distance, _, ok := distanceAndBearing(samples[i-1], *sample); ok {
			cumulative += distance
			sample.DistanceDelta = activity.Value{Number: distance, Valid: true}
			sample.CumulativeDistance = activity.Value{Number: cumulative, Valid: true}
		}
		if sample.Distance.Valid {
			sample.Odometer = sample.Distance
		} else if sample.CumulativeDistance.Valid {
			sample.Odometer = sample.CumulativeDistance
		}
	}
}

func processSpeeds(samples []activity.Sample, profile Profile) {
	for i := range samples {
		samples[i].RawSpeed = samples[i].Speed
	}
	kalmanSensor := kalman{}
	for i := range samples {
		if samples[i].Speed.Valid {
			samples[i].Speed.Number = kalmanSensor.update(samples[i].Speed.Number)
		}
	}
	shortFilter, longFilter := kalman{}, kalman{}
	for i := range samples {
		if a, b, ok := windowPair(samples, i, profile.ShortWindow); ok {
			if distance, bearing, valid := distanceAndBearing(samples[a], samples[b]); valid {
				elapsed := samples[b].Timestamp.Sub(samples[a].Timestamp).Seconds()
				if elapsed > 0 {
					rawSpeed := distance / elapsed
					samples[i].CSpeedRaw = activity.Value{Number: rawSpeed, Valid: true}
					samples[i].CSpeed = activity.Value{Number: shortFilter.update(rawSpeed), Valid: true}
					samples[i].Elapsed = activity.Value{Number: elapsed, Valid: true}
					samples[i].DistanceDelta = activity.Value{Number: distance / float64(b-a), Valid: true}
					samples[i].Azimuth = activity.Value{Number: bearing, Valid: true}
					if bearing < 0 {
						bearing += 360
					}
					samples[i].Course = activity.Value{Number: bearing, Valid: true}
				}
			}
		}
		if a, b, ok := windowPair(samples, i, profile.LongWindow); ok {
			if distance, _, valid := speedDistance(samples[a], samples[b]); valid {
				elapsed := samples[b].Timestamp.Sub(samples[a].Timestamp).Seconds()
				if elapsed > 0 {
					samples[i].LongSpeed = activity.Value{Number: longFilter.update(distance / elapsed), Valid: true}
				}
			}
		}
	}
}

func speedDistance(a, b activity.Sample) (float64, float64, bool) {
	if a.Distance.Valid && b.Distance.Valid {
		distance := b.Distance.Number - a.Distance.Number
		if distance >= 0 {
			return distance, 0, true
		}
	}
	return distanceAndBearing(a, b)
}

func processVerticalSpeed(samples []activity.Sample, profile Profile) {
	windows := []struct {
		window time.Duration
		set    func(*activity.Sample, activity.Value)
	}{
		{profile.ShortWindow, func(s *activity.Sample, v activity.Value) { s.CVerticalSpeed = v }},
		{profile.LongWindow, func(s *activity.Sample, v activity.Value) { s.LVerticalSpeed = v }},
		{profile.ExtraLongWindow, func(s *activity.Sample, v activity.Value) { s.XLVerticalSpeed = v }},
	}
	for _, w := range windows {
		for i := range samples {
			a, b, ok := windowPair(samples, i, w.window)
			if !ok || !gpsFixed3D(samples[a]) || !gpsFixed3D(samples[b]) || !samples[a].Altitude.Valid || !samples[b].Altitude.Valid {
				continue
			}
			elapsed := samples[b].Timestamp.Sub(samples[a].Timestamp).Seconds()
			if elapsed > 0 {
				w.set(&samples[i], activity.Value{Number: (samples[b].Altitude.Number - samples[a].Altitude.Number) / elapsed, Valid: true})
			}
		}
	}
}

func processGradients(samples []activity.Sample, profile Profile) {
	allDistance := true
	for _, sample := range samples {
		allDistance = allDistance && sample.Distance.Valid
	}
	if allDistance {
		monotonic := true
		for i := 1; i < len(samples); i++ {
			monotonic = monotonic && samples[i].Distance.Number >= samples[i-1].Distance.Number
		}
		if !monotonic {
			return
		}
		for i := range samples {
			threshold := samples[i].Distance.Number + DistanceGradientWindowM
			j := sort.Search(len(samples), func(j int) bool { return samples[j].Distance.Number >= threshold })
			if j >= len(samples) {
				j = sort.Search(i, func(j int) bool {
					return samples[j].Distance.Number > samples[i].Distance.Number-DistanceGradientWindowM
				}) - 1
				if j < 0 {
					continue
				}
				samples[i].DistanceGradient = gradientBetween(samples[j], samples[i], samples[i].Distance.Number-samples[j].Distance.Number)
			} else {
				samples[i].DistanceGradient = gradientBetween(samples[i], samples[j], samples[j].Distance.Number-samples[i].Distance.Number)
			}
		}
		return
	}
	windows := []struct {
		window time.Duration
		set    func(*activity.Sample, activity.Value)
	}{
		{profile.ShortWindow, func(s *activity.Sample, v activity.Value) { s.CGradient = v }},
		{profile.LongWindow, func(s *activity.Sample, v activity.Value) { s.LGradient = v }},
		{profile.ExtraLongWindow, func(s *activity.Sample, v activity.Value) { s.XLGradient = v }},
	}
	for _, w := range windows {
		for i := range samples {
			a, b, ok := windowPair(samples, i, w.window)
			if !ok {
				continue
			}
			distance, _, valid := distanceAndBearing(samples[a], samples[b])
			if valid {
				w.set(&samples[i], gradientBetween(samples[a], samples[b], distance))
			}
		}
	}
}

func processAcceleration(samples []activity.Sample, window time.Duration) {
	for i := range samples {
		a, b, ok := windowPair(samples, i, window)
		if !ok || !samples[a].RawSpeed.Valid || !samples[b].RawSpeed.Valid {
			continue
		}
		elapsed := samples[b].Timestamp.Sub(samples[a].Timestamp).Seconds()
		if elapsed > 0 {
			samples[i].Acceleration = activity.Value{Number: (samples[b].RawSpeed.Number - samples[a].RawSpeed.Number) / elapsed, Valid: true}
		}
	}
}

type kalman struct {
	p        float64
	estimate float64
	seen     bool
}

func (k *kalman) update(measurement float64) float64 {
	if !k.seen {
		k.estimate = measurement
		k.seen = true
	}
	gain := k.p / (k.p + 100)
	k.estimate += gain * (measurement - k.estimate)
	k.p = (1-gain)*k.p + 10
	return k.estimate
}

func ValueAt(sample activity.Sample, name string) (activity.Value, error) {
	switch name {
	case "hr":
		return sample.HR, nil
	case "cadence":
		return sample.Cadence, nil
	case "running_cadence":
		return sample.RunningCadence, nil
	case "power":
		return sample.Power, nil
	case "speed":
		if sample.Speed.Valid {
			return sample.Speed, nil
		}
		return sample.CSpeed, nil
	case "raw_speed":
		return sample.RawSpeed, nil
	case "cspeed", "cspeed.k":
		return sample.CSpeed, nil
	case "cspeed.raw":
		return sample.CSpeedRaw, nil
	case "lspeed":
		return sample.LongSpeed, nil
	case "pace":
		speed := referenceSpeed(sample)
		if speed.Valid && speed.Number <= StopSpeedMPS {
			return activity.Value{}, nil
		}
		selected := selectWindow(sample.CSpeed, sample.LongSpeed, activity.Value{}, speed, false)
		if selected.Valid && selected.Number <= StopSpeedMPS {
			return activity.Value{}, nil
		}
		return selected, nil
	case "accel":
		return sample.Acceleration, nil
	case "temp":
		return sample.Temperature, nil
	case "gradient":
		if sample.Distance.Valid {
			return sample.DistanceGradient, nil
		}
		return selectWindow(sample.CGradient, sample.LGradient, sample.XLGradient, referenceSpeed(sample), true), nil
	case "cgrad":
		return sample.CGradient, nil
	case "vertical_speed":
		if sample.VerticalSpeed.Valid {
			return sample.VerticalSpeed, nil
		}
		return selectWindow(sample.CVerticalSpeed, sample.LVerticalSpeed, sample.XLVerticalSpeed, referenceSpeed(sample), true), nil
	case "cvspeed":
		return sample.CVerticalSpeed, nil
	case "lvspeed":
		return sample.LVerticalSpeed, nil
	case "xlvspeed":
		return sample.XLVerticalSpeed, nil
	case "alt":
		return sample.Altitude, nil
	case "odo":
		if sample.Odometer.Valid {
			return sample.Odometer, nil
		}
		return sample.CumulativeDistance, nil
	case "codo":
		return sample.CumulativeDistance, nil
	case "dist":
		return sample.DistanceDelta, nil
	case "time":
		return sample.Elapsed, nil
	case "azi":
		return sample.Azimuth, nil
	case "cog":
		return sample.Course, nil
	case "gps-dop":
		return sample.GPSAccuracy, nil
	case "timestamp":
		return activity.Value{Number: float64(sample.Timestamp.Unix()), Valid: !sample.Timestamp.IsZero()}, nil
	case "gps-packet":
		return sample.GPSPacket, nil
	case "gps-packet-index":
		return sample.GPSPacketIndex, nil
	case "gps-lock":
		return sample.GPSLock, nil
	case "respiration":
		return sample.Respiration, nil
	case "gear.front":
		return sample.FrontGear, nil
	case "gear.rear":
		return sample.RearGear, nil
	case "sdps":
		return sample.SDPS, nil
	case "lat":
		return sample.Latitude, nil
	case "lon":
		return sample.Longitude, nil
	case "accl.x", "accl.y", "accl.z", "grav.x", "grav.y", "grav.z", "ori.pitch", "ori.roll", "ori.yaw":
		return activity.Value{}, nil
	default:
		return activity.Value{}, fmt.Errorf("unsupported activity metric %q", name)
	}
}

func referenceSpeed(sample activity.Sample) activity.Value {
	if sample.RawSpeed.Valid {
		return sample.RawSpeed
	}
	if sample.Speed.Valid {
		return sample.Speed
	}
	return sample.CSpeed
}

func selectWindow(short, long, extra, speed activity.Value, useExtra bool) activity.Value {
	if !speed.Valid || speed.Number < SlowPaceSpeedMPS {
		if useExtra && speed.Valid && speed.Number < VerySlowPaceSpeedMPS && extra.Valid {
			return extra
		}
		if long.Valid {
			return long
		}
		return short
	}
	if short.Valid {
		return short
	}
	return long
}
