package activity

import "time"

type Value struct {
	Number float64
	Valid  bool
}

type Sample struct {
	Timestamp          time.Time
	Latitude           Value
	Longitude          Value
	Distance           Value
	Altitude           Value
	Speed              Value
	VerticalSpeed      Value
	HR                 Value
	Cadence            Value
	RunningCadence     Value
	Power              Value
	Temperature        Value
	Grade              Value
	RawSpeed           Value
	CSpeed             Value
	CSpeedRaw          Value
	LongSpeed          Value
	DistanceDelta      Value
	Elapsed            Value
	Azimuth            Value
	Course             Value
	Acceleration       Value
	CVerticalSpeed     Value
	LVerticalSpeed     Value
	XLVerticalSpeed    Value
	CGradient          Value
	LGradient          Value
	XLGradient         Value
	DistanceGradient   Value
	Odometer           Value
	CumulativeDistance Value
	GPSAccuracy        Value
	GPSPacket          Value
	GPSPacketIndex     Value
	GPSLock            Value
	GPSFix             Value
	FrontGear          Value
	RearGear           Value
	Respiration        Value
	SDPS               Value
}

type Activity struct {
	Samples []Sample
	Start   time.Time
	End     time.Time
}
