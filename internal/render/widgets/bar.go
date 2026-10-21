package widgets

import (
	"fmt"
	"image/color"
	"math"

	"github.com/tdewolff/canvas"
	"video-overlay-cli/internal/layout"
	"video-overlay-cli/internal/render"
)

func barFactory(element *layout.Element, setup *render.Context) (render.Drawer, error) {
	name := attr(element, "metric", "")
	if name == "" {
		return nil, fmt.Errorf("bar component requires metric")
	}
	if _, _, err := metric(setup, element); err != nil {
		return nil, err
	}
	width, err := intAttr(element, "width", 400)
	if err != nil || width <= 0 {
		return nil, fmt.Errorf("bar width must be positive")
	}
	height, err := intAttr(element, "height", 30)
	if err != nil || height <= 0 {
		return nil, fmt.Errorf("bar height must be positive")
	}
	minValue, err := floatAttr(element, "min", -20)
	if err != nil {
		return nil, err
	}
	maxValue, err := floatAttr(element, "max", 20)
	if err != nil || maxValue <= minValue {
		return nil, fmt.Errorf("bar max must be greater than min")
	}
	radius, err := floatAttr(element, "cr", 5)
	if err != nil || radius < 0 {
		return nil, fmt.Errorf("bar corner radius must not be negative")
	}
	lineWidth, err := floatAttr(element, "outline-width", 3)
	if err != nil || lineWidth < 0 {
		return nil, fmt.Errorf("bar outline-width must not be negative")
	}
	fill, err := colorAttr(element, "fill", color.RGBA{})
	if err != nil {
		return nil, err
	}
	outline, err := colorAttr(element, "outline", color.RGBA{R: 255, G: 255, B: 255, A: 255})
	if err != nil {
		return nil, err
	}
	bar, err := colorAttr(element, "bar", color.RGBA{R: 255, G: 255, B: 255, A: 255})
	if err != nil {
		return nil, err
	}
	zero, err := colorAttr(element, "zero", color.RGBA{R: 255, G: 255, B: 255, A: 255})
	if err != nil {
		return nil, err
	}
	negative, err := colorAttr(element, "h-neg", color.RGBA{R: 255, A: 255})
	if err != nil {
		return nil, err
	}
	positive, err := colorAttr(element, "h-pos", color.RGBA{G: 255, A: 255})
	if err != nil {
		return nil, err
	}
	units := attr(element, "units", "")
	return func(ctx *render.Context) error {
		value, _, err := metric(ctx, element)
		if err != nil {
			return err
		}
		value = convert(value, units, name)
		reading := 0.0
		if value.Valid {
			reading = math.Max(minValue, math.Min(maxValue, value.Number))
		}
		x := func(v float64) float64 {
			return (v - minValue) / (maxValue - minValue) * float64(width-1)
		}
		path := canvas.RoundedRectangle(float64(width), float64(height), math.Min(radius, float64(height)/2))
		drawShape(ctx, path, fill, outline, lineWidth)
		zeroX, currentX := x(math.Max(minValue, math.Min(maxValue, 0))), x(reading)
		if currentX >= zeroX {
			drawRect(ctx, zeroX, lineWidth+1, currentX-zeroX, math.Max(0, float64(height)-lineWidth*2-2), 0, bar)
			drawRect(ctx, zeroX+(currentX-zeroX)*0.95, lineWidth+1, (currentX-zeroX)*0.05, math.Max(0, float64(height)-lineWidth*2-2), 0, positive)
		} else {
			drawRect(ctx, currentX, lineWidth+1, zeroX-currentX, math.Max(0, float64(height)-lineWidth*2-2), 0, bar)
			drawRect(ctx, currentX, lineWidth+1, (zeroX-currentX)*0.05, math.Max(0, float64(height)-lineWidth*2-2), 0, negative)
		}
		drawLine(ctx, [][2]float64{{zeroX, 0}, {zeroX, float64(height)}}, zero, 1)
		return nil
	}, nil
}

func zoneBarFactory(element *layout.Element, setup *render.Context) (render.Drawer, error) {
	name := attr(element, "metric", "")
	if name == "" {
		return nil, fmt.Errorf("zone_bar component requires metric")
	}
	if _, _, err := metric(setup, element); err != nil {
		return nil, err
	}
	width, err := intAttr(element, "width", 400)
	if err != nil || width <= 0 {
		return nil, fmt.Errorf("zone_bar width must be positive")
	}
	height, err := intAttr(element, "height", 30)
	if err != nil || height <= 0 {
		return nil, fmt.Errorf("zone_bar height must be positive")
	}
	minValue, err := floatAttr(element, "min", 0)
	if err != nil {
		return nil, err
	}
	maxValue, err := floatAttr(element, "max", 400)
	if err != nil || maxValue <= minValue {
		return nil, fmt.Errorf("zone_bar max must be greater than min")
	}
	zoneValues := make([]float64, 3)
	for i, key := range []string{"z1", "z2", "z3"} {
		zoneValues[i], err = floatAttr(element, key, []float64{120, 160, 200}[i])
		if err != nil || zoneValues[i] <= minValue || (i > 0 && zoneValues[i] <= zoneValues[i-1]) {
			return nil, fmt.Errorf("zone_bar %s must increase above the previous zone", key)
		}
	}
	fill, err := colorAttr(element, "fill", color.RGBA{})
	if err != nil {
		return nil, err
	}
	outline, err := colorAttr(element, "outline", color.RGBA{R: 255, G: 255, B: 255, A: 255})
	if err != nil {
		return nil, err
	}
	divider, err := colorAttr(element, "zone-divider", color.RGBA{R: 255, G: 255, B: 255, A: 255})
	if err != nil {
		return nil, err
	}
	zones := make([]color.RGBA, 4)
	for i, key := range []string{"z0-rgb", "z1-rgb", "z2-rgb", "z3-rgb"} {
		zones[i], err = colorAttr(element, key, []color.RGBA{{R: 255, G: 255, B: 255, A: 255}, {R: 67, G: 235, B: 52, A: 255}, {R: 240, G: 232, B: 19, A: 255}, {R: 207, G: 19, B: 2, A: 255}}[i])
		if err != nil {
			return nil, err
		}
	}
	lineWidth, err := floatAttr(element, "outline-width", 3)
	if err != nil || lineWidth < 0 {
		return nil, fmt.Errorf("zone_bar outline-width must not be negative")
	}
	units := attr(element, "units", "")
	return func(ctx *render.Context) error {
		value, _, err := metric(ctx, element)
		if err != nil {
			return err
		}
		reading := minValue
		if converted := convert(value, units, name); converted.Valid {
			reading = math.Max(minValue, math.Min(maxValue, converted.Number))
		}
		x := func(v float64) float64 { return (v - minValue) / (maxValue - minValue) * float64(width-1) }
		drawShape(ctx, canvas.RoundedRectangle(float64(width), float64(height), float64(height)/6), fill, outline, lineWidth)
		boundaries := []float64{minValue, zoneValues[0], zoneValues[1], zoneValues[2], maxValue}
		for i := 0; i < 4; i++ {
			start, end := boundaries[i], math.Min(reading, boundaries[i+1])
			if end > start {
				drawRect(ctx, x(start), lineWidth, x(end)-x(start), float64(height)-lineWidth*2, 0, zones[i])
			}
		}
		for _, boundary := range zoneValues {
			bx := x(boundary)
			drawLine(ctx, [][2]float64{{bx, lineWidth}, {bx, float64(height) - lineWidth}}, divider, 1)
		}
		return nil
	}, nil
}

func drawRect(ctx *render.Context, x, y, width, height, radius float64, fill color.RGBA) {
	if width <= 0 || height <= 0 {
		return
	}
	drawShape(ctx, canvas.RoundedRectangle(width, height, math.Min(radius, math.Min(width, height)/2)).Translate(x, y), fill, color.RGBA{}, 0)
}
