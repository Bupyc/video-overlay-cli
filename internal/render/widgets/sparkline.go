package widgets

import (
	"image"
	"image/color"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/tdewolff/canvas"
	"video-overlay-cli/internal/activity"
	"video-overlay-cli/internal/render"
)

func NewSparkLine(a *activity.Activity, at, size image.Point) render.Drawer {
	if a == nil || len(a.Samples) == 0 || size.X <= 0 || size.Y <= 0 {
		return func(*render.Context) error { return nil }
	}
	values := make([]float64, len(a.Samples))
	timestamps := make([]time.Time, len(a.Samples))
	minimum, maximum := math.Inf(1), math.Inf(-1)
	for i, sample := range a.Samples {
		timestamps[i] = sample.Timestamp
		if sample.Cadence.Valid && !math.IsNaN(sample.Cadence.Number) && !math.IsInf(sample.Cadence.Number, 0) {
			values[i] = sample.Cadence.Number
		}
		minimum = math.Min(minimum, values[i])
		maximum = math.Max(maximum, values[i])
	}
	var chart *image.RGBA
	var cache sync.Once
	return func(ctx *render.Context) error {
		cache.Do(func() {
			chart = renderSparkLine(ctx, values, size, minimum, maximum)
		})
		drawImage(ctx, float64(at.X), float64(at.Y), chart)
		current := a.Start.Add(ctx.At)
		index := sort.Search(len(timestamps), func(i int) bool { return timestamps[i].After(current) })
		if index == len(values) {
			index--
		}
		x := sparkX(index, len(values), size.X)
		y := sparkY(values[index], minimum, maximum, size.Y)
		drawGaugeCircle(ctx, float64(at.X)+x, float64(at.Y)+y, math.Min(3, float64(size.Y)/4), color.RGBA{R: 255, G: 255, B: 255, A: 255})
		return nil
	}
}

func renderSparkLine(ctx *render.Context, values []float64, size image.Point, minimum, maximum float64) *image.RGBA {
	chart := image.NewRGBA(image.Rect(0, 0, size.X, size.Y))
	local, raster := localCanvas(ctx, chart)
	defer raster.Close()
	if len(values) < 2 {
		return chart
	}
	if maximum > minimum {
		area := &canvas.Path{}
		area.MoveTo(sparkX(0, len(values), size.X), float64(size.Y-1))
		for i, value := range values {
			area.LineTo(sparkX(i, len(values), size.X), sparkY(value, minimum, maximum, size.Y))
		}
		area.LineTo(sparkX(len(values)-1, len(values), size.X), float64(size.Y-1))
		area.Close()
		local.Canvas.SetFillColor(color.NRGBA{R: 255, A: 51})
		local.Canvas.SetStrokeColor(color.RGBA{})
		local.Canvas.DrawPath(0, 0, area)
	}
	points := make([][2]float64, len(values))
	for i, value := range values {
		points[i] = [2]float64{sparkX(i, len(values), size.X), sparkY(value, minimum, maximum, size.Y)}
	}
	drawLine(&local, points, color.RGBA{R: 255, A: 255}, 1.5)
	return chart
}

func sparkX(index, count, width int) float64 {
	if count <= 1 || width <= 1 {
		return float64(width-1) / 2
	}
	return float64(index) * float64(width-1) / float64(count-1)
}

func sparkY(value, minimum, maximum float64, height int) float64 {
	if maximum == minimum || height <= 1 {
		return float64(height-1) / 2
	}
	return float64(height-1) * (1 - (value-minimum)/(maximum-minimum))
}
