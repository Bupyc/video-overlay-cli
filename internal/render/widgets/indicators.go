package widgets

import (
	"fmt"
	"image/color"
	"math"

	"github.com/tdewolff/canvas"
	"video-overlay-cli/internal/activity"
	"video-overlay-cli/internal/layout"
	"video-overlay-cli/internal/metrics"
	"video-overlay-cli/internal/render"
)

func asiFactory(element *layout.Element, setup *render.Context) (render.Drawer, error) {
	return analogGaugeFactory(element, setup, true, true)
}

func msiFactory(element *layout.Element, setup *render.Context) (render.Drawer, error) {
	needle := attr(element, "needle", "true") != "false"
	return analogGaugeFactory(element, setup, false, needle)
}

func msi2Factory(element *layout.Element, setup *render.Context) (render.Drawer, error) {
	return analogGaugeFactory(element, setup, false, false)
}

func analogGaugeFactory(element *layout.Element, setup *render.Context, airspeed, needle bool) (render.Drawer, error) {
	size, err := floatAttr(element, "size", 256)
	if err != nil || size <= 0 {
		return nil, fmt.Errorf("gauge size must be positive")
	}
	textSize, err := floatAttr(element, "textsize", 16)
	if err != nil || textSize <= 0 {
		return nil, fmt.Errorf("gauge textsize must be positive")
	}
	rotateDefault := 180.0
	if airspeed {
		rotateDefault = 0
	}
	rotate, err := floatAttr(element, "rotate", rotateDefault)
	if err != nil {
		return nil, err
	}
	green, err := floatAttr(element, "green", 0)
	if err != nil {
		return nil, err
	}
	yellow, err := floatAttr(element, "yellow", 130)
	if err != nil {
		return nil, err
	}
	end, err := floatAttr(element, "end", 180)
	if err != nil || end <= 0 {
		return nil, fmt.Errorf("gauge end must be positive")
	}
	vs0, err := floatAttr(element, "vs0", 40)
	if err != nil {
		return nil, err
	}
	vs, err := floatAttr(element, "vs", 46)
	if err != nil {
		return nil, err
	}
	vfe, err := floatAttr(element, "vfe", 103)
	if err != nil {
		return nil, err
	}
	vno, err := floatAttr(element, "vno", 126)
	if err != nil {
		return nil, err
	}
	vne, err := floatAttr(element, "vne", 180)
	if err != nil {
		return nil, err
	}
	metricName := attr(element, "metric", "speed")
	if _, err := metricsValue(setup, metricName); err != nil {
		return nil, err
	}
	units := attr(element, "units", "knots")
	outlineWidth, err := floatAttr(element, "outline", 2)
	if err != nil || outlineWidth < 0 {
		return nil, fmt.Errorf("gauge outline must not be negative")
	}
	return func(ctx *render.Context) error {
		cx, cy := size/2, size/2
		radius := size/2 - 3
		minimum, maximum := green, end
		if airspeed {
			minimum = vs0
			maximum = math.Ceil((vne+(vne-vs0)*0.05)/20) * 20
		} else {
			maximum = math.Ceil((end*1.05)/20) * 20
		}
		angle := func(value float64) float64 {
			return 30 + 300*(value-minimum)/(maximum-minimum) + rotate
		}
		ctx.Canvas.Push()
		ctx.Canvas.SetFillColor(color.RGBA{A: 128})
		ctx.Canvas.SetStrokeColor(color.RGBA{R: 0, G: 0, B: 0, A: 128})
		ctx.Canvas.SetStrokeWidth(2)
		ctx.Canvas.DrawPath(cx, cy, circlePath(radius))
		ctx.Canvas.Pop()
		if airspeed {
			gaugeArc(ctx, cx, cy, radius-size*0.07, angle(vs0), angle(vfe), color.RGBA{R: 255, G: 255, B: 255, A: 255}, size*0.055)
			gaugeArc(ctx, cx, cy, radius-size*0.07, angle(vs), angle(vno), color.RGBA{R: 51, G: 193, B: 25, A: 255}, size*0.055)
			gaugeArc(ctx, cx, cy, radius-size*0.07, angle(vno), angle(vne), color.RGBA{R: 237, G: 239, B: 42, A: 255}, size*0.055)
			gaugeArc(ctx, cx, cy, radius-size*0.07, angle(vne), angle(maximum), color.RGBA{R: 246, G: 34, B: 21, A: 255}, size*0.055)
		} else {
			gaugeArc(ctx, cx, cy, radius-size*0.07, angle(green), angle(yellow), color.RGBA{R: 51, G: 193, B: 25, A: 255}, size/17)
			gaugeArc(ctx, cx, cy, radius-size*0.07, angle(yellow), angle(end), color.RGBA{R: 237, G: 239, B: 42, A: 255}, size/17)
		}
		step := 5.0
		tickPath := linePathPool.Get().(*canvas.Path)
		tickPath.Reset()
		for value := math.Ceil(minimum/step) * step; value <= maximum; value += step {
			length := size / 30
			if math.Mod(value, 10) == 0 {
				length = size / 12
			}
			x1, y1 := roundPoint(cx, cy, radius, angle(value))
			x2, y2 := roundPoint(cx, cy, radius-length, angle(value))
			addLinePolygon(tickPath, x1, y1, x2, y2, math.Max(1, size/256))
			if math.Mod(value-minimum, 20) == 0 {
				drawShape(ctx, tickPath, color.RGBA{R: 255, G: 255, B: 255, A: 255}, color.RGBA{}, 0)
				tickPath.Reset()
				lx, ly := roundPoint(cx, cy, radius*0.58, angle(value))
				drawText(ctx, lx, ly+textSize/3, textSize, fmt.Sprintf("%.0f", value), "center", color.RGBA{R: 255, G: 255, B: 255, A: 255}, color.RGBA{A: 255}, outlineWidth)
			}
		}
		drawShape(ctx, tickPath, color.RGBA{R: 255, G: 255, B: 255, A: 255}, color.RGBA{}, 0)
		if cap(tickPath.Data()) <= maxPooledPathData {
			tickPath.Reset()
			linePathPool.Put(tickPath)
		}
		reading, _ := metricsValue(ctx, metricName)
		reading = convert(reading, units, metricName)
		value := minimum
		if reading.Valid {
			value = math.Max(minimum, math.Min(maximum, reading.Number))
		}
		if airspeed && value < vs0 {
			value = math.Max(0, vs0-1)
		}
		currentAngle := angle(value)
		if needle {
			leftX, leftY := roundPoint(cx, cy, size*0.035, currentAngle-90)
			rightX, rightY := roundPoint(cx, cy, size*0.035, currentAngle+90)
			tipX, tipY := roundPoint(cx, cy, radius*0.8, currentAngle)
			path := &canvas.Path{}
			path.MoveTo(cx, cy)
			path.LineTo(leftX, leftY)
			path.LineTo(tipX, tipY)
			path.LineTo(rightX, rightY)
			path.Close()
			drawShape(ctx, path, color.RGBA{R: 255, G: 255, B: 255, A: 255}, color.RGBA{A: 255}, 1)
		} else {
			gaugeArc(ctx, cx, cy, radius-size*0.04, angle(minimum), currentAngle, color.RGBA{R: 0, G: 191, B: 255, A: 255}, size/17)
		}
		return nil
	}, nil
}

func metricsValue(ctx *render.Context, name string) (activity.Value, error) {
	return metrics.ValueAt(ctx.Sample, name)
}

func circlePath(radius float64) *canvas.Path { return canvas.Circle(radius) }
