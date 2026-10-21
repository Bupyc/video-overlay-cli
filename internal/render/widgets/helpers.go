package widgets

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/rasterizer"
	canvastext "github.com/tdewolff/canvas/text"
	font "github.com/tdewolff/font"
	"video-overlay-cli/internal/activity"
	"video-overlay-cli/internal/layout"
	"video-overlay-cli/internal/metrics"
	"video-overlay-cli/internal/render"
)

func attr(element *layout.Element, key, fallback string) string {
	for _, item := range element.Attributes {
		if item.Name == key {
			return item.Value
		}
	}
	return fallback
}

func floatAttr(element *layout.Element, key string, fallback float64) (float64, error) {
	value := attr(element, key, "")
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, fmt.Errorf("invalid %s=%q", key, value)
	}
	return parsed, nil
}

func intAttr(element *layout.Element, key string, fallback int) (int, error) {
	value := attr(element, key, "")
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s=%q", key, value)
	}
	return parsed, nil
}

func boolAttr(element *layout.Element, key string, fallback bool) (bool, error) {
	value := attr(element, key, "")
	if value == "" {
		return fallback, nil
	}
	switch strings.ToLower(value) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("invalid %s=%q", key, value)
	}
}

func nrgba(value color.RGBA) color.NRGBA {
	return color.NRGBA{R: value.R, G: value.G, B: value.B, A: value.A}
}

func colorAttr(element *layout.Element, key string, fallback color.RGBA) (color.RGBA, error) {
	value := attr(element, key, "")
	if value == "" {
		return fallback, nil
	}
	parts := strings.Split(value, ",")
	if len(parts) != 3 && len(parts) != 4 {
		return color.RGBA{}, fmt.Errorf("invalid %s color %q", key, value)
	}
	channels := [4]uint8{0, 0, 0, 255}
	for i, part := range parts {
		v, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || v < 0 || v > 255 {
			return color.RGBA{}, fmt.Errorf("invalid %s color %q", key, value)
		}
		channels[i] = uint8(v)
	}
	return color.RGBA{R: channels[0], G: channels[1], B: channels[2], A: channels[3]}, nil
}

func drawText(ctx *render.Context, x, y, size float64, text, align string, fill, outline color.RGBA, outlineWidth float64) {
	drawTextWithCache(ctx, x, y, size, text, align, fill, outline, outlineWidth, true)
}

type textLookahead func(*render.Context, string, <-chan struct{}, func(string) bool)

const maxTextLookahead = 5

func drawTextAtAscender(ctx *render.Context, x, y, size float64, text, align string, fill, outline color.RGBA, outlineWidth float64, cache bool) {
	drawTextAtAscenderWithLookahead(ctx, x, y, size, text, align, fill, outline, outlineWidth, cache, nil)
}

func drawTextAtAscenderWithLookahead(ctx *render.Context, x, y, size float64, text, align string, fill, outline color.RGBA, outlineWidth float64, cache bool, lookahead textLookahead) {
	ascent := ctx.Fonts.Face(size*72/25.4, nrgba(fill)).Metrics().Ascent
	drawTextWithLookahead(ctx, x, y+ascent, size, text, align, fill, outline, outlineWidth, cache, lookahead)
}

func drawTextWithCache(ctx *render.Context, x, y, size float64, text, align string, fill, outline color.RGBA, outlineWidth float64, cache bool) {
	drawTextWithLookahead(ctx, x, y, size, text, align, fill, outline, outlineWidth, cache, nil)
}

func drawTextWithLookahead(ctx *render.Context, x, y, size float64, text, align string, fill, outline color.RGBA, outlineWidth float64, cache bool, lookahead textLookahead) {
	if text == "" {
		return
	}
	textAlign := canvasTextAlign(align)
	ctx.Canvas.Push()
	defer ctx.Canvas.Pop()
	buildText := func(value string) *canvas.Text {
		face := ctx.Fonts.Face(size*72/25.4, nrgba(fill))
		if outlineWidth > 0 && outline.A > 0 {
			face = ctx.Fonts.Face(size*72/25.4, nrgba(fill), canvas.FontStroke(outlineWidth, nrgba(outline)))
		}
		return canvas.NewTextLine(face, value, textAlign)
	}
	if !cache {
		line := buildText(text)
		xFraction, yFraction := fractional(x), fractional(y)
		if bitmap, offset, ok := rasterizeTextGlyphs(ctx, line, xFraction, yFraction, fill, outline, outlineWidth); ok {
			drawTextBitmap(ctx, bitmap, image.Pt(int(math.Floor(x))+offset.X, int(math.Floor(y))+offset.Y))
		} else {
			ctx.Canvas.DrawText(x, y, line)
		}
		return
	}
	xFraction := x - math.Floor(x)
	yFraction := y - math.Floor(y)
	key := textImageKey(size, text, align, fill, outline, outlineWidth, xFraction, yFraction)
	build := func() (*image.RGBA, image.Point) {
		return rasterizeText(ctx, buildText(text), xFraction, yFraction, fill, outline, outlineWidth)
	}
	var bitmap *image.RGBA
	var offset image.Point
	if lookahead == nil {
		bitmap, offset = ctx.CachedTextImage(key, build)
	} else {
		bitmap, offset = ctx.CachedTextImageWithLookahead(key, build, func(done <-chan struct{}, prefetch func(string, func() (*image.RGBA, image.Point)) bool) {
			lookahead(ctx, text, done, func(value string) bool {
				if value != "" && value != text {
					futureKey := textImageKey(size, value, align, fill, outline, outlineWidth, xFraction, yFraction)
					return prefetch(futureKey, func() (*image.RGBA, image.Point) {
						return rasterizeText(ctx, buildText(value), xFraction, yFraction, fill, outline, outlineWidth)
					})
				}
				return false
			})
		})
	}
	drawTextBitmap(ctx, bitmap, image.Pt(int(math.Floor(x))+offset.X, int(math.Floor(y))+offset.Y))
}

func canvasTextAlign(align string) canvas.TextAlign {
	switch align {
	case "right":
		return canvas.Right
	case "centre", "center":
		return canvas.Center
	default:
		return canvas.Left
	}
}

func textImageKey(size float64, text, align string, fill, outline color.RGBA, outlineWidth, x, y float64) string {
	return fmt.Sprintf("%g\x00%s\x00%s\x00%v\x00%v\x00%g\x00%g\x00%g", size, text, align, fill, outline, outlineWidth, x, y)
}

func prepareStaticTextImage(setup *render.Context, size float64, text, align string, fill, outline color.RGBA, outlineWidth float64) (*image.RGBA, image.Point) {
	face := setup.Fonts.Face(size*72/25.4, nrgba(fill))
	if outlineWidth > 0 && outline.A > 0 {
		face = setup.Fonts.Face(size*72/25.4, nrgba(fill), canvas.FontStroke(outlineWidth, nrgba(outline)))
	}
	ascent := face.Metrics().Ascent
	y := ascent - math.Floor(ascent)
	key := textImageKey(size, text, align, fill, outline, outlineWidth, 0, y)
	bitmap, offset := setup.CachedTextImage(key, func() (*image.RGBA, image.Point) {
		return rasterizeText(setup, canvas.NewTextLine(face, text, canvasTextAlign(align)), 0, y, fill, outline, outlineWidth)
	})
	return bitmap, image.Pt(offset.X, int(math.Floor(ascent))+offset.Y)
}

func prepareStaticTopToBottomTextImage(setup *render.Context, size float64, text, align string, fill, outline color.RGBA, outlineWidth float64) (*image.RGBA, image.Point) {
	face := setup.Fonts.Face(size*72/25.4, nrgba(fill))
	if outlineWidth > 0 && outline.A > 0 {
		face = setup.Fonts.Face(size*72/25.4, nrgba(fill), canvas.FontStroke(outlineWidth, nrgba(outline)))
	}
	ascent := face.Metrics().Ascent
	y := ascent - math.Floor(ascent)
	key := textImageKey(size, text, "ttb:"+align, fill, outline, outlineWidth, 0, y)
	bitmap, offset := setup.CachedTextImage(key, func() (*image.RGBA, image.Point) {
		type glyphImage struct {
			bitmap   *image.RGBA
			position image.Point
		}
		runes := []rune(text)
		glyphs := make([]glyphImage, 0, len(runes))
		var bounds image.Rectangle
		for i, r := range runes {
			glyph, glyphOffset := rasterizeText(setup, canvas.NewTextLine(face, string(r), canvasTextAlign(align)), 0, y, fill, outline, outlineWidth)
			position := image.Pt(glyphOffset.X, int(math.Floor(ascent+float64(i)*face.Size))+glyphOffset.Y)
			glyphBounds := glyph.Bounds().Add(position)
			if i == 0 {
				bounds = glyphBounds
			} else {
				bounds = bounds.Union(glyphBounds)
			}
			glyphs = append(glyphs, glyphImage{glyph, position})
		}
		composed := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
		for _, glyph := range glyphs {
			blitGlyph(composed, glyph.bitmap, glyph.position.Sub(bounds.Min))
		}
		return composed, bounds.Min
	})
	return bitmap, offset
}

func drawTextBitmap(ctx *render.Context, bitmap *image.RGBA, position image.Point) {
	x, y := float64(position.X), float64(position.Y)
	if !ctx.DrawCachedImage(x, y, bitmap) {
		ctx.Canvas.DrawImage(x, y, bitmap, canvas.DPMM(1))
	}
}

func drawImage(ctx *render.Context, x, y float64, img image.Image) {
	if !ctx.DrawCachedImage(x, y, img) {
		ctx.Canvas.DrawImage(x, y, img, canvas.DPMM(1))
	}
}

func prefetchFutureLabels(ctx *render.Context, current string, label func(activity.Sample) string, prefetch func(string) bool, done <-chan struct{}) {
	if ctx.Activity == nil || len(ctx.Activity.Samples) == 0 {
		return
	}
	samples := ctx.Activity.Samples
	first := sort.Search(len(samples), func(i int) bool {
		return samples[i].Timestamp.After(ctx.Sample.Timestamp)
	})
	seen := [maxTextLookahead + 1]string{}
	seen[0] = current
	seenCount, count := 1, 0
	for i := first; i < len(samples) && count < maxTextLookahead; i++ {
		select {
		case <-done:
			return
		default:
		}
		value := label(samples[i])
		duplicate := false
		for _, previous := range seen[:seenCount] {
			if value == previous {
				duplicate = true
				break
			}
		}
		if duplicate || value == "" {
			continue
		}
		seen[seenCount] = value
		seenCount++
		count++
		if prefetch(value) {
			return
		}
	}
}

func rasterizeText(ctx *render.Context, line *canvas.Text, x, y float64, fill, outline color.RGBA, outlineWidth float64) (*image.RGBA, image.Point) {
	if bitmap, offset, ok := rasterizeTextGlyphs(ctx, line, x, y, fill, outline, outlineWidth); ok {
		return bitmap, offset
	}
	return rasterizeTextCanvas(line, x, y, outlineWidth)
}

func rasterizeTextCanvas(line *canvas.Text, x, y, outlineWidth float64) (*image.RGBA, image.Point) {
	bounds := line.Bounds()
	padding := math.Max(1, outlineWidth+1)
	x0, y0 := math.Floor(x+bounds.X0-padding), math.Floor(y-bounds.Y1-padding)
	x1, y1 := math.Ceil(x+bounds.X1+padding), math.Ceil(y-bounds.Y0+padding)
	bitmap := image.NewRGBA(image.Rect(0, 0, int(x1-x0), int(y1-y0)))
	raster := rasterizer.FromImage(bitmap, canvas.DPMM(1), nil)
	ctx := canvas.NewContext(raster)
	ctx.SetCoordSystem(canvas.CartesianIV)
	ctx.DrawText(x-x0, y-y0, line)
	raster.Close()
	return bitmap, image.Pt(int(x0), int(y0))
}

// shortcut: rotated, faux-style, or vertical glyph runs use Canvas; add bitmap transforms if those layouts need this fast path.
func rasterizeTextGlyphs(ctx *render.Context, line *canvas.Text, x, y float64, fill, outline color.RGBA, outlineWidth float64) (*image.RGBA, image.Point, bool) {
	bounds := line.Bounds()
	padding := math.Max(1, outlineWidth+1)
	x0, y0 := math.Floor(x+bounds.X0-padding), math.Floor(y-bounds.Y1-padding)
	x1, y1 := math.Ceil(x+bounds.X1+padding), math.Ceil(y-bounds.Y0+padding)
	bitmap := image.NewRGBA(image.Rect(0, 0, int(x1-x0), int(y1-y0)))
	valid := true
	glyphOutlineWidth := outlineWidth
	if outline.A == 0 {
		glyphOutlineWidth = 0
	}
	line.WalkSpans(func(spanX, spanY float64, span canvas.TextSpan) {
		if !valid {
			return
		}
		if !span.IsText() || span.Rotation != canvastext.NoRotation || span.Face.FauxBold != 0 || span.Face.FauxItalic != 0 {
			valid = false
			return
		}
		lineY := spanY - span.Face.MmPerEm*float64(span.Face.YOffset)
		hintShift := 0.0
		if span.Face.Hinting != font.NoHinting {
			dy := float64(bitmap.Bounds().Dy()) - y + y0 + lineY
			hintShift = float64(int(dy+0.5)) - dy
		}
		var penX, penY int32
		for _, glyph := range span.Glyphs {
			if glyph.Vertical {
				valid = false
				return
			}
			glyphX := spanX + span.Face.MmPerEm*float64(penX+glyph.XOffset)
			glyphY := spanY + span.Face.MmPerEm*float64(penY+glyph.YOffset)
			baseX, baseY := x+glyphX, y-glyphY-hintShift
			xPhase, yPhase := fractional(baseX), fractional(baseY)
			key := fmt.Sprintf("glyph\x00%p\x00%g\x00%d\x00%g\x00%g\x00%g", span.Face.Font, span.Face.MmPerEm, glyph.ID, glyphOutlineWidth, xPhase, yPhase)
			glyphMasks, glyphOffset := ctx.CachedTextImage(key, func() (*image.RGBA, image.Point) {
				return rasterizeGlyphMasks(span.Face, glyph.ID, xPhase, yPhase, glyphOutlineWidth)
			})
			if glyphMasks == nil {
				valid = false
				return
			}
			position := image.Pt(int(math.Floor(baseX))+glyphOffset.X-int(x0), int(math.Floor(baseY))+glyphOffset.Y-int(y0))
			if !glyphMasks.Bounds().Empty() {
				mergeGlyphMasks(bitmap, glyphMasks, position)
			}
			penX += glyph.XAdvance
			penY += glyph.YAdvance
		}
	})
	if valid {
		colorizeGlyphMasks(bitmap, fill, outline)
	}
	return bitmap, image.Pt(int(x0), int(y0)), valid
}

func fractional(value float64) float64 {
	return value - math.Floor(value)
}

func mergeGlyphMasks(dst, src *image.RGBA, position image.Point) {
	r := image.Rectangle{Min: position, Max: position.Add(src.Bounds().Size())}.Intersect(dst.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		dstOffset := dst.PixOffset(r.Min.X, y)
		srcOffset := src.PixOffset(src.Bounds().Min.X+r.Min.X-position.X, src.Bounds().Min.Y+y-position.Y)
		for x := r.Min.X; x < r.Max.X; x++ {
			dst.Pix[dstOffset] = unionCoverage(dst.Pix[dstOffset], src.Pix[srcOffset])
			dst.Pix[dstOffset+1] = unionCoverage(dst.Pix[dstOffset+1], src.Pix[srcOffset+1])
			dstOffset += 4
			srcOffset += 4
		}
	}
}

func unionCoverage(a, b uint8) uint8 {
	return uint8(255 - (uint16(255-a)*uint16(255-b)+127)/255)
}

func blitGlyph(dst, src *image.RGBA, position image.Point) {
	r := image.Rectangle{Min: position, Max: position.Add(src.Bounds().Size())}.Intersect(dst.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		dx := dst.PixOffset(r.Min.X, y)
		sx := src.PixOffset(src.Bounds().Min.X+r.Min.X-position.X, src.Bounds().Min.Y+y-position.Y)
		for x := 0; x < r.Dx(); x++ {
			if src.Pix[sx+3] != 0 {
				copy(dst.Pix[dx:dx+4], src.Pix[sx:sx+4])
			}
			dx += 4
			sx += 4
		}
	}
}

func rasterizeGlyphBitmap(face *canvas.FontFace, glyphID uint16, x, y float64, fill, outline color.RGBA, outlineWidth float64) (*image.RGBA, image.Point) {
	bitmap, offset := rasterizeGlyphMasks(face, glyphID, x, y, outlineWidth)
	colorizeGlyphMasks(bitmap, fill, outline)
	return bitmap, offset
}

type glyphGeometryKey struct {
	font         *canvas.Font
	ppem         uint16
	glyphID      uint16
	size         float64
	outlineWidth float64
}

type glyphGeometry struct {
	path    *canvas.Path
	outline *canvas.Path
	bounds  canvas.Rect
}

type glyphGeometryBuild struct {
	done     chan struct{}
	geometry *glyphGeometry
}

const maxGlyphGeometryCacheEntries = 512

var glyphGeometries = struct {
	mu       sync.Mutex
	items    map[glyphGeometryKey]*glyphGeometry
	building map[glyphGeometryKey]*glyphGeometryBuild
}{}

var glyphMaskPool = sync.Pool{New: func() any { return &image.RGBA{} }}

func cachedGlyphGeometry(face *canvas.FontFace, glyphID uint16, outlineWidth float64) *glyphGeometry {
	key := glyphGeometryKey{face.Font, face.PPEM(canvas.DPMM(1)), glyphID, face.MmPerEm, outlineWidth}
	glyphGeometries.mu.Lock()
	if geometry := glyphGeometries.items[key]; geometry != nil {
		glyphGeometries.mu.Unlock()
		return geometry
	}
	if build := glyphGeometries.building[key]; build != nil {
		glyphGeometries.mu.Unlock()
		<-build.done
		return build.geometry
	}
	build := &glyphGeometryBuild{done: make(chan struct{})}
	if glyphGeometries.items == nil {
		glyphGeometries.items = make(map[glyphGeometryKey]*glyphGeometry)
		glyphGeometries.building = make(map[glyphGeometryKey]*glyphGeometryBuild)
	}
	glyphGeometries.building[key] = build
	glyphGeometries.mu.Unlock()

	path := canvas.NewPathFromData(make([]float64, 0, 3*256))
	_ = face.Font.GlyphPath(path, glyphID, key.ppem, 0, 0, face.MmPerEm, font.NoHinting)
	geometry := &glyphGeometry{path: path, bounds: path.Bounds()}
	if !path.Empty() && outlineWidth > 0 {
		geometry.outline = path.Offset(outlineWidth, canvas.Tolerance).Not(path)
	}

	glyphGeometries.mu.Lock()
	if len(glyphGeometries.items) >= maxGlyphGeometryCacheEntries {
		for oldKey := range glyphGeometries.items {
			delete(glyphGeometries.items, oldKey)
			break
		}
	}
	glyphGeometries.items[key] = geometry
	build.geometry = geometry
	delete(glyphGeometries.building, key)
	close(build.done)
	glyphGeometries.mu.Unlock()
	return geometry
}

func pooledGlyphMask(bounds image.Rectangle) *image.RGBA {
	mask := glyphMaskPool.Get().(*image.RGBA)
	width, height := bounds.Dx(), bounds.Dy()
	length := 4 * width * height
	if cap(mask.Pix) < length {
		mask.Pix = make([]byte, length)
	} else {
		mask.Pix = mask.Pix[:length]
		clear(mask.Pix)
	}
	mask.Stride = 4 * width
	mask.Rect = bounds
	return mask
}

func releaseGlyphMask(mask *image.RGBA) {
	if cap(mask.Pix) <= 256*1024 {
		mask.Pix = mask.Pix[:0]
		mask.Stride = 0
		mask.Rect = image.Rectangle{}
		glyphMaskPool.Put(mask)
	}
}

func rasterizeGlyphMasks(face *canvas.FontFace, glyphID uint16, x, y, outlineWidth float64) (*image.RGBA, image.Point) {
	geometry := cachedGlyphGeometry(face, glyphID, outlineWidth)
	path := geometry.path
	if path.Empty() {
		return image.NewRGBA(image.Rectangle{}), image.Point{}
	}
	padding := math.Max(1, outlineWidth+1)
	bounds := geometry.bounds
	x0, y0 := math.Floor(x+bounds.X0-padding), math.Floor(y-bounds.Y1-padding)
	x1, y1 := math.Ceil(x+bounds.X1+padding), math.Ceil(y-bounds.Y0+padding)
	bitmap := image.NewRGBA(image.Rect(0, 0, int(x1-x0), int(y1-y0)))
	matrix := canvas.Identity.Translate(x-x0, float64(bitmap.Bounds().Dy())-y+y0)
	style := canvas.DefaultStyle
	style.Fill = canvas.Paint{Color: color.RGBA{R: 255, G: 255, B: 255, A: 255}}
	raster := rasterizer.FromImage(bitmap, canvas.DPMM(1), nil)
	raster.RenderPath(path, style, matrix)
	raster.Close()
	for i := 0; i < len(bitmap.Pix); i += 4 {
		bitmap.Pix[i+1] = bitmap.Pix[i+3]
		bitmap.Pix[i], bitmap.Pix[i+2], bitmap.Pix[i+3] = 0, 0, 0
	}
	if geometry.outline != nil && !geometry.outline.Empty() {
		outlineMask := pooledGlyphMask(bitmap.Bounds())
		raster := rasterizer.FromImage(outlineMask, canvas.DPMM(1), nil)
		raster.RenderPath(geometry.outline, style, matrix)
		raster.Close()
		for i := 0; i < len(bitmap.Pix); i += 4 {
			bitmap.Pix[i] = outlineMask.Pix[i+3]
		}
		releaseGlyphMask(outlineMask)
	}
	return bitmap, image.Pt(int(x0), int(y0))
}

func colorizeGlyphMasks(bitmap *image.RGBA, fill, outline color.RGBA) {
	fillColor := color.RGBAModel.Convert(nrgba(fill)).(color.RGBA)
	outlineColor := color.RGBAModel.Convert(nrgba(outline)).(color.RGBA)
	for i := 0; i < len(bitmap.Pix); i += 4 {
		outlineCoverage, fillCoverage := bitmap.Pix[i], bitmap.Pix[i+1]
		outlineAlpha := multiplyAlpha(outlineCoverage, outlineColor.A)
		fillAlpha := multiplyAlpha(fillCoverage, fillColor.A)
		inverseFillAlpha := uint8(255 - fillAlpha)
		bitmap.Pix[i] = fillPixel(fillColor.R, outlineColor.R, fillCoverage, outlineCoverage, inverseFillAlpha)
		bitmap.Pix[i+1] = fillPixel(fillColor.G, outlineColor.G, fillCoverage, outlineCoverage, inverseFillAlpha)
		bitmap.Pix[i+2] = fillPixel(fillColor.B, outlineColor.B, fillCoverage, outlineCoverage, inverseFillAlpha)
		bitmap.Pix[i+3] = uint8(min(255, int(fillAlpha+multiplyAlpha(outlineAlpha, inverseFillAlpha))))
	}
}

func multiplyAlpha(value, alpha uint8) uint8 {
	return uint8((uint16(value)*uint16(alpha) + 127) / 255)
}

func fillPixel(fill, outline, fillCoverage, outlineCoverage, inverseFillAlpha uint8) uint8 {
	return uint8(min(255, int(multiplyAlpha(fill, fillCoverage)+multiplyAlpha(multiplyAlpha(outline, outlineCoverage), inverseFillAlpha))))
}

const maxPooledPathData = 3 * 4096

var linePathPool = sync.Pool{New: func() any {
	return canvas.NewPathFromData(make([]float64, 0, 3*256))
}}

func drawLine(ctx *render.Context, points [][2]float64, stroke color.RGBA, width float64) {
	if len(points) < 2 {
		return
	}
	path := linePathPool.Get().(*canvas.Path)
	path.Reset()
	defer func() {
		if cap(path.Data()) <= maxPooledPathData {
			path.Reset()
			linePathPool.Put(path)
		}
	}()
	path.MoveTo(points[0][0], points[0][1])
	for _, point := range points[1:] {
		path.LineTo(point[0], point[1])
	}
	drawLinePath(ctx, path, stroke, width)
}

func addLinePolygon(path *canvas.Path, x1, y1, x2, y2, width float64) {
	dx, dy := x2-x1, y2-y1
	length := math.Hypot(dx, dy)
	if length == 0 {
		return
	}
	nx, ny := -dy/length*width/2, dx/length*width/2
	path.MoveTo(x1+nx, y1+ny)
	path.LineTo(x2+nx, y2+ny)
	path.LineTo(x2-nx, y2-ny)
	path.LineTo(x1-nx, y1-ny)
	path.Close()
}

func drawLinePath(ctx *render.Context, path *canvas.Path, stroke color.RGBA, width float64) {
	if path == nil || width <= 0 {
		return
	}
	ctx.Canvas.Push()
	defer ctx.Canvas.Pop()
	ctx.Canvas.SetFillColor(color.RGBA{})
	ctx.Canvas.SetStrokeColor(nrgba(stroke))
	ctx.Canvas.SetStrokeWidth(width)
	ctx.Canvas.DrawPath(0, 0, path)
}

func drawShape(ctx *render.Context, path *canvas.Path, fill, outline color.RGBA, outlineWidth float64) {
	ctx.Canvas.Push()
	defer ctx.Canvas.Pop()
	ctx.Canvas.SetFillColor(nrgba(fill))
	ctx.Canvas.SetStrokeColor(nrgba(outline))
	ctx.Canvas.SetStrokeWidth(outlineWidth)
	ctx.Canvas.DrawPath(0, 0, path)
}

func metric(ctx *render.Context, element *layout.Element) (activity.Value, string, error) {
	name := attr(element, "metric", "")
	value, err := metrics.ValueAt(ctx.Sample, name)
	return value, attr(element, "units", ""), err
}

func convert(value activity.Value, units, metricName string) activity.Value {
	if !value.Valid {
		return value
	}
	n := value.Number
	switch strings.ToLower(units) {
	case "", "m", "m/s", "w", "bpm", "spm", "rpm", "deg", "°", "%", "c", "°c":
	case "km":
		n /= 1000
	case "mi", "mile", "miles":
		n /= 1609.344
	case "ft", "feet":
		n *= 3.280839895
	case "km/h", "kph":
		n *= 3.6
	case "speed", "mph":
		n *= 2.236936292
	case "knots", "knot", "kt":
		n *= 1.943844492
	case "pace_km", "min/km":
		if metricName == "pace" {
			n = math.Min(n, 500)
			break
		}
		return metrics.PaceMinutesPerKM(value)
	case "pace_mi", "min/mi":
		if metricName == "pace" {
			n *= 1.609344
		} else {
			if n <= 0 {
				return activity.Value{}
			}
			n = 1609.344 / (60 * n)
		}
		if n > 500 {
			n = 500
		}
	case "km/h_vertical", "m/h":
		n *= 3600
	case "ft/h":
		n *= 3600 * 3.280839895
	case "f", "°f":
		n = n*9/5 + 32
	case "w/kg":
		return activity.Value{}
	}
	return activity.Value{Number: n, Valid: true}
}

func metricText(value activity.Value, units, format string, metricName string) string {
	if !value.Valid || math.IsNaN(value.Number) || math.IsInf(value.Number, 0) {
		if strings.Contains(strings.ToLower(units), "pace") || metricName == "pace" {
			return "—"
		}
		return "-"
	}
	value = convert(value, units, metricName)
	if !value.Valid {
		return "—"
	}
	if format == "pace" || strings.Contains(strings.ToLower(units), "pace") {
		seconds := int(math.Ceil(value.Number * 60))
		return fmt.Sprintf("%d:%02d", seconds/60, seconds%60)
	}
	if format == "" {
		format = ".2f"
	}
	if strings.HasPrefix(format, ".") || strings.HasPrefix(format, "0") {
		format = "%" + format
	} else if !strings.HasPrefix(format, "%") {
		format = "%" + format
	}
	return fmt.Sprintf(format, value.Number)
}

func unitText(units, metricName string) string {
	switch strings.ToLower(units) {
	case "speed":
		return "MPH"
	case "alt", "altitude":
		return "M"
	case "distance":
		return "MI"
	case "temp", "temperature":
		return "°C"
	}
	if units != "" {
		return strings.ToUpper(strings.ReplaceAll(units, "_", "/"))
	}
	switch metricName {
	case "speed":
		return "MPH"
	case "power":
		return "W"
	case "hr":
		return "BPM"
	case "cadence", "running_cadence":
		return "SPM"
	case "alt", "odo", "codo", "dist":
		return "M"
	case "temp", "temperature":
		return "°C"
	case "gradient":
		return "%"
	default:
		return ""
	}
}

func timeFormat(value string) string {
	replacer := strings.NewReplacer(
		"%Y", "2006", "%y", "06", "%m", "01", "%d", "02", "%H", "15", "%I", "03", "%M", "04", "%S", "05",
		"%f", "000000", "%j", "002", "%a", "Mon", "%A", "Monday", "%b", "Jan", "%B", "January", "%p", "PM", "%z", "-0700", "%Z", "MST", "%%", "%",
	)
	return replacer.Replace(value)
}

func truncateText(value string, count int) string {
	if count <= 0 {
		return value
	}
	runes := []rune(value)
	if count >= len(runes) {
		return ""
	}
	return string(runes[:len(runes)-count])
}

func roundPoint(cx, cy, radius, degrees float64) (float64, float64) {
	radians := degrees * math.Pi / 180
	return cx + radius*math.Sin(radians), cy - radius*math.Cos(radians)
}
