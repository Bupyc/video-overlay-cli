package widgets

import (
	"fmt"
	"image/color"
	"math"

	"github.com/tdewolff/canvas"
	"video-overlay-cli/internal/layout"
	"video-overlay-cli/internal/render"
)

func compassFactory(element *layout.Element, _ *render.Context) (render.Drawer, error) {
	size, err := floatAttr(element, "size", 256)
	if err != nil || size <= 0 {
		return nil, fmt.Errorf("compass size must be positive")
	}
	textSize, err := floatAttr(element, "textsize", 16)
	if err != nil || textSize <= 0 {
		return nil, fmt.Errorf("compass textsize must be positive")
	}
	fg, err := colorAttr(element, "fg", color.RGBA{R: 255, G: 255, B: 255, A: 255})
	if err != nil {
		return nil, err
	}
	bg, err := colorAttr(element, "bg", color.RGBA{})
	if err != nil {
		return nil, err
	}
	text, err := colorAttr(element, "text", color.RGBA{R: 255, G: 255, B: 255, A: 255})
	if err != nil {
		return nil, err
	}
	return func(ctx *render.Context) error {
		radius := size / 2
		cx, cy := radius, radius
		if bg.A > 0 {
			ctx.Canvas.Push()
			ctx.Canvas.SetFillColor(nrgba(bg))
			ctx.Canvas.SetStrokeColor(nrgba(fg))
			ctx.Canvas.SetStrokeWidth(2)
			ctx.Canvas.DrawPath(cx, cy, canvas.Circle(radius-1))
			ctx.Canvas.Pop()
		} else {
			ctx.Canvas.Push()
			ctx.Canvas.SetFillColor(color.RGBA{})
			ctx.Canvas.SetStrokeColor(nrgba(fg))
			ctx.Canvas.SetStrokeWidth(2)
			ctx.Canvas.DrawPath(cx, cy, canvas.Circle(radius-1))
			ctx.Canvas.Pop()
		}
		ctx.Canvas.Push()
		ctx.Canvas.SetFillColor(color.RGBA{})
		ctx.Canvas.SetStrokeColor(nrgba(fg))
		ctx.Canvas.SetStrokeWidth(2)
		ctx.Canvas.DrawPath(cx, cy, canvas.Circle(radius*0.3))
		ctx.Canvas.Pop()
		heading := 0.0
		if ctx.Sample.Course.Valid {
			heading = ctx.Sample.Course.Number
		}
		if fg.A == 255 {
			tickPath := linePathPool.Get().(*canvas.Path)
			tickPath.Reset()
			for angle := 0; angle < 360; angle += 5 {
				length := size / 30
				switch {
				case angle%90 == 0:
					length = size / 7
				case angle%45 == 0:
					length = size / 10
				case angle%10 == 0:
					length = size / 20
				}
				x1, y1 := roundPoint(cx, cy, radius-2, float64(angle)-heading)
				x2, y2 := roundPoint(cx, cy, radius-length, float64(angle)-heading)
				addLinePolygon(tickPath, x1, y1, x2, y2, 2)
			}
			drawShape(ctx, tickPath, fg, color.RGBA{}, 0)
			if cap(tickPath.Data()) <= maxPooledPathData {
				tickPath.Reset()
				linePathPool.Put(tickPath)
			}
		} else {
			for angle := 0; angle < 360; angle += 5 {
				length := size / 30
				switch {
				case angle%90 == 0:
					length = size / 7
				case angle%45 == 0:
					length = size / 10
				case angle%10 == 0:
					length = size / 20
				}
				x1, y1 := roundPoint(cx, cy, radius-2, float64(angle)-heading)
				x2, y2 := roundPoint(cx, cy, radius-length, float64(angle)-heading)
				drawLine(ctx, [][2]float64{{x1, y1}, {x2, y2}}, fg, 2)
			}
		}
		minorTick := size / 10
		point := func(distance, angle float64) [2]float64 {
			x, y := roundPoint(cx, cy, distance, angle)
			return [2]float64{x, y}
		}
		drawCompassTriangle(ctx,
			point(radius-1, -heading),
			point(radius-minorTick, -heading-5),
			point(radius-minorTick, -heading+5), fg)
		drawCompassTriangle(ctx,
			point(radius-1, 0),
			point(radius-minorTick, -5),
			point(radius-minorTick, 5), fg)
		for angle, label := range map[int]string{0: "N", 90: "E", 180: "S", 270: "W"} {
			x, y := roundPoint(cx, cy, radius/2, float64(angle)-heading)
			drawText(ctx, x, y+textSize/3, textSize, label, "center", text, color.RGBA{}, 0)
		}
		ctx.Canvas.Push()
		ctx.Canvas.SetFillColor(nrgba(fg))
		ctx.Canvas.DrawPath(cx, cy, canvas.Circle(2))
		ctx.Canvas.Pop()
		return nil
	}, nil
}

func drawCompassTriangle(ctx *render.Context, tip, left, right [2]float64, fill color.RGBA) {
	path := &canvas.Path{}
	path.MoveTo(tip[0], tip[1])
	path.LineTo(left[0], left[1])
	path.LineTo(right[0], right[1])
	path.Close()
	drawShape(ctx, path, fill, color.RGBA{}, 0)
}

func compassArrowFactory(element *layout.Element, _ *render.Context) (render.Drawer, error) {
	size, err := floatAttr(element, "size", 256)
	if err != nil || size <= 0 {
		return nil, fmt.Errorf("compass_arrow size must be positive")
	}
	textSize, err := floatAttr(element, "textsize", 32)
	if err != nil || textSize <= 0 {
		return nil, fmt.Errorf("compass_arrow textsize must be positive")
	}
	arrow, err := colorAttr(element, "arrow", color.RGBA{R: 255, G: 255, B: 255, A: 255})
	if err != nil {
		return nil, err
	}
	bg, err := colorAttr(element, "bg", color.RGBA{})
	if err != nil {
		return nil, err
	}
	text, err := colorAttr(element, "text", color.RGBA{R: 255, G: 255, B: 255, A: 255})
	if err != nil {
		return nil, err
	}
	outline, err := colorAttr(element, "outline", color.RGBA{A: 255})
	if err != nil {
		return nil, err
	}
	arrowOutline, err := colorAttr(element, "arrow-outline", color.RGBA{A: 255})
	if err != nil {
		return nil, err
	}
	return func(ctx *render.Context) error {
		radius := size / 2
		cx, cy := radius, radius
		ctx.Canvas.Push()
		ctx.Canvas.SetFillColor(nrgba(bg))
		ctx.Canvas.SetStrokeColor(nrgba(outline))
		ctx.Canvas.SetStrokeWidth(2)
		ctx.Canvas.DrawPath(cx, cy, canvas.Circle(radius-1))
		ctx.Canvas.Pop()
		for angle, label := range map[int]string{0: "N", 90: "E", 180: "S", 270: "W"} {
			x, y := roundPoint(cx, cy, radius*0.32, float64(angle))
			drawText(ctx, x, y+textSize/3, textSize, label, "center", text, color.RGBA{}, 0)
		}
		heading := 0.0
		if ctx.Sample.Course.Valid {
			heading = ctx.Sample.Course.Number
		}
		tipX, tipY := roundPoint(cx, cy, radius*0.46, heading)
		baseLX, baseLY := roundPoint(cx, cy, radius*0.1, heading-90)
		baseX, baseY := roundPoint(cx, cy, radius*0.1, heading)
		baseRX, baseRY := roundPoint(cx, cy, radius*0.1, heading+90)
		path := &canvas.Path{}
		path.MoveTo(tipX, tipY)
		path.LineTo(baseLX, baseLY)
		path.LineTo(baseX, baseY)
		path.LineTo(baseRX, baseRY)
		path.Close()
		drawShape(ctx, path, arrow, arrowOutline, 2)
		return nil
	}, nil
}

func gaugeArc(ctx *render.Context, cx, cy, radius, start, end float64, stroke color.RGBA, width float64) {
	steps := max(2, int(math.Abs(end-start)/3))
	points := make([][2]float64, 0, steps+1)
	for i := 0; i <= steps; i++ {
		angle := start + (end-start)*float64(i)/float64(steps)
		x, y := roundPoint(cx, cy, radius, angle)
		points = append(points, [2]float64{x, y})
	}
	drawLine(ctx, points, stroke, width)
}
