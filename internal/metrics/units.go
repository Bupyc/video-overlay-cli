package metrics

import (
	"math"

	"video-overlay-cli/internal/activity"
)

func PaceMinutesPerKM(speed activity.Value) activity.Value {
	if !speed.Valid || speed.Number <= 0.0000001 || math.IsNaN(speed.Number) || math.IsInf(speed.Number, 0) {
		return activity.Value{}
	}
	pace := 1000 / (60 * speed.Number)
	if pace > 500 {
		pace = 500
	}
	return activity.Value{Number: pace, Valid: true}
}

func RunningCadenceStepsPerMinute(cadence activity.Value) activity.Value {
	if !cadence.Valid {
		return activity.Value{}
	}
	return activity.Value{Number: cadence.Number * 2, Valid: true}
}

func VerticalMetersPerHour(speed activity.Value) activity.Value {
	if !speed.Valid {
		return activity.Value{}
	}
	return activity.Value{Number: speed.Number * 3600, Valid: true}
}
