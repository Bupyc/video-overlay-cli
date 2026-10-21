package render

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/rasterizer"
	"video-overlay-cli/internal/activity"
	"video-overlay-cli/internal/layout"
	"video-overlay-cli/internal/maps"
)

type Background string

const (
	Transparent Background = "transparent"
	Black       Background = "black"
)

type Options struct {
	Width      int
	Height     int
	Background Background
	FontPath   string
	AssetDir   string
	MapContext context.Context
	MapConfig  maps.Config
	Privacy    []maps.PrivacyZone
}

type Renderer struct {
	opts      Options
	activity  *activity.Activity
	fonts     *canvas.FontFamily
	textCache *textCache
	drawer    Drawer
}

func New(opts Options, doc *layout.Document, a *activity.Activity, registry Registry) (*Renderer, error) {
	workers, err := NewWorkers(opts, doc, a, registry, 1)
	if err != nil {
		return nil, err
	}
	return workers[0], nil
}

func NewWorkers(opts Options, doc *layout.Document, a *activity.Activity, registry Registry, workerCount int) ([]*Renderer, error) {
	if workerCount <= 0 {
		return nil, fmt.Errorf("worker count must be positive")
	}
	maxInt := int(^uint(0) >> 1)
	if workerCount > maxInt/maxTextCacheItems {
		return nil, fmt.Errorf("worker count is too large")
	}
	r, err := newRenderer(opts, doc, a, registry, maxTextCacheItems*workerCount)
	if err != nil {
		return nil, err
	}
	workers := make([]*Renderer, workerCount)
	for i := range workers {
		workers[i] = r
	}
	return workers, nil
}

func newRenderer(opts Options, doc *layout.Document, a *activity.Activity, registry Registry, textCacheLimit int) (*Renderer, error) {
	if opts.Width <= 0 || opts.Height <= 0 {
		return nil, fmt.Errorf("render size must be positive")
	}
	maxInt := int(^uint(0) >> 1)
	if opts.Width > maxInt/opts.Height/4 {
		return nil, fmt.Errorf("render size is too large")
	}
	if opts.Background != Transparent && opts.Background != Black {
		return nil, fmt.Errorf("unknown render background %q", opts.Background)
	}
	if a == nil || len(a.Samples) == 0 {
		return nil, fmt.Errorf("activity has no samples")
	}
	if a.End.Before(a.Start) {
		return nil, fmt.Errorf("activity end precedes its start")
	}
	for i := 1; i < len(a.Samples); i++ {
		if a.Samples[i].Timestamp.Before(a.Samples[i-1].Timestamp) {
			return nil, fmt.Errorf("activity samples are not timestamp ordered")
		}
	}
	if doc == nil || doc.Root == nil {
		return nil, fmt.Errorf("layout has no root element")
	}
	if opts.MapContext == nil {
		opts.MapContext = context.Background()
	}
	opts.MapConfig = opts.MapConfig.WithDefaults()
	if err := layout.Validate(doc, registry.IsSupported); err != nil {
		return nil, err
	}
	fonts, err := loadFonts(opts.FontPath)
	if err != nil {
		return nil, err
	}
	textCache := &textCache{items: make(map[string]*cachedTextImage), building: make(map[string]*textImageBuild), maxItems: textCacheLimit}
	setup := &Context{
		Activity: a, Sample: a.Samples[0], Size: image.Pt(opts.Width, opts.Height), FontPath: opts.FontPath,
		Fonts: fonts, textCache: textCache, AssetDir: opts.AssetDir,
		MapContext: opts.MapContext, MapConfig: opts.MapConfig, Privacy: opts.Privacy,
	}
	drawer, err := prepare(doc.Root, registry, setup)
	if err != nil {
		return nil, err
	}
	return &Renderer{opts: opts, activity: a, fonts: fonts, textCache: textCache, drawer: drawer}, nil
}

func (r *Renderer) FrameAt(at time.Duration) (*image.RGBA, error) {
	return r.FrameAtContext(r.opts.MapContext, at)
}

func (r *Renderer) FrameAtContext(mapContext context.Context, at time.Duration) (*image.RGBA, error) {
	if at < 0 || at > r.activity.End.Sub(r.activity.Start) {
		return nil, fmt.Errorf("frame time %s is outside activity duration", at)
	}
	frame := image.NewRGBA(image.Rect(0, 0, r.opts.Width, r.opts.Height))
	if err := r.renderIntoContext(mapContext, at, frame, false); err != nil {
		return nil, err
	}
	return frame, nil
}

func (r *Renderer) FrameAtContextInto(mapContext context.Context, at time.Duration, frame *image.RGBA) error {
	if at < 0 || at > r.activity.End.Sub(r.activity.Start) {
		return fmt.Errorf("frame time %s is outside activity duration", at)
	}
	return r.renderIntoContext(mapContext, at, frame, true)
}

func (r *Renderer) renderIntoContext(mapContext context.Context, at time.Duration, frame *image.RGBA, reset bool) error {
	if frame == nil || frame.Bounds() != image.Rect(0, 0, r.opts.Width, r.opts.Height) {
		return fmt.Errorf("frame buffer bounds do not match renderer size %dx%d", r.opts.Width, r.opts.Height)
	}
	if reset && r.opts.Background == Transparent {
		clear(frame.Pix)
	}
	if r.opts.Background == Black {
		draw.Draw(frame, frame.Bounds(), image.NewUniform(color.RGBA{A: 255}), image.Point{}, draw.Src)
	}
	if mapContext == nil {
		mapContext = r.opts.MapContext
	}
	raster := rasterizer.FromImage(frame, canvas.DPMM(1), nil)
	context := &Context{
		At:         at,
		Activity:   r.activity,
		Sample:     sampleAt(r.activity, r.activity.Start.Add(at)),
		Size:       frame.Bounds().Size(),
		FontPath:   r.opts.FontPath,
		Fonts:      r.fonts,
		textCache:  r.textCache,
		AssetDir:   r.opts.AssetDir,
		MapContext: mapContext,
		MapConfig:  r.opts.MapConfig,
		Privacy:    r.opts.Privacy,
		Canvas:     canvas.NewContext(raster),
		frame:      frame,
		opaque:     r.opts.Background == Black,
	}
	context.Canvas.SetCoordSystem(canvas.CartesianIV)
	if err := r.drawer(context); err != nil {
		raster.Close()
		return err
	}
	raster.Close()
	return nil
}

func prepare(element *layout.Element, registry Registry, setup *Context) (Drawer, error) {
	switch element.Tag {
	case "layout", "composite", "translate":
		x, err := floatAttribute(element, "x", 0)
		if err != nil {
			return nil, err
		}
		y, err := floatAttribute(element, "y", 0)
		if err != nil {
			return nil, err
		}
		children, err := prepareChildren(element, registry, setup)
		if err != nil {
			return nil, err
		}
		return func(ctx *Context) error {
			ctx.Canvas.Push()
			defer ctx.Canvas.Pop()
			ctx.Canvas.Translate(x, y)
			return drawChildren(ctx, children)
		}, nil
	case "frame":
		return prepareFrame(element, registry, setup)
	case "component":
		componentType := elementAttribute(element, "type")
		factory, ok := registry.factory(componentType)
		if !ok {
			return nil, fmt.Errorf("component type %q is not registered", componentType)
		}
		drawer, err := factory(element, setup)
		if err != nil {
			return nil, fmt.Errorf("prepare component %q at line %d, column %d: %w", componentType, element.Location.Line, element.Location.Column, err)
		}
		x, err := floatAttribute(element, "x", 0)
		if err != nil {
			return nil, err
		}
		y, err := floatAttribute(element, "y", 0)
		if err != nil {
			return nil, err
		}
		return func(ctx *Context) error {
			ctx.Canvas.Push()
			defer ctx.Canvas.Pop()
			ctx.Canvas.Translate(x, y)
			return drawer(ctx)
		}, nil
	default:
		return nil, fmt.Errorf("unrecognized layout element <%s> at line %d, column %d", element.Tag, element.Location.Line, element.Location.Column)
	}
}

func prepareChildren(element *layout.Element, registry Registry, setup *Context) ([]Drawer, error) {
	children := make([]Drawer, 0, len(element.Children))
	for _, child := range element.Children {
		drawer, err := prepare(child, registry, setup)
		if err != nil {
			return nil, err
		}
		children = append(children, drawer)
	}
	return children, nil
}

func drawChildren(ctx *Context, children []Drawer) error {
	for _, drawer := range children {
		if err := drawer(ctx); err != nil {
			return err
		}
	}
	return nil
}

func prepareFrame(element *layout.Element, registry Registry, setup *Context) (Drawer, error) {
	x, err := floatAttribute(element, "x", 0)
	if err != nil {
		return nil, err
	}
	y, err := floatAttribute(element, "y", 0)
	if err != nil {
		return nil, err
	}
	width, err := integerAttribute(element, "width", -1)
	if err != nil || width <= 0 {
		return nil, fmt.Errorf("frame width must be a positive integer")
	}
	height, err := integerAttribute(element, "height", -1)
	if err != nil || height <= 0 {
		return nil, fmt.Errorf("frame height must be a positive integer")
	}
	opacity, err := floatAttribute(element, "opacity", 1)
	if err != nil || opacity < 0 || opacity > 1 {
		return nil, fmt.Errorf("frame opacity must be between 0 and 1")
	}
	radius, err := integerAttribute(element, "cr", 0)
	if err != nil || radius < 0 {
		return nil, fmt.Errorf("frame corner radius must not be negative")
	}
	fade, err := integerAttribute(element, "fo", 0)
	if err != nil || fade < 0 {
		return nil, fmt.Errorf("frame fade distance must not be negative")
	}
	fill, err := colorAttribute(element, "bg")
	if err != nil {
		return nil, err
	}
	outline, err := colorAttribute(element, "outline")
	if err != nil {
		return nil, err
	}
	children, err := prepareChildren(element, registry, setup)
	if err != nil {
		return nil, err
	}
	maskPixels := frameMask(width, height, radius, fade, opacity)
	mask := &image.Alpha{Pix: maskPixels, Stride: width, Rect: image.Rect(0, 0, width, height)}
	return func(ctx *Context) error {
		image := image.NewRGBA(image.Rect(0, 0, width, height))
		raster := rasterizer.FromImage(image, canvas.DPMM(1), nil)
		frameContext := *ctx
		frameContext.Canvas = canvas.NewContext(raster)
		frameContext.Canvas.SetCoordSystem(canvas.CartesianIV)
		frameContext.Size = image.Bounds().Size()
		if fill != nil {
			drawRect(frameContext.Canvas, 0, 0, float64(width), float64(height), float64(radius), *fill)
		}
		if err := drawChildren(&frameContext, children); err != nil {
			raster.Close()
			return err
		}
		if outline != nil {
			drawRoundedOutline(frameContext.Canvas, float64(width), float64(height), float64(radius), *outline)
		}
		raster.Close()
		if ctx.DrawCachedImageWithMask(x, y, image, mask) {
			return nil
		}
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				pixel := image.PixOffset(x, y)
				factor := uint16(maskPixels[y*width+x])
				image.Pix[pixel] = uint8(uint16(image.Pix[pixel]) * factor / 255)
				image.Pix[pixel+1] = uint8(uint16(image.Pix[pixel+1]) * factor / 255)
				image.Pix[pixel+2] = uint8(uint16(image.Pix[pixel+2]) * factor / 255)
				image.Pix[pixel+3] = uint8(uint16(image.Pix[pixel+3]) * factor / 255)
			}
		}
		ctx.Canvas.DrawImage(x, y, image, canvas.DPMM(1))
		return nil
	}, nil
}

func loadFonts(path string) (*canvas.FontFamily, error) {
	family := canvas.NewFontFamily("Overlay")
	if path == "" {
		path = defaultFontFile()
		if _, err := os.Stat(path); err != nil {
			if err := family.LoadSystemFont("Arial", canvas.FontBold); err != nil {
				return nil, fmt.Errorf("load default Arial font: %w", err)
			}
			return family, nil
		}
	}
	if err := family.LoadFontFile(path, canvas.FontRegular); err != nil {
		return nil, fmt.Errorf("load font %q: %w", path, err)
	}
	return family, nil
}

func defaultFontFile() string {
	if roboto, ok := canvas.FindSystemFont("Roboto", canvas.FontMedium); ok {
		return roboto
	}
	return "/System/Library/Fonts/Supplemental/Arial Bold.ttf"
}

func frameMask(width, height, radius, fade int, opacity float64) []uint8 {
	radius = min(radius, width/2, height/2)
	mask := make([]uint8, width*height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			alpha := opacity
			if radius > 0 {
				cx := math.Max(float64(radius), math.Min(float64(width-radius), float64(x)+0.5))
				cy := math.Max(float64(radius), math.Min(float64(height-radius), float64(y)+0.5))
				dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
				if dx*dx+dy*dy > float64(radius*radius) {
					alpha = 0
				}
			}
			if fade > 0 {
				margin := min(x, y, width-1-x, height-1-y)
				alpha *= math.Min(1, float64(margin)/float64(fade))
			}
			mask[y*width+x] = uint8(math.Round(255 * alpha))
		}
	}
	return mask
}

func drawRoundedOutline(ctx *canvas.Context, width, height, radius float64, outline color.RGBA) {
	ctx.Push()
	defer ctx.Pop()
	ctx.SetFillColor(color.RGBA{})
	ctx.SetStrokeColor(color.NRGBA{R: outline.R, G: outline.G, B: outline.B, A: outline.A})
	ctx.SetStrokeWidth(1)
	p := roundedPath(width, height, radius)
	ctx.DrawPath(0, 0, p)
}

func drawRect(ctx *canvas.Context, x, y, width, height, radius float64, fill color.RGBA) {
	ctx.Push()
	defer ctx.Pop()
	ctx.SetFillColor(color.NRGBA{R: fill.R, G: fill.G, B: fill.B, A: fill.A})
	ctx.SetStrokeColor(color.RGBA{})
	ctx.DrawPath(x, y, roundedPath(width, height, radius))
}

func roundedPath(width, height, radius float64) *canvas.Path {
	if radius <= 0 {
		path := &canvas.Path{}
		path.MoveTo(0, 0)
		path.LineTo(width, 0)
		path.LineTo(width, height)
		path.LineTo(0, height)
		path.Close()
		return path
	}
	return canvas.RoundedRectangle(width, height, math.Min(radius, math.Min(width, height)/2))
}

func integerAttribute(element *layout.Element, key string, fallback int) (int, error) {
	value, ok := attribute(element, key)
	if !ok {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("element <%s> has invalid %s=%q", element.Tag, key, value)
	}
	return parsed, nil
}

func floatAttribute(element *layout.Element, key string, fallback float64) (float64, error) {
	value, ok := attribute(element, key)
	if !ok {
		return fallback, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, fmt.Errorf("element <%s> has invalid %s=%q", element.Tag, key, value)
	}
	return parsed, nil
}

func colorAttribute(element *layout.Element, key string) (*color.RGBA, error) {
	value, ok := attribute(element, key)
	if !ok {
		return nil, nil
	}
	parts := strings.Split(value, ",")
	if len(parts) != 3 && len(parts) != 4 {
		return nil, fmt.Errorf("element <%s> has invalid %s color %q", element.Tag, key, value)
	}
	channels := [4]uint8{0, 0, 0, 255}
	for i, part := range parts {
		channel, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || channel < 0 || channel > 255 {
			return nil, fmt.Errorf("element <%s> has invalid %s color %q", element.Tag, key, value)
		}
		channels[i] = uint8(channel)
	}
	color := color.RGBA{R: channels[0], G: channels[1], B: channels[2], A: channels[3]}
	return &color, nil
}

func attribute(element *layout.Element, key string) (string, bool) {
	for _, attr := range element.Attributes {
		if attr.Name == key {
			return attr.Value, true
		}
	}
	return "", false
}

func sampleAt(a *activity.Activity, at time.Time) activity.Sample {
	i := sort.Search(len(a.Samples), func(i int) bool { return a.Samples[i].Timestamp.After(at) })
	if i == 0 {
		return a.Samples[0]
	}
	return a.Samples[i-1]
}

func elementAttribute(element *layout.Element, name string) string {
	for _, attr := range element.Attributes {
		if attr.Name == name {
			return attr.Value
		}
	}
	return ""
}
