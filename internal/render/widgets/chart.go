package widgets

import (
	"fmt"
	"image/color"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tdewolff/canvas"
	"video-overlay-cli/internal/activity"
	"video-overlay-cli/internal/layout"
	"video-overlay-cli/internal/metrics"
	"video-overlay-cli/internal/render"
)

func chartFactory(element *layout.Element, setup *render.Context) (render.Drawer, error) {
	name := attr(element, "metric", "alt")
	if _, err := metrics.ValueAt(setup.Sample, name); err != nil {
		return nil, err
	}
	secondsText := attr(element, "seconds", "")
	distanceText := attr(element, "distance", "")
	if secondsText != "" && distanceText != "" {
		return nil, fmt.Errorf("chart can use either seconds or distance, not both")
	}
	seconds := 300
	if secondsText != "" {
		parsed, err := strconv.Atoi(secondsText)
		if err != nil || parsed <= 0 {
			return nil, fmt.Errorf("chart seconds must be positive")
		}
		seconds = parsed
	}
	windowDistance := 0.0
	if distanceText != "" {
		distance, err := parseDistance(distanceText)
		if err != nil {
			return nil, fmt.Errorf("chart distance: %w", err)
		}
		windowDistance = distance
	}
	useDistance := useDistanceWindow(setup.Activity, windowDistance)
	width, err := intAttr(element, "width", 0)
	if err != nil || width < 0 {
		return nil, fmt.Errorf("chart width must not be negative")
	}
	height, err := intAttr(element, "height", 64)
	if err != nil || height <= 0 {
		return nil, fmt.Errorf("chart height must be positive")
	}
	samples, err := intAttr(element, "samples", 256)
	if err != nil || samples <= 0 {
		return nil, fmt.Errorf("chart samples must be positive")
	}
	markerSize, err := floatAttr(element, "marker-size", 4)
	if err != nil || markerSize < 0 {
		return nil, fmt.Errorf("chart marker-size must not be negative")
	}
	textSize, err := floatAttr(element, "textsize", 16)
	if err != nil || textSize <= 0 {
		return nil, fmt.Errorf("chart textsize must be positive")
	}
	bg, err := colorAttr(element, "bg", color.RGBA{A: 170})
	if err != nil {
		return nil, err
	}
	fill, err := colorAttr(element, "fill", color.RGBA{R: 91, G: 113, B: 146, A: 170})
	if err != nil {
		return nil, err
	}
	line, err := colorAttr(element, "line", color.RGBA{R: 255, G: 255, B: 255, A: 170})
	if err != nil {
		return nil, err
	}
	textColor, err := colorAttr(element, "text", color.RGBA{R: 255, G: 255, B: 255, A: 170})
	if err != nil {
		return nil, err
	}
	filled := attr(element, "filled", "true") != "false"
	showValues := attr(element, "values", "true") != "false"
	units := attr(element, "units", "m")
	return func(ctx *render.Context) error {
		current := ctx.Activity.Start.Add(ctx.At)
		values, markerIndex := timeChartValues(ctx.Activity, current, time.Duration(seconds)*time.Second, samples, name, units)
		if useDistance {
			values, markerIndex = distanceChartValues(ctx.Activity, odometerAt(ctx.Activity, current), windowDistance, samples, name, units)
		}
		chartWidth := width
		if chartWidth == 0 {
			chartWidth = max(1, len(values))
		}
		drawShape(ctx, canvas.Rectangle(float64(chartWidth), float64(height)), bg, color.RGBA{}, 0)
		minimum, maximum := math.Inf(1), math.Inf(-1)
		first, last := -1, -1
		for i, value := range values {
			if value.Valid {
				minimum = math.Min(minimum, value.Number)
				maximum = math.Max(maximum, value.Number)
				if first < 0 {
					first = i
				}
				last = i
			}
		}
		if first < 0 {
			return nil
		}
		scaleY := float64(height) / (math.Max(maximum-minimum, 1) * 1.1)
		xPosition := func(index float64) float64 {
			if width == 0 {
				return index * float64(chartWidth) / float64(len(values))
			}
			return (index - float64(first)) * float64(chartWidth) / float64(max(last-first, 1))
		}
		points := make([][2]float64, 0, len(values))
		for i, value := range values {
			if !value.Valid {
				continue
			}
			px := xPosition(float64(i))
			py := float64(height-1) - (value.Number-minimum)*scaleY
			points = append(points, [2]float64{px, py})
		}
		if filled && len(points) > 1 {
			area := &canvas.Path{}
			area.MoveTo(points[0][0], float64(height-1))
			area.LineTo(points[0][0], points[0][1])
			for _, point := range points[1:] {
				area.LineTo(point[0], point[1])
			}
			area.LineTo(points[len(points)-1][0], float64(height-1))
			area.Close()
			drawShape(ctx, area, fill, color.RGBA{}, 0)
		}
		drawLine(ctx, points, line, 2)
		if len(points) > 0 && markerSize > 0 {
			markerIndex = math.Max(float64(first), math.Min(float64(last), markerIndex))
			if value, ok := valueAtIndex(values, markerIndex); ok {
				markerY := float64(height-1) - (value-minimum)*scaleY
				ctx.Canvas.Push()
				ctx.Canvas.SetFillColor(color.RGBA{R: 255, A: 255})
				ctx.Canvas.SetStrokeColor(color.RGBA{A: 255})
				ctx.Canvas.SetStrokeWidth(1)
				ctx.Canvas.DrawPath(xPosition(markerIndex), markerY, canvas.Circle(markerSize))
				ctx.Canvas.Pop()
			}
		}
		if showValues {
			drawText(ctx, 10, textSize+1, textSize, fmt.Sprintf("%.0f", maximum), "left", textColor, color.RGBA{A: 255}, 1)
			drawText(ctx, 10, float64(height)-2, textSize, fmt.Sprintf("%.0f", minimum), "left", textColor, color.RGBA{A: 255}, 1)
		}
		return nil
	}, nil
}

func timeChartValues(a *activity.Activity, current time.Time, duration time.Duration, samples int, name, units string) ([]activity.Value, float64) {
	values := make([]activity.Value, samples)
	step := duration / time.Duration(samples)
	start := current.Add(-duration / 2)
	for i := range values {
		values[i] = convert(chartValueAt(a, start.Add(time.Duration(i)*step), name), units, name)
	}
	return values, float64(samples / 2)
}

func useDistanceWindow(a *activity.Activity, distance float64) bool {
	if distance <= 0 {
		return false
	}
	for _, sample := range a.Samples {
		if !sample.Distance.Valid {
			return false
		}
	}
	return len(a.Samples) > 0
}

func chartValueAt(a *activity.Activity, at time.Time, name string) activity.Value {
	samples := a.Samples
	i := sort.Search(len(samples), func(i int) bool { return !samples[i].Timestamp.Before(at) })
	if i < len(samples) && samples[i].Timestamp.Equal(at) {
		value, _ := metrics.ValueAt(samples[i], name)
		return value
	}
	if i == 0 || i == len(samples) {
		return activity.Value{}
	}
	start, _ := metrics.ValueAt(samples[i-1], name)
	end, _ := metrics.ValueAt(samples[i], name)
	if !start.Valid || !end.Valid {
		return activity.Value{}
	}
	span := samples[i].Timestamp.Sub(samples[i-1].Timestamp)
	if span <= 0 {
		return activity.Value{}
	}
	position := float64(at.Sub(samples[i-1].Timestamp)) / float64(span)
	return activity.Value{Number: start.Number + (end.Number-start.Number)*position, Valid: true}
}

func distanceChartValues(a *activity.Activity, currentOdo activity.Value, distance float64, samples int, name, units string) ([]activity.Value, float64) {
	values := make([]activity.Value, samples)
	points := make([]activity.Sample, 0, len(a.Samples))
	for _, sample := range a.Samples {
		if odo := sampleOdometer(sample); odo.Valid {
			if len(points) > 0 && odo.Number < sampleOdometer(points[len(points)-1]).Number {
				return values, 0
			}
			points = append(points, sample)
		}
	}
	if len(points) == 0 {
		return values, 0
	}
	if !currentOdo.Valid {
		return values, 0
	}
	minimum, maximum := sampleOdometer(points[0]).Number, sampleOdometer(points[len(points)-1]).Number
	span := math.Min(distance, maximum-minimum)
	start := math.Max(minimum, math.Min(currentOdo.Number-span/2, maximum-span))
	step := 0.0
	if samples > 1 {
		step = span / float64(samples-1)
	}
	for i := range values {
		target := start + float64(i)*step
		index := sort.Search(len(points), func(j int) bool { return sampleOdometer(points[j]).Number >= target })
		if index > 0 && (index == len(points) || target-sampleOdometer(points[index-1]).Number < sampleOdometer(points[index]).Number-target) {
			index--
		}
		value, _ := metrics.ValueAt(points[index], name)
		values[i] = convert(value, units, name)
	}
	marker := 0.0
	if step > 0 {
		marker = (currentOdo.Number - start) / step
	}
	return values, marker
}

func sampleOdometer(sample activity.Sample) activity.Value {
	if sample.Odometer.Valid {
		return sample.Odometer
	}
	return sample.CumulativeDistance
}

func odometerAt(a *activity.Activity, at time.Time) activity.Value {
	i := sort.Search(len(a.Samples), func(i int) bool { return !a.Samples[i].Timestamp.Before(at) })
	if i < len(a.Samples) && a.Samples[i].Timestamp.Equal(at) {
		return sampleOdometer(a.Samples[i])
	}
	if i == 0 || i == len(a.Samples) {
		return activity.Value{}
	}
	start, end := sampleOdometer(a.Samples[i-1]), sampleOdometer(a.Samples[i])
	span := a.Samples[i].Timestamp.Sub(a.Samples[i-1].Timestamp)
	if !start.Valid || !end.Valid || span <= 0 {
		return activity.Value{}
	}
	position := float64(at.Sub(a.Samples[i-1].Timestamp)) / float64(span)
	return activity.Value{Number: start.Number + (end.Number-start.Number)*position, Valid: true}
}

func valueAtIndex(values []activity.Value, index float64) (float64, bool) {
	lo := int(index)
	hi := min(lo+1, len(values)-1)
	fraction := index - float64(lo)
	if values[lo].Valid && values[hi].Valid {
		return values[lo].Number*(1-fraction) + values[hi].Number*fraction, true
	}
	if values[lo].Valid {
		return values[lo].Number, true
	}
	if values[hi].Valid {
		return values[hi].Number, true
	}
	return 0, false
}

func parseDistance(value string) (float64, error) {
	value = strings.TrimSpace(value)
	index := 0
	for index < len(value) && ((value[index] >= '0' && value[index] <= '9') || value[index] == '.' || value[index] == '+' || value[index] == '-') {
		index++
	}
	number, err := strconv.ParseFloat(strings.TrimSpace(value[:index]), 64)
	if err != nil || number <= 0 {
		return 0, fmt.Errorf("%q must be a positive distance", value)
	}
	switch strings.ToLower(strings.TrimSpace(value[index:])) {
	case "", "m", "meter", "meters", "metre", "metres":
	case "km", "kilometer", "kilometers", "kilometre", "kilometres":
		number *= 1000
	case "mi", "mile", "miles":
		number *= 1609.344
	case "ft", "foot", "feet":
		number *= 0.3048
	default:
		return 0, fmt.Errorf("unsupported unit in %q", value)
	}
	return number, nil
}
