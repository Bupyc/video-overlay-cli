package widgets

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"sync"

	"github.com/tdewolff/canvas"
	"video-overlay-cli/internal/layout"
	"video-overlay-cli/internal/maps"
	"video-overlay-cli/internal/render"
)

func cairoCircuitFactory(element *layout.Element, setup *render.Context) (render.Drawer, error) {
	size, err := intAttr(element, "size", 256)
	if err != nil || size <= 0 || size > 4096 {
		return nil, fmt.Errorf("cairo_circuit_map size must be between 1 and 4096")
	}
	rotation, err := intAttr(element, "rotate", 0)
	if err != nil {
		return nil, err
	}
	lineFill, err := colorAttr(element, "fill", color.RGBA{R: 255, G: 255, B: 255, A: 255})
	if err != nil {
		return nil, err
	}
	lineOutline, err := colorAttr(element, "outline", color.RGBA{R: 255, A: 255})
	if err != nil {
		return nil, err
	}
	lineWidth, err := floatAttr(element, "line-width", 0.01)
	if err != nil || lineWidth <= 0 {
		return nil, fmt.Errorf("cairo_circuit_map line-width must be positive")
	}
	markerFill, err := colorAttr(element, "loc-fill", color.RGBA{B: 255, A: 255})
	if err != nil {
		return nil, err
	}
	markerOutline, err := colorAttr(element, "loc-outline", color.RGBA{A: 255})
	if err != nil {
		return nil, err
	}
	markerSize, err := floatAttr(element, "loc-size", 0.011)
	if err != nil || markerSize < 0 {
		return nil, fmt.Errorf("cairo_circuit_map loc-size must not be negative")
	}
	segments := publicRouteSegments(setup.Activity, setup.Privacy)
	points := flattenRoute(segments)
	if len(points) == 0 {
		if len(flattenRoute(publicRouteSegments(setup.Activity, nil))) == 0 {
			return nil, fmt.Errorf("cairo_circuit_map requires GPS coordinates")
		}
		return func(*render.Context) error { return nil }, nil
	}
	minLat, maxLat := math.Inf(1), math.Inf(-1)
	minLon, maxLon := math.Inf(1), math.Inf(-1)
	for _, point := range points {
		minLat, maxLat = math.Min(minLat, point.Lat), math.Max(maxLat, point.Lat)
		minLon, maxLon = math.Min(minLon, point.Lon), math.Max(maxLon, point.Lon)
	}
	centerLat, centerLon := (minLat+maxLat)/2, (minLon+maxLon)/2
	extent := math.Max(maxLat-minLat, maxLon-minLon) * 1.1
	if extent < 1e-9 {
		extent = 1e-9
	}
	project := func(point maps.Point) [2]float64 {
		return [2]float64{float64(size)/2 + (point.Lat-centerLat)/extent*float64(size), float64(size)/2 + (point.Lon-centerLon)/extent*float64(size)}
	}
	routes := make([][][2]float64, 0, len(segments))
	for _, segment := range segments {
		projected := make([][2]float64, 0, len(segment))
		for _, point := range segment {
			projected = append(projected, project(point))
		}
		routes = append(routes, simplifyRoute(projected, float64(size)*lineWidth/8))
	}
	var routeImage *image.RGBA
	var routeOnce sync.Once
	return func(ctx *render.Context) error {
		routeOnce.Do(func() {
			routeImage = image.NewRGBA(image.Rect(0, 0, size, size))
			local, raster := localCanvas(ctx, routeImage)
			local.Canvas.Push()
			rotateCircuit(&local, rotation, float64(size))
			for _, route := range routes {
				drawCircuitLine(&local, route, lineOutline, lineWidth*float64(size))
				drawCircuitLine(&local, route, lineFill, lineWidth*float64(size)*0.75)
			}
			local.Canvas.Pop()
			raster.Close()
		})
		drawImage(ctx, 0, 0, routeImage)
		point, ok := samplePoint(ctx.Sample)
		if !ok || isPrivate(point, ctx.Privacy) {
			return nil
		}
		px, py := project(point)[0], project(point)[1]
		ctx.Canvas.Push()
		rotateCircuit(ctx, rotation, float64(size))
		drawGaugePath(ctx, circlePath(markerSize*float64(size)), markerFill, markerOutline, markerSize*float64(size), px, py)
		ctx.Canvas.Pop()
		return nil
	}, nil
}

func rotateCircuit(ctx *render.Context, degrees int, size float64) {
	cx, cy := size/2, size/2
	angle := float64(degrees) * math.Pi / 180
	scale := math.Max(math.Cos(angle)+math.Sin(angle), math.Cos(angle)-math.Sin(angle))
	ctx.Canvas.RotateAbout(float64(degrees), cx, cy)
	if scale > 1 {
		ctx.Canvas.ScaleAbout(1/scale, 1/scale, cx, cy)
	}
}

func drawCircuitLine(ctx *render.Context, points [][2]float64, stroke color.RGBA, width float64) {
	if len(points) < 2 || width <= 0 {
		return
	}
	path := &canvas.Path{}
	path.MoveTo(points[0][0], points[0][1])
	for _, point := range points[1:] {
		path.LineTo(point[0], point[1])
	}
	ctx.Canvas.Push()
	defer ctx.Canvas.Pop()
	ctx.Canvas.SetFillColor(color.RGBA{})
	ctx.Canvas.SetStrokeColor(vectorColor(stroke))
	ctx.Canvas.SetStrokeWidth(width)
	ctx.Canvas.SetStrokeCapper(canvas.ButtCap)
	ctx.Canvas.DrawPath(0, 0, path)
}
