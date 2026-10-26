package activity

import (
	"fmt"
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
	resampled := make([]Sample, 0, int(end.Sub(start)/step)+1)
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
	result.Latitude = interpolateValue(start.Latitude, end.Latitude, position)
	result.Longitude = interpolateValue(start.Longitude, end.Longitude, position)
	result.Distance = interpolateValue(start.Distance, end.Distance, position)
	result.Altitude = interpolateValue(start.Altitude, end.Altitude, position)
	result.Speed = interpolateValue(start.Speed, end.Speed, position)
	result.VerticalSpeed = interpolateValue(start.VerticalSpeed, end.VerticalSpeed, position)
	result.HR = interpolateValue(start.HR, end.HR, position)
	result.Cadence = interpolateValue(start.Cadence, end.Cadence, position)
	result.RunningCadence = interpolateValue(start.RunningCadence, end.RunningCadence, position)
	result.Power = interpolateValue(start.Power, end.Power, position)
	result.Temperature = interpolateValue(start.Temperature, end.Temperature, position)
	result.Grade = interpolateValue(start.Grade, end.Grade, position)
	result.RawSpeed = interpolateValue(start.RawSpeed, end.RawSpeed, position)
	result.CSpeed = interpolateValue(start.CSpeed, end.CSpeed, position)
	result.CSpeedRaw = interpolateValue(start.CSpeedRaw, end.CSpeedRaw, position)
	result.LongSpeed = interpolateValue(start.LongSpeed, end.LongSpeed, position)
	result.DistanceDelta = interpolateValue(start.DistanceDelta, end.DistanceDelta, position)
	result.Elapsed = interpolateValue(start.Elapsed, end.Elapsed, position)
	result.Azimuth = interpolateValue(start.Azimuth, end.Azimuth, position)
	result.Course = interpolateValue(start.Course, end.Course, position)
	result.Acceleration = interpolateValue(start.Acceleration, end.Acceleration, position)
	result.CVerticalSpeed = interpolateValue(start.CVerticalSpeed, end.CVerticalSpeed, position)
	result.LVerticalSpeed = interpolateValue(start.LVerticalSpeed, end.LVerticalSpeed, position)
	result.XLVerticalSpeed = interpolateValue(start.XLVerticalSpeed, end.XLVerticalSpeed, position)
	result.CGradient = interpolateValue(start.CGradient, end.CGradient, position)
	result.LGradient = interpolateValue(start.LGradient, end.LGradient, position)
	result.XLGradient = interpolateValue(start.XLGradient, end.XLGradient, position)
	result.DistanceGradient = interpolateValue(start.DistanceGradient, end.DistanceGradient, position)
	result.Odometer = interpolateValue(start.Odometer, end.Odometer, position)
	result.CumulativeDistance = interpolateValue(start.CumulativeDistance, end.CumulativeDistance, position)
	result.GPSAccuracy = interpolateValue(start.GPSAccuracy, end.GPSAccuracy, position)
	result.GPSPacket = interpolateValue(start.GPSPacket, end.GPSPacket, position)
	result.GPSPacketIndex = interpolateValue(start.GPSPacketIndex, end.GPSPacketIndex, position)
	result.GPSLock = interpolateValue(start.GPSLock, end.GPSLock, position)
	result.GPSFix = interpolateValue(start.GPSFix, end.GPSFix, position)
	result.FrontGear = interpolateValue(start.FrontGear, end.FrontGear, position)
	result.RearGear = interpolateValue(start.RearGear, end.RearGear, position)
	result.Respiration = interpolateValue(start.Respiration, end.Respiration, position)
	result.SDPS = interpolateValue(start.SDPS, end.SDPS, position)
	return result
}

func interpolateValue(start, end Value, position float64) Value {
	if !start.Valid || !end.Valid {
		return Value{}
	}
	return Value{Number: start.Number + (end.Number-start.Number)*position, Valid: true}
}
