package widgets

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"sync"

	"github.com/tdewolff/canvas"
	"video-overlay-cli/internal/layout"
	"video-overlay-cli/internal/metrics"
	"video-overlay-cli/internal/render"
)

type vectorGauge struct {
	size, minimum, maximum, start, length float64
	sectors                               int
	metric, units                         string
	background, majorAnn, minorAnn        color.RGBA
	backgroundOuter                       color.RGBA
	majorTick, minorTick, needle          color.RGBA
	arcInner, arcOuter                    color.RGBA
	arcLower, arcUpper                    float64
	arcLowerMetric, arcUpperMetric        string
	hasArc                                bool
}

func cairoGaugeFactory(element *layout.Element, setup *render.Context, kind string) (render.Drawer, error) {
	size, err := floatAttr(element, "size", 256)
	if err != nil || size <= 0 || size > 4096 {
		return nil, fmt.Errorf("%s size must be between 0 and 4096", kind)
	}
	minimum, err := floatAttr(element, "min", 0)
	if err != nil {
		return nil, err
	}
	maximum, err := floatAttr(element, "max", 50)
	if err != nil || maximum <= minimum {
		return nil, fmt.Errorf("%s max must be greater than min", kind)
	}
	sectors, err := intAttr(element, "sectors", 5)
	if kind == "cairo_gauge_marker" {
		sectors, err = intAttr(element, "sectors", 6)
	}
	if err != nil || sectors < 1 || sectors > 100 {
		return nil, fmt.Errorf("%s sectors must be between 1 and 100", kind)
	}
	start, err := floatAttr(element, "start", 143)
	if kind == "cairo_gauge_marker" {
		start, err = floatAttr(element, "start", 0)
	}
	if err != nil {
		return nil, err
	}
	lengthDefault := 254.0
	if kind == "cairo_gauge_donut" {
		lengthDefault = 90
	}
	length, err := floatAttr(element, "length", lengthDefault)
	if err != nil || length == 0 || math.Abs(length) > 360 {
		return nil, fmt.Errorf("%s length must be nonzero and at most 360 degrees", kind)
	}
	metricName := attr(element, "metric", "speed")
	if _, err := metrics.ValueAt(setup.Sample, metricName); err != nil {
		return nil, err
	}
	g := vectorGauge{size: size, minimum: minimum, maximum: maximum, start: start, length: length, sectors: sectors, metric: metricName, units: attr(element, "units", "")}

	if kind == "cairo_gauge_marker" {
		g.majorTick, err = colorAttr(element, "tick-rgb", color.RGBA{R: 255, G: 255, B: 255, A: 255})
		if err != nil {
			return nil, err
		}
		g.needle, err = colorAttr(element, "gauge-rgb", color.RGBA{R: 0, G: 191, B: 255, A: 255})
		if err != nil {
			return nil, err
		}
		g.background, err = colorAttr(element, "background-rgb", withAlpha(g.majorTick, 0.2))
		if err != nil {
			return nil, err
		}
		g.arcOuter = g.needle
		g.arcInner = darken(g.needle, 0.2)
		return cairoMarkerGauge(g, element)
	}

	g.background, err = colorAttr(element, "background-rgb", color.RGBA{R: 255, G: 255, B: 255, A: 150})
	if kind == "cairo_gauge_donut" {
		g.background, err = colorAttr(element, "background-inner-rgb", color.RGBA{R: 255, G: 255, B: 255, A: 150})
		if err == nil {
			g.backgroundOuter, err = colorAttr(element, "background-outer-rgb", color.RGBA{R: 255, G: 255, B: 255, A: 255})
		}
	} else {
		g.backgroundOuter = g.background
	}
	if err != nil {
		return nil, err
	}
	g.majorAnn, err = colorAttr(element, "major-ann-rgb", color.RGBA{A: 255})
	if err != nil {
		return nil, err
	}
	g.minorAnn, err = colorAttr(element, "minor-ann-rgb", color.RGBA{A: 255})
	if err != nil {
		return nil, err
	}
	g.majorTick, err = colorAttr(element, "major-tick-rgb", color.RGBA{A: 255})
	if err != nil {
		return nil, err
	}
	g.minorTick, err = colorAttr(element, "minor-tick-rgb", color.RGBA{A: 255})
	if err != nil {
		return nil, err
	}
	g.needle, err = colorAttr(element, "needle-rgb", color.RGBA{R: 255, A: 255})
	if err != nil {
		return nil, err
	}
	g.arcInner, err = colorAttr(element, "arc-inner-rgb", color.RGBA{B: 190, A: 50})
	if err != nil {
		return nil, err
	}
	g.arcOuter, err = colorAttr(element, "arc-outer-rgb", color.RGBA{B: 190, A: 190})
	if err != nil {
		return nil, err
	}
	g.hasArc = kind == "cairo_gauge_arc_annotated" || kind == "cairo_gauge_donut"
	if _, ok := elementAttributeValue(element, "arc-value-upper"); ok {
		g.arcUpper, err = floatAttr(element, "arc-value-upper", 0)
	} else if upperMetric := attr(element, "arc-metric-upper", ""); upperMetric != "" {
		if _, valueErr := metrics.ValueAt(setup.Sample, upperMetric); valueErr != nil {
			return nil, valueErr
		}
		g.arcUpperMetric = upperMetric
	}
	if err != nil {
		return nil, err
	}
	if g.hasArc {
		if _, ok := elementAttributeValue(element, "arc-value-upper"); !ok && attr(element, "arc-metric-upper", "") == "" {
			g.hasArc = false
		}
		g.arcLower, err = floatAttr(element, "arc-value-lower", 0)
		if lowerMetric := attr(element, "arc-metric-lower", ""); lowerMetric != "" {
			if _, valueErr := metrics.ValueAt(setup.Sample, lowerMetric); valueErr != nil {
				return nil, valueErr
			}
			g.arcLowerMetric = lowerMetric
		}
		if err != nil {
			return nil, err
		}
	}
	return cairoAnnotatedGauge(g, element, kind), nil
}

func cairoMarkerGauge(g vectorGauge, element *layout.Element) (render.Drawer, error) {
	capper, err := gaugeCap(attr(element, "cap", "square"))
	if err != nil {
		return nil, err
	}
	outer, err := colorAttr(element, "dot-outer-rgb", withAlpha(g.majorTick, 0.7))
	if err != nil {
		return nil, err
	}
	inner, err := colorAttr(element, "dot-inner-rgb", withAlpha(g.needle, 0.7))
	if err != nil {
		return nil, err
	}
	var face *image.RGBA
	var faceOnce sync.Once
	return func(ctx *render.Context) error {
		faceOnce.Do(func() {
			face = cachedGaugePart(ctx, g.size, func(local *render.Context) {
				cx, cy := g.size/2, g.size/2
				drawGaugeCircle(local, cx, cy, 0.45*g.size, g.background)
				for i := 0; i <= g.sectors; i++ {
					angle := (g.start + g.length*float64(i)/float64(g.sectors)) * math.Pi / 180
					drawGaugeRadialLine(local, cx, cy, angle, 0.35*g.size, 0.45*g.size, g.majorTick, 0.02*g.size, capper)
				}
			})
		})
		drawImage(ctx, 0, 0, face)
		reading := math.Max(0.0001, gaugeReading(ctx, g.metric, g.units, g.maximum-g.minimum))
		angle := (g.start + g.length*reading) * math.Pi / 180
		cx, cy := g.size/2, g.size/2
		drawGaugeArcStroke(ctx, cx, cy, 0.40*g.size, g.start*math.Pi/180, g.length*reading*math.Pi/180, 0.05*g.size, g.arcInner, capper)
		drawGaugeArcStroke(ctx, cx, cy, 0.40*g.size, g.start*math.Pi/180, g.length*reading*math.Pi/180, 0.04*g.size, g.needle, capper)
		for i := 0; i <= g.sectors && float64(i)/float64(g.sectors) <= reading; i++ {
			a := (g.start + g.length*float64(i)/float64(g.sectors)) * math.Pi / 180
			drawGaugeRadialLine(ctx, cx, cy, a, 0.39*g.size, 0.41*g.size, g.majorTick, 0.02*g.size, capper)
			drawGaugeRadialLine(ctx, cx, cy, a, 0.39*g.size, 0.41*g.size, g.arcInner, 0.018*g.size, capper)
		}
		px, py := gaugePoint(cx, cy, 0.40*g.size, angle)
		drawGaugeCircle(ctx, px, py, 0.05*g.size, outer)
		drawGaugeCircle(ctx, px, py, 0.03*g.size, inner)
		return nil
	}, nil
}

func cairoAnnotatedGauge(g vectorGauge, element *layout.Element, kind string) render.Drawer {
	var background, decorations *image.RGBA
	var backgroundOnce, decorationsOnce sync.Once
	return func(ctx *render.Context) error {
		backgroundOnce.Do(func() {
			background = cachedGaugePart(ctx, g.size, func(local *render.Context) {
				if kind == "cairo_gauge_donut" {
					step := g.length / float64(g.sectors)
					bodge := step / 2.5
					drawGaugeSectorGradient(local, g.size/2, g.size/2, 0.30*g.size, 0.50*g.size, g.start-bodge, g.length+2*bodge, g.background, g.backgroundOuter)
				} else {
					drawGaugeCircle(local, g.size/2, g.size/2, 0.50*g.size, g.background)
				}
			})
		})
		decorationsOnce.Do(func() {
			decorations = cachedGaugePart(ctx, g.size, func(local *render.Context) {
				drawGaugeTicks(local, g)
				drawGaugeLabels(local, g)
			})
		})
		drawImage(ctx, 0, 0, background)
		if g.hasArc {
			lower := clampGaugeReading(g.arcLower / (g.maximum - g.minimum))
			upper := clampGaugeReading(g.arcUpper / (g.maximum - g.minimum))
			if g.arcLowerMetric != "" {
				lower = gaugeReading(ctx, g.arcLowerMetric, g.units, g.maximum-g.minimum)
			}
			if g.arcUpperMetric != "" {
				upper = gaugeReading(ctx, g.arcUpperMetric, g.units, g.maximum-g.minimum)
			}
			if lower > upper {
				lower, upper = upper, lower
			}
			drawGaugeSectorGradient(ctx, g.size/2, g.size/2, 0.30*g.size, 0.50*g.size, g.start+g.length*lower, g.length*(upper-lower), g.arcInner, g.arcOuter)
		}
		drawImage(ctx, 0, 0, decorations)
		reading := gaugeReading(ctx, g.metric, g.units, g.maximum-g.minimum)
		if kind == "cairo_gauge_round_annotated" {
			drawAnnotatedNeedle(ctx, g, reading, 0.135, 0.46, 0.03, 0.0175)
		} else if kind == "cairo_gauge_donut" {
			drawGaugeRadialLine(ctx, g.size/2, g.size/2, (g.start+g.length*reading)*math.Pi/180, 0.31*g.size, 0.49*g.size, g.needle, 0.01*g.size, canvas.ButtCap)
		} else {
			drawGaugeRadialLine(ctx, g.size/2, g.size/2, (g.start+g.length*reading)*math.Pi/180, 0.28*g.size, 0.49*g.size, g.needle, 0.0075*g.size, canvas.ButtCap)
		}
		return nil
	}
}

func drawGaugeTicks(ctx *render.Context, g vectorGauge) {
	cx, cy := g.size/2, g.size/2
	step := g.length / float64(g.sectors)
	for i := 0; i <= g.sectors; i++ {
		angle := (g.start + step*float64(i)) * math.Pi / 180
		drawGaugeRadialLine(ctx, cx, cy, angle, 0.43*g.size, 0.49*g.size, g.majorTick, 6*g.size/400, canvas.ButtCap)
		if i < g.sectors {
			minor := (g.start + step*(float64(i)+0.5)) * math.Pi / 180
			drawGaugeRadialLine(ctx, cx, cy, minor, 0.46*g.size, 0.49*g.size, g.minorTick, g.size/400, canvas.ButtCap)
		}
	}
}

func drawGaugeLabels(ctx *render.Context, g vectorGauge) {
	cx, cy := g.size/2, g.size/2
	step := (g.maximum - g.minimum) / float64(g.sectors)
	angleStep := g.length / float64(g.sectors)
	for i := 0; i <= g.sectors; i++ {
		value := g.minimum + step*float64(i)
		radius, font, fill := 0.41, 0.035, g.minorAnn
		if i%2 == 1 {
			radius, font, fill = 0.41, 0.05, g.majorAnn
		}
		angle := (g.start + angleStep*float64(i)) * math.Pi / 180
		x, y := gaugePoint(cx, cy, radius*g.size, angle)
		drawTextWithCache(ctx, x, y+font*g.size*0.33, font*g.size, fmt.Sprintf("%d", int(value)), "center", fill, color.RGBA{}, 0, true)
	}
}

func drawAnnotatedNeedle(ctx *render.Context, g vectorGauge, reading, rearRadius, tipRadius, rearWidth, tipWidth float64) {
	cx, cy := g.size/2, g.size/2
	a := (g.start + g.length*reading) * math.Pi / 180
	ux, uy := math.Cos(a), math.Sin(a)
	px, py := -uy, ux
	rx, ry := cx-ux*rearRadius*g.size, cy-uy*rearRadius*g.size
	tx, ty := cx+ux*tipRadius*g.size, cy+uy*tipRadius*g.size
	path := &canvas.Path{}
	path.MoveTo(tx+px*tipWidth*g.size/2, ty+py*tipWidth*g.size/2)
	path.LineTo(tx-px*tipWidth*g.size/2, ty-py*tipWidth*g.size/2)
	path.LineTo(rx-px*rearWidth*g.size/2, ry-py*rearWidth*g.size/2)
	path.LineTo(rx+px*rearWidth*g.size/2, ry+py*rearWidth*g.size/2)
	path.Close()
	drawGaugePath(ctx, path, g.needle, color.RGBA{}, 0)
}

func cachedGaugePart(ctx *render.Context, size float64, draw func(*render.Context)) *image.RGBA {
	imageSize := int(math.Ceil(size))
	img := image.NewRGBA(image.Rect(0, 0, imageSize, imageSize))
	local, raster := localCanvas(ctx, img)
	draw(&local)
	raster.Close()
	return img
}

func drawGaugeCircle(ctx *render.Context, x, y, radius float64, fill color.RGBA) {
	drawGaugePath(ctx, circlePath(radius), fill, color.RGBA{}, 0, x, y)
}

func drawGaugePath(ctx *render.Context, path *canvas.Path, fill, stroke color.RGBA, width float64, at ...float64) {
	x, y := 0.0, 0.0
	if len(at) == 2 {
		x, y = at[0], at[1]
	}
	ctx.Canvas.Push()
	defer ctx.Canvas.Pop()
	ctx.Canvas.SetFillColor(vectorColor(fill))
	ctx.Canvas.SetStrokeColor(vectorColor(stroke))
	ctx.Canvas.SetStrokeWidth(width)
	ctx.Canvas.DrawPath(x, y, path)
}

func drawGaugeRadialLine(ctx *render.Context, cx, cy, angle, inner, outer float64, stroke color.RGBA, width float64, capper canvas.Capper) {
	x1, y1 := gaugePoint(cx, cy, inner, angle)
	x2, y2 := gaugePoint(cx, cy, outer, angle)
	path := &canvas.Path{}
	path.MoveTo(x1, y1)
	path.LineTo(x2, y2)
	ctx.Canvas.Push()
	defer ctx.Canvas.Pop()
	ctx.Canvas.SetFillColor(color.RGBA{})
	ctx.Canvas.SetStrokeColor(vectorColor(stroke))
	ctx.Canvas.SetStrokeWidth(width)
	ctx.Canvas.SetStrokeCapper(capper)
	ctx.Canvas.DrawPath(0, 0, path)
}

func drawGaugeArcStroke(ctx *render.Context, cx, cy, radius, start, length, width float64, stroke color.RGBA, capper canvas.Capper) {
	path := gaugeArcPath(cx, cy, radius, start, length)
	ctx.Canvas.Push()
	defer ctx.Canvas.Pop()
	ctx.Canvas.SetFillColor(color.RGBA{})
	ctx.Canvas.SetStrokeColor(vectorColor(stroke))
	ctx.Canvas.SetStrokeWidth(width)
	ctx.Canvas.SetStrokeCapper(capper)
	ctx.Canvas.DrawPath(0, 0, path)
}

func drawGaugeSectorGradient(ctx *render.Context, cx, cy, inner, outer, startDegrees, lengthDegrees float64, innerColor, outerColor color.RGBA) {
	path := gaugeSectorPath(cx, cy, inner, outer, startDegrees*math.Pi/180, lengthDegrees*math.Pi/180)
	gradient := canvas.NewRadialGradient(canvas.Point{X: cx, Y: cy}, inner, canvas.Point{X: cx, Y: cy}, outer)
	gradient.Add(0, premultipliedColor(innerColor))
	gradient.Add(1, premultipliedColor(outerColor))
	ctx.Canvas.Push()
	defer ctx.Canvas.Pop()
	ctx.Canvas.SetFillGradient(gradient)
	ctx.Canvas.SetStrokeColor(color.RGBA{})
	ctx.Canvas.DrawPath(0, 0, path)
}

func gaugeArcPath(cx, cy, radius, start, length float64) *canvas.Path {
	steps := max(2, int(math.Ceil(math.Abs(length)*180/math.Pi/3)))
	path := &canvas.Path{}
	for i := 0; i <= steps; i++ {
		x, y := gaugePoint(cx, cy, radius, start+length*float64(i)/float64(steps))
		if i == 0 {
			path.MoveTo(x, y)
		} else {
			path.LineTo(x, y)
		}
	}
	return path
}

func gaugeSectorPath(cx, cy, inner, outer, start, length float64) *canvas.Path {
	steps := max(2, int(math.Ceil(math.Abs(length)*180/math.Pi/3)))
	path := &canvas.Path{}
	for i := 0; i <= steps; i++ {
		x, y := gaugePoint(cx, cy, outer, start+length*float64(i)/float64(steps))
		if i == 0 {
			path.MoveTo(x, y)
		} else {
			path.LineTo(x, y)
		}
	}
	for i := steps; i >= 0; i-- {
		x, y := gaugePoint(cx, cy, inner, start+length*float64(i)/float64(steps))
		path.LineTo(x, y)
	}
	path.Close()
	return path
}

func gaugePoint(cx, cy, radius, angle float64) (float64, float64) {
	return cx + radius*math.Cos(angle), cy + radius*math.Sin(angle)
}

func gaugeReading(ctx *render.Context, metricName, units string, denominator float64) float64 {
	value, err := metrics.ValueAt(ctx.Sample, metricName)
	if err != nil || !value.Valid || denominator <= 0 {
		return 0
	}
	return clampGaugeReading(convert(value, units, metricName).Number / denominator)
}

func clampGaugeReading(value float64) float64 {
	return math.Max(0, math.Min(1, value))
}

func vectorColor(value color.RGBA) color.Color {
	return nrgba(value)
}

func premultipliedColor(value color.RGBA) color.RGBA {
	alpha := uint16(value.A)
	return color.RGBA{R: uint8(uint16(value.R) * alpha / 255), G: uint8(uint16(value.G) * alpha / 255), B: uint8(uint16(value.B) * alpha / 255), A: value.A}
}

func withAlpha(value color.RGBA, alpha float64) color.RGBA {
	value.A = uint8(math.Round(float64(value.A) * alpha))
	return value
}

func darken(value color.RGBA, factor float64) color.RGBA {
	value.R = uint8(float64(value.R) * (1 - factor))
	value.G = uint8(float64(value.G) * (1 - factor))
	value.B = uint8(float64(value.B) * (1 - factor))
	return value
}

func gaugeCap(value string) (canvas.Capper, error) {
	switch value {
	case "square":
		return canvas.SquareCap, nil
	case "butt":
		return canvas.ButtCap, nil
	case "round":
		return canvas.RoundCap, nil
	default:
		return canvas.ButtCap, fmt.Errorf("invalid Cairo gauge cap %q", value)
	}
}

func elementAttributeValue(element *layout.Element, key string) (string, bool) {
	for _, item := range element.Attributes {
		if item.Name == key {
			return item.Value, true
		}
	}
	return "", false
}
