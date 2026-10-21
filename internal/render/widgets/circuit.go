package widgets

import (
	"fmt"
	"image"
	"image/color"
	"math"

	"github.com/tdewolff/canvas"
	"video-overlay-cli/internal/layout"
	"video-overlay-cli/internal/maps"
	"video-overlay-cli/internal/render"
)

func circuitMapFactory(element *layout.Element, setup *render.Context) (render.Drawer, error) {
	size, err := floatAttr(element, "size", 256)
	if err != nil || size <= 0 {
		return nil, fmt.Errorf("circuit_map size must be positive")
	}
	fill, err := colorAttr(element, "fill", color.RGBA{R: 255, A: 255})
	if err != nil {
		return nil, err
	}
	outline, err := colorAttr(element, "outline", color.RGBA{R: 255, G: 255, B: 255, A: 255})
	if err != nil {
		return nil, err
	}
	lineWidth, err := floatAttr(element, "fill_width", 4)
	if err != nil || lineWidth <= 0 {
		return nil, fmt.Errorf("circuit_map fill_width must be positive")
	}
	outlineWidth, err := floatAttr(element, "outline_width", 0)
	if err != nil || outlineWidth < 0 || outlineWidth > lineWidth {
		return nil, fmt.Errorf("circuit_map outline_width must be between 0 and fill_width")
	}
	segments := publicRouteSegments(setup.Activity, setup.Privacy)
	points := flattenRoute(segments)
	if len(points) == 0 {
		if len(flattenRoute(publicRouteSegments(setup.Activity, nil))) == 0 {
			return nil, fmt.Errorf("circuit_map requires GPS coordinates")
		}
		return func(*render.Context) error { return nil }, nil
	}
	minLat, maxLat := math.Inf(1), math.Inf(-1)
	minLon, maxLon := math.Inf(1), math.Inf(-1)
	for _, point := range points {
		minLat = math.Min(minLat, point.Lat)
		maxLat = math.Max(maxLat, point.Lat)
		minLon = math.Min(minLon, point.Lon)
		maxLon = math.Max(maxLon, point.Lon)
	}
	centerLat, centerLon := (minLat+maxLat)/2, (minLon+maxLon)/2
	lonScale := math.Cos(centerLat * math.Pi / 180)
	mapWidth := math.Max((maxLon-minLon)*lonScale, 0.0000001)
	mapHeight := math.Max(maxLat-minLat, 0.0000001)
	scale := math.Min(size/mapWidth, size/mapHeight) * 0.9
	toPoint := func(point maps.Point) [2]float64 {
		return [2]float64{size/2 + (point.Lon-centerLon)*lonScale*scale, size/2 - (point.Lat-centerLat)*scale}
	}
	routes := make([][][2]float64, 0, len(segments))
	for _, segment := range segments {
		route := make([][2]float64, 0, len(segment))
		for _, point := range segment {
			route = append(route, toPoint(point))
		}
		routes = append(routes, simplifyRoute(route, lineWidth/8))
	}
	path := &canvas.Path{}
	for _, route := range routes {
		if len(route) < 2 {
			continue
		}
		path.MoveTo(route[0][0], route[0][1])
		for _, point := range route[1:] {
			path.LineTo(point[0], point[1])
		}
	}
	var routeImage *image.RGBA
	if size == math.Trunc(size) && size <= 4096 {
		routeImage = image.NewRGBA(image.Rect(0, 0, int(size), int(size)))
		local, raster := localCanvas(setup, routeImage)
		if outlineWidth > 0 {
			drawLinePath(&local, path, outline, lineWidth)
		}
		drawLinePath(&local, path, fill, math.Max(1, lineWidth-outlineWidth))
		raster.Close()
	}
	return func(ctx *render.Context) error {
		if routeImage != nil {
			drawMapImage(ctx, routeImage)
		} else {
			if outlineWidth > 0 {
				drawLinePath(ctx, path, outline, lineWidth)
			}
			drawLinePath(ctx, path, fill, math.Max(1, lineWidth-outlineWidth))
		}
		point, ok := samplePoint(ctx.Sample)
		if ok && !isPrivate(point, ctx.Privacy) {
			point := toPoint(point)
			ctx.Canvas.Push()
			ctx.Canvas.SetFillColor(color.RGBA{R: 255, G: 255, B: 255, A: 255})
			ctx.Canvas.SetStrokeColor(color.RGBA{R: 0, G: 0, B: 0, A: 255})
			ctx.Canvas.SetStrokeWidth(1)
			ctx.Canvas.DrawPath(point[0], point[1], circlePath(4))
			ctx.Canvas.Pop()
		}
		return nil
	}, nil
}
