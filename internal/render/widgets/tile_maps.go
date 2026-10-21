package widgets

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"
	"sync"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/rasterizer"
	"video-overlay-cli/internal/activity"
	"video-overlay-cli/internal/layout"
	"video-overlay-cli/internal/maps"
	"video-overlay-cli/internal/render"
)

func movingMapFactory(element *layout.Element, _ *render.Context) (render.Drawer, error) {
	size, err := intAttr(element, "size", 256)
	if err != nil || size <= 0 || size > 4096 {
		return nil, fmt.Errorf("moving_map size must be between 1 and 4096")
	}
	showAttribution, err := boolAttr(element, "show-attribution", true)
	if err != nil {
		return nil, err
	}
	zoom, err := intAttr(element, "zoom", 16)
	if err != nil || zoom < 1 || zoom > 19 {
		return nil, fmt.Errorf("moving_map zoom must be between 1 and 19")
	}
	rotate, err := boolAttr(element, "rotate", true)
	if err != nil {
		return nil, err
	}
	radius, opacity, err := mapBorderAttrs(element, "moving_map")
	if err != nil {
		return nil, err
	}
	mask := newMapMask(size, size, radius, opacity)
	border := newMapBorder(size, size, radius)
	frameSize := int(math.Sqrt(2 * float64(size*size)))
	var cached *image.RGBA
	var lastPosition image.Point
	var lastHeading float64
	var lastHeadingValid bool
	var cacheMu sync.Mutex
	basePool := sync.Pool{New: func() any { return image.NewRGBA(image.Rect(0, 0, frameSize, frameSize)) }}
	return func(ctx *render.Context) error {
		point, ok := samplePoint(ctx.Sample)
		if !ok {
			return nil
		}
		heading := 0.0
		headingValid := false
		if ctx.Sample.Azimuth.Valid && !math.IsNaN(ctx.Sample.Azimuth.Number) && !math.IsInf(ctx.Sample.Azimuth.Number, 0) {
			heading = ctx.Sample.Azimuth.Number
			headingValid = true
		} else if ctx.Sample.Course.Valid && !math.IsNaN(ctx.Sample.Course.Number) && !math.IsInf(ctx.Sample.Course.Number, 0) {
			heading = ctx.Sample.Course.Number
			headingValid = true
		}
		if isPrivate(point, ctx.Privacy) {
			return nil
		}
		viewport, err := maps.NewViewport(point, zoom, image.Pt(frameSize, frameSize))
		if err != nil {
			return err
		}
		pixelX, pixelY := maps.ProjectPixel(point, zoom)
		position := image.Pt(int(math.Floor(pixelX)), int(math.Floor(pixelY)))
		cacheMu.Lock()
		updateHeading := rotate && (headingValid != lastHeadingValid || headingValid && heading != lastHeading)
		if cached != nil && position == lastPosition && !updateHeading {
			frame := cached
			cacheMu.Unlock()
			drawMapImage(ctx, frame)
			return nil
		}
		cacheMu.Unlock()
		base := basePool.Get().(*image.RGBA)
		defer basePool.Put(base)
		if err := maps.RenderInto(ctx.MapContext, maps.Request{Viewport: viewport, Config: ctx.MapConfig}, base); err != nil {
			return err
		}
		mapContext, raster := localCanvas(ctx, base)
		drawMapMarker(&mapContext, float64(frameSize)/2, float64(frameSize)/2, 6, color.RGBA{B: 255, A: 255}, color.RGBA{A: 255})
		raster.Close()
		var frame *image.RGBA
		if rotate && headingValid && math.Mod(heading, 360) != 0 {
			frame = image.NewRGBA(image.Rect(0, 0, size, size))
			rotateCropInto(frame, base, heading, size)
		} else {
			frame = cropCenter(base, size)
		}
		if showAttribution {
			drawMapAttribution(ctx, frame, ctx.MapConfig.Attribution)
		}
		applyMapBorder(frame, border)
		applyPrecomputedMapMask(frame, mask)
		cacheMu.Lock()
		cached, lastPosition, lastHeading, lastHeadingValid = frame, position, heading, headingValid
		cacheMu.Unlock()
		drawMapImage(ctx, frame)
		return nil
	}, nil
}

func journeyMapFactory(element *layout.Element, setup *render.Context) (render.Drawer, error) {
	size, err := intAttr(element, "size", 256)
	if err != nil || size <= 0 || size > 4096 {
		return nil, fmt.Errorf("journey_map size must be between 1 and 4096")
	}
	showAttribution, err := boolAttr(element, "show-attribution", true)
	if err != nil {
		return nil, err
	}
	radius, opacity, err := mapBorderAttrs(element, "journey_map")
	if err != nil {
		return nil, err
	}
	mask := newMapMask(size, size, radius, opacity)
	border := newMapBorder(size, size, radius)
	lineFill, err := colorAttr(element, "fill", color.RGBA{R: 255, A: 255})
	if err != nil {
		return nil, err
	}
	lineWidth, err := floatAttr(element, "line-width", 4)
	if err != nil || lineWidth < 0 {
		return nil, fmt.Errorf("journey_map line-width must not be negative")
	}
	markerFill, err := colorAttr(element, "loc-fill", color.RGBA{B: 255, A: 255})
	if err != nil {
		return nil, err
	}
	markerOutline, err := colorAttr(element, "loc-outline", color.RGBA{A: 255})
	if err != nil {
		return nil, err
	}
	markerSize, err := intAttr(element, "loc-size", 6)
	if err != nil || markerSize < 0 {
		return nil, fmt.Errorf("journey_map loc-size must not be negative")
	}
	segments := publicRouteSegments(setup.Activity, setup.Privacy)
	points := flattenRoute(segments)
	if len(points) == 0 {
		return func(*render.Context) error { return nil }, nil
	}
	viewport, err := maps.Fit(points, image.Pt(size, size), 18)
	if err != nil {
		return nil, err
	}
	for i := range segments {
		segments[i] = simplifyMapRoute(segments[i], viewport.Zoom)
	}
	var base *image.RGBA
	var cacheMu sync.Mutex
	return func(ctx *render.Context) error {
		cacheMu.Lock()
		if base == nil {
			rendered, err := maps.Render(ctx.MapContext, maps.Request{Viewport: viewport, Config: ctx.MapConfig})
			if err != nil {
				cacheMu.Unlock()
				return err
			}
			base = rendered
			mapContext, raster := localCanvas(ctx, base)
			drawMapRoutes(&mapContext, viewport, segments, lineFill, lineWidth)
			if showAttribution {
				drawText(&mapContext, float64(size-4), float64(size-14), 7, ctx.MapConfig.Attribution, "right", color.RGBA{R: 255, G: 255, B: 255, A: 255}, color.RGBA{A: 255}, 1)
			}
			raster.Close()
		}
		frame := cloneRGBA(base)
		cacheMu.Unlock()
		if point, ok := samplePoint(ctx.Sample); ok && !isPrivate(point, ctx.Privacy) {
			mapContext, raster := localCanvas(ctx, frame)
			pixelX, pixelY := viewport.Project(point)
			drawMapMarker(&mapContext, pixelX, pixelY, float64(markerSize), markerFill, markerOutline)
			raster.Close()
		}
		applyMapBorder(frame, border)
		applyPrecomputedMapMask(frame, mask)
		drawMapImage(ctx, frame)
		return nil
	}, nil
}

func movingJourneyMapFactory(element *layout.Element, setup *render.Context) (render.Drawer, error) {
	size, err := intAttr(element, "size", 256)
	if err != nil || size <= 0 || size > 4096 {
		return nil, fmt.Errorf("moving_journey_map size must be between 1 and 4096")
	}
	showAttribution, err := boolAttr(element, "show-attribution", true)
	if err != nil {
		return nil, err
	}
	zoom, err := intAttr(element, "zoom", 16)
	if err != nil || zoom < 1 || zoom > 19 {
		return nil, fmt.Errorf("moving_journey_map zoom must be between 1 and 19")
	}
	lineFill, err := colorAttr(element, "fill", color.RGBA{R: 255, A: 255})
	if err != nil {
		return nil, err
	}
	lineWidth, err := floatAttr(element, "line-width", 4)
	if err != nil || lineWidth < 0 {
		return nil, fmt.Errorf("moving_journey_map line-width must not be negative")
	}
	markerFill, err := colorAttr(element, "loc-fill", color.RGBA{B: 255, A: 255})
	if err != nil {
		return nil, err
	}
	markerOutline, err := colorAttr(element, "loc-outline", color.RGBA{A: 255})
	if err != nil {
		return nil, err
	}
	markerSize, err := intAttr(element, "loc-size", 6)
	if err != nil || markerSize < 0 {
		return nil, fmt.Errorf("moving_journey_map loc-size must not be negative")
	}
	segments := publicRouteSegments(setup.Activity, setup.Privacy)
	for i := range segments {
		segments[i] = simplifyMapRoute(segments[i], zoom)
	}
	return func(ctx *render.Context) error {
		point, ok := samplePoint(ctx.Sample)
		if !ok || isPrivate(point, ctx.Privacy) {
			return nil
		}
		viewport, err := maps.NewViewport(point, zoom, image.Pt(size, size))
		if err != nil {
			return err
		}
		frame, err := maps.Render(ctx.MapContext, maps.Request{Viewport: viewport, Config: ctx.MapConfig})
		if err != nil {
			return err
		}
		mapContext, raster := localCanvas(ctx, frame)
		drawMapRoutes(&mapContext, viewport, segments, lineFill, lineWidth)
		drawMapMarker(&mapContext, float64(size)/2, float64(size)/2, float64(markerSize), markerFill, markerOutline)
		if showAttribution {
			drawText(&mapContext, float64(size-4), float64(size-14), 7, ctx.MapConfig.Attribution, "right", color.RGBA{R: 255, G: 255, B: 255, A: 255}, color.RGBA{A: 255}, 1)
		}
		raster.Close()
		drawMapImage(ctx, frame)
		return nil
	}, nil
}

func mapBorderAttrs(element *layout.Element, name string) (int, float64, error) {
	radius, err := intAttr(element, "corner_radius", 0)
	if err != nil || radius < 0 {
		return 0, 0, fmt.Errorf("%s corner_radius must not be negative", name)
	}
	opacity, err := floatAttr(element, "opacity", 0.7)
	if err != nil || opacity < 0 || opacity > 1 {
		return 0, 0, fmt.Errorf("%s opacity must be between 0 and 1", name)
	}
	return radius, opacity, nil
}

func publicRouteSegments(a *activity.Activity, zones []maps.PrivacyZone) [][]maps.Point {
	segments := make([][]maps.Point, 0)
	current := make([]maps.Point, 0)
	flush := func() {
		if len(current) > 0 {
			segments = append(segments, current)
			current = nil
		}
	}
	for _, sample := range a.Samples {
		point, ok := samplePoint(sample)
		if !ok || isPrivate(point, zones) {
			flush()
			continue
		}
		current = append(current, point)
	}
	flush()
	return segments
}

func flattenRoute(segments [][]maps.Point) []maps.Point {
	var points []maps.Point
	for _, segment := range segments {
		points = append(points, segment...)
	}
	return points
}

func samplePoint(sample activity.Sample) (maps.Point, bool) {
	if !sample.Latitude.Valid || !sample.Longitude.Valid || math.IsNaN(sample.Latitude.Number) || math.IsInf(sample.Latitude.Number, 0) || math.IsNaN(sample.Longitude.Number) || math.IsInf(sample.Longitude.Number, 0) || sample.Latitude.Number < -90 || sample.Latitude.Number > 90 || sample.Longitude.Number < -180 || sample.Longitude.Number > 180 {
		return maps.Point{}, false
	}
	return maps.Point{Lon: sample.Longitude.Number, Lat: sample.Latitude.Number}, true
}

func isPrivate(point maps.Point, zones []maps.PrivacyZone) bool {
	for _, zone := range zones {
		if zone.Contains(point) {
			return true
		}
	}
	return false
}

func localCanvas(ctx *render.Context, target *image.RGBA) (render.Context, *rasterizer.Rasterizer) {
	raster := rasterizer.FromImage(target, canvas.DPMM(1), nil)
	local := *ctx
	local.Canvas = canvas.NewContext(raster)
	local.Canvas.SetCoordSystem(canvas.CartesianIV)
	return local, raster
}

func drawMapImage(ctx *render.Context, img *image.RGBA) {
	drawImage(ctx, 0, 0, img)
}

func drawMapRoutes(ctx *render.Context, viewport maps.Viewport, routes [][]maps.Point, fill color.RGBA, width float64) {
	for _, route := range routes {
		drawMapRoute(ctx, viewport, route, fill, width)
	}
}

func drawMapRoute(ctx *render.Context, viewport maps.Viewport, route []maps.Point, fill color.RGBA, width float64) {
	if width <= 0 || len(route) < 2 {
		return
	}
	projected := make([][2]float64, 0, len(route))
	for _, point := range route {
		x, y := viewport.Project(point)
		projected = append(projected, [2]float64{x, y})
	}
	drawLine(ctx, projected, fill, width)
}

func drawMapMarker(ctx *render.Context, x, y, radius float64, fill, outline color.RGBA) {
	ctx.Canvas.Push()
	ctx.Canvas.SetFillColor(nrgba(fill))
	ctx.Canvas.SetStrokeColor(nrgba(outline))
	ctx.Canvas.SetStrokeWidth(1)
	ctx.Canvas.DrawPath(x, y, circlePath(radius))
	ctx.Canvas.Pop()
}

func drawMapAttribution(ctx *render.Context, target *image.RGBA, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	local, raster := localCanvas(ctx, target)
	drawText(&local, float64(target.Bounds().Dx()-4), float64(target.Bounds().Dy()-14), 7, text, "right", color.RGBA{R: 255, G: 255, B: 255, A: 255}, color.RGBA{A: 255}, 1)
	raster.Close()
}

func cloneRGBA(source *image.RGBA) *image.RGBA {
	copy := image.NewRGBA(source.Bounds())
	copy.Pix = append(copy.Pix[:0], source.Pix...)
	return copy
}

type mapBorderPixel struct {
	x, y  int
	color color.RGBA
}

type mapBorder struct {
	pixels []mapBorderPixel
}

func newMapBorder(width, height, radius int) mapBorder {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	raster := rasterizer.FromImage(img, canvas.DPMM(1), nil)
	ctx := canvas.NewContext(raster)
	ctx.SetCoordSystem(canvas.CartesianIV)
	ctx.SetFillColor(color.RGBA{})
	ctx.SetStrokeColor(color.RGBA{A: 255})
	ctx.SetStrokeWidth(1)
	ctx.DrawPath(0, 0, canvas.RoundedRectangle(float64(width), float64(height), float64(min(radius, width/2, height/2))))
	raster.Close()

	border := mapBorder{}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			pixel := img.RGBAAt(x, y)
			if pixel.A != 0 {
				border.pixels = append(border.pixels, mapBorderPixel{x: x, y: y, color: pixel})
			}
		}
	}
	return border
}

func applyMapBorder(img *image.RGBA, border mapBorder) {
	bounds := img.Bounds()
	for _, pixel := range border.pixels {
		index := img.PixOffset(bounds.Min.X+pixel.x, bounds.Min.Y+pixel.y)
		source := pixel.color
		sourceAlpha := uint32(source.A) * 0x101
		blend := (0xffff - sourceAlpha) * 0x101
		for channel, sourceValue := range [...]uint8{source.R, source.G, source.B, source.A} {
			img.Pix[index+channel] = uint8((uint32(img.Pix[index+channel])*blend/0xffff + uint32(sourceValue)*0x101) >> 8)
		}
	}
}

type mapMaskRow struct {
	left, right int
}

type mapMask struct {
	rows     []mapMaskRow
	alpha    [256]uint8
	scale    bool
	identity bool
}

func newMapMask(width, height, radius int, opacity float64) mapMask {
	radius = min(radius, width/2, height/2)
	mask := mapMask{rows: make([]mapMaskRow, height), scale: opacity != 1, identity: radius == 0 && opacity == 1}
	if mask.identity {
		return mask
	}
	if mask.scale {
		for alpha := range mask.alpha {
			mask.alpha[alpha] = uint8(math.Round(float64(alpha) * opacity))
		}
	}
	for y := range mask.rows {
		left := 0
		if radius > 0 {
			cy := math.Max(float64(radius), math.Min(float64(height-radius), float64(y)+0.5))
			dy := float64(y) + 0.5 - cy
			halfWidth := math.Sqrt(math.Max(0, float64(radius*radius)-dy*dy))
			left = max(0, min(width/2, int(math.Ceil(float64(radius)-halfWidth-0.5))))
		}
		mask.rows[y] = mapMaskRow{left: left, right: width - left}
	}
	return mask
}

func applyPrecomputedMapMask(img *image.RGBA, mask mapMask) {
	if mask.identity {
		return
	}
	bounds := img.Bounds()
	width := bounds.Dx()
	for y, row := range mask.rows {
		start := img.PixOffset(bounds.Min.X, bounds.Min.Y+y)
		pixels := img.Pix[start : start+width*4]
		for x := 0; x < row.left; x++ {
			pixels[x*4+3] = 0
		}
		if mask.scale {
			for x := row.left; x < row.right; x++ {
				index := x * 4
				pixels[index] = mask.alpha[pixels[index]]
				pixels[index+1] = mask.alpha[pixels[index+1]]
				pixels[index+2] = mask.alpha[pixels[index+2]]
				pixels[index+3] = mask.alpha[pixels[index+3]]
			}
		}
		for x := row.right; x < width; x++ {
			pixels[x*4+3] = 0
		}
	}
}

func rotateImage(source *image.RGBA, degrees float64) *image.RGBA {
	degrees = math.Mod(degrees, 360)
	if degrees == 0 {
		return source
	}
	result := image.NewRGBA(source.Bounds())
	rotateImageInto(result, source, degrees)
	return result
}

func rotateImageInto(result, source *image.RGBA, degrees float64) {
	bounds := source.Bounds()
	if result.Bounds() != bounds {
		*result = *image.NewRGBA(bounds)
	} else {
		clear(result.Pix)
	}
	width, height := bounds.Dx(), bounds.Dy()
	angle := math.Mod(degrees, 360) * math.Pi / 180
	cosine, sine := math.Cos(angle), math.Sin(angle)
	cx, cy := float64(width-1)/2, float64(height-1)/2
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			dx, dy := float64(x)-cx, float64(y)-cy
			sx := cosine*dx - sine*dy + cx
			sy := sine*dx + cosine*dy + cy
			if sx >= 0 && sy >= 0 && sx < float64(width-1) && sy < float64(height-1) {
				x0, y0 := int(math.Floor(sx)), int(math.Floor(sy))
				x1, y1 := x0+1, y0+1
				fx, fy := sx-float64(x0), sy-float64(y0)
				p00 := source.PixOffset(bounds.Min.X+x0, bounds.Min.Y+y0)
				p10 := source.PixOffset(bounds.Min.X+x1, bounds.Min.Y+y0)
				p01 := source.PixOffset(bounds.Min.X+x0, bounds.Min.Y+y1)
				p11 := source.PixOffset(bounds.Min.X+x1, bounds.Min.Y+y1)
				dst := result.PixOffset(bounds.Min.X+x, bounds.Min.Y+y)
				for channel := 0; channel < 4; channel++ {
					value := float64(source.Pix[p00+channel])*(1-fx)*(1-fy) +
						float64(source.Pix[p10+channel])*fx*(1-fy) +
						float64(source.Pix[p01+channel])*(1-fx)*fy +
						float64(source.Pix[p11+channel])*fx*fy
					result.Pix[dst+channel] = uint8(math.Round(value))
				}
			}
		}
	}
}

func cropCenter(source *image.RGBA, size int) *image.RGBA {
	result := image.NewRGBA(image.Rect(0, 0, size, size))
	cropCenterInto(result, source, size)
	return result
}

func cropCenterInto(result, source *image.RGBA, size int) {
	bounds := source.Bounds()
	startX := bounds.Min.X + (bounds.Dx()-size)/2
	startY := bounds.Min.Y + (bounds.Dy()-size)/2
	resultBounds := result.Bounds()
	for y := 0; y < size; y++ {
		src := source.PixOffset(startX, startY+y)
		dst := result.PixOffset(resultBounds.Min.X, resultBounds.Min.Y+y)
		copy(result.Pix[dst:dst+size*4], source.Pix[src:src+size*4])
	}
}

func rotateCropInto(result, source *image.RGBA, degrees float64, size int) {
	resultBounds := result.Bounds()
	if resultBounds.Dx() != size || resultBounds.Dy() != size {
		*result = *image.NewRGBA(image.Rect(0, 0, size, size))
		resultBounds = result.Bounds()
	}
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	cropX, cropY := (width-size)/2, (height-size)/2
	angle := math.Mod(degrees, 360) * math.Pi / 180
	cosine, sine := math.Cos(angle), math.Sin(angle)
	cx, cy := float64(width-1)/2, float64(height-1)/2
	for y := 0; y < size; y++ {
		dy := float64(cropY+y) - cy
		for x := 0; x < size; x++ {
			dx := float64(cropX+x) - cx
			sx := cosine*dx - sine*dy + cx
			sy := sine*dx + cosine*dy + cy
			if sx >= 0 && sy >= 0 && sx < float64(width-1) && sy < float64(height-1) {
				x0, y0 := int(math.Floor(sx)), int(math.Floor(sy))
				x1, y1 := x0+1, y0+1
				fx, fy := sx-float64(x0), sy-float64(y0)
				p00 := source.PixOffset(bounds.Min.X+x0, bounds.Min.Y+y0)
				p10 := source.PixOffset(bounds.Min.X+x1, bounds.Min.Y+y0)
				p01 := source.PixOffset(bounds.Min.X+x0, bounds.Min.Y+y1)
				p11 := source.PixOffset(bounds.Min.X+x1, bounds.Min.Y+y1)
				dst := result.PixOffset(resultBounds.Min.X+x, resultBounds.Min.Y+y)
				for channel := 0; channel < 4; channel++ {
					value := float64(source.Pix[p00+channel])*(1-fx)*(1-fy) +
						float64(source.Pix[p10+channel])*fx*(1-fy) +
						float64(source.Pix[p01+channel])*(1-fx)*fy +
						float64(source.Pix[p11+channel])*fx*fy
					result.Pix[dst+channel] = uint8(math.Round(value))
				}
			} else {
				dst := result.PixOffset(resultBounds.Min.X+x, resultBounds.Min.Y+y)
				result.Pix[dst], result.Pix[dst+1], result.Pix[dst+2], result.Pix[dst+3] = 0, 0, 0, 0
			}
		}
	}
}

func simplifyRoute(points [][2]float64, epsilon float64) [][2]float64 {
	if len(points) <= 2 {
		return points
	}
	indices := simplifyRouteIndices(points, epsilon)
	result := make([][2]float64, 0, len(indices))
	for _, index := range indices {
		result = append(result, points[index])
	}
	return result
}

func simplifyMapRoute(points []maps.Point, zoom int) []maps.Point {
	if len(points) <= 2 {
		return points
	}
	projected := make([][2]float64, len(points))
	firstX, _ := maps.ProjectPixel(points[0], zoom)
	world := float64(maps.TileSize) * math.Pow(2, float64(zoom))
	for i, point := range points {
		x, y := maps.ProjectPixel(point, zoom)
		for x-firstX > world/2 {
			x -= world
		}
		for x-firstX < -world/2 {
			x += world
		}
		projected[i] = [2]float64{x, y}
	}
	indices := simplifyRouteIndices(projected, 1)
	result := make([]maps.Point, 0, len(indices))
	for _, index := range indices {
		result = append(result, points[index])
	}
	return result
}

func simplifyRouteIndices(points [][2]float64, epsilon float64) []int {
	if len(points) <= 2 {
		indices := make([]int, len(points))
		for i := range points {
			indices[i] = i
		}
		return indices
	}
	keep := make([]bool, len(points))
	keep[0], keep[len(points)-1] = true, true
	segments := [][2]int{{0, len(points) - 1}}
	for len(segments) > 0 {
		segment := segments[len(segments)-1]
		segments = segments[:len(segments)-1]
		maxDistance, index := 0.0, -1
		for i := segment[0] + 1; i < segment[1]; i++ {
			distance := pointLineDistance(points[i], points[segment[0]], points[segment[1]])
			if distance > maxDistance {
				maxDistance, index = distance, i
			}
		}
		if maxDistance >= epsilon {
			keep[index] = true
			segments = append(segments, [2]int{segment[0], index}, [2]int{index, segment[1]})
		}
	}
	result := make([]int, 0, len(points))
	for i := range points {
		if keep[i] {
			result = append(result, i)
		}
	}
	return result
}

func pointLineDistance(point, start, end [2]float64) float64 {
	dx, dy := end[0]-start[0], end[1]-start[1]
	if dx == 0 && dy == 0 {
		return math.Hypot(point[0]-start[0], point[1]-start[1])
	}
	return math.Abs(dx*(start[1]-point[1])-(start[0]-point[0])*dy) / math.Hypot(dx, dy)
}
