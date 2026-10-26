package render

import (
	"container/list"
	"context"
	"image"
	"image/color"
	"image/draw"
	"math"
	"sync"
	"time"

	"github.com/tdewolff/canvas"
	"video-overlay-cli/internal/activity"
	"video-overlay-cli/internal/layout"
	"video-overlay-cli/internal/maps"
)

type Context struct {
	At         time.Duration
	Activity   *activity.Activity
	Sample     activity.Sample
	Size       image.Point
	FontPath   string
	Fonts      *canvas.FontFamily
	AssetDir   string
	MapContext context.Context
	MapConfig  maps.Config
	Privacy    []maps.PrivacyZone
	Canvas     *canvas.Context
	textCache  *textCache
	frame      *image.RGBA
	opaque     bool
	pathFill   *pathFillRenderer
}

type Drawer func(*Context) error

type Factory func(*layout.Element, *Context) (Drawer, error)

type TextImageCacheKey struct {
	Text         string
	Align        string
	Size         float64
	Fill         color.RGBA
	Outline      color.RGBA
	OutlineWidth float64
	X            float64
	Y            float64
}

type textImageCacheKey struct {
	kind                uint8
	raw, text, align    string
	size, outline, x, y uint64
	fill, outlineColor  color.RGBA
}

type textCache struct {
	mu       sync.Mutex
	items    map[textImageCacheKey]*cachedTextImage
	building map[textImageCacheKey]*textImageBuild
	order    list.List
	maxItems int
}

const maxTextCacheItems = 1024
const maxSharedTextCacheItems = 4 * maxTextCacheItems
const maxTextLookaheadBuilds = 5

type cachedTextImage struct {
	image  *image.RGBA
	offset image.Point
	order  *list.Element
}

type textImageBuild struct {
	done           chan struct{}
	image          *image.RGBA
	offset         image.Point
	lookaheadCount int
}

func (ctx *Context) CachedTextImage(key string, build func() (*image.RGBA, image.Point)) (*image.RGBA, image.Point) {
	return ctx.cachedTextImage(textImageCacheKey{raw: key}, build, nil)
}

func (ctx *Context) LookupCachedTextImage(key string) (*image.RGBA, image.Point, bool) {
	return ctx.lookupCachedTextImage(textImageCacheKey{raw: key})
}

func (ctx *Context) CachedTextImageByKey(key TextImageCacheKey, build func() (*image.RGBA, image.Point)) (*image.RGBA, image.Point) {
	return ctx.cachedTextImage(key.cacheKey(), build, nil)
}

func (ctx *Context) LookupCachedTextImageByKey(key TextImageCacheKey) (*image.RGBA, image.Point, bool) {
	return ctx.lookupCachedTextImage(key.cacheKey())
}

func (ctx *Context) lookupCachedTextImage(key textImageCacheKey) (*image.RGBA, image.Point, bool) {
	if ctx.textCache == nil {
		return nil, image.Point{}, false
	}
	cache := ctx.textCache
	cache.mu.Lock()
	cache.initialize()
	if cached := cache.items[key]; cached != nil {
		cache.order.MoveToFront(cached.order)
		cache.mu.Unlock()
		return cached.image, cached.offset, true
	}
	cache.mu.Unlock()
	return nil, image.Point{}, false
}

func (ctx *Context) CachedTextImageWithLookahead(key string, build func() (*image.RGBA, image.Point), lookahead func(<-chan struct{}, func(string, func() (*image.RGBA, image.Point)) bool)) (*image.RGBA, image.Point) {
	var cacheLookahead func(<-chan struct{}, func(textImageCacheKey, func() (*image.RGBA, image.Point)) bool)
	if lookahead != nil {
		cacheLookahead = func(done <-chan struct{}, prefetch func(textImageCacheKey, func() (*image.RGBA, image.Point)) bool) {
			lookahead(done, func(futureKey string, futureBuild func() (*image.RGBA, image.Point)) bool {
				return prefetch(textImageCacheKey{raw: futureKey}, futureBuild)
			})
		}
	}
	return ctx.cachedTextImage(textImageCacheKey{raw: key}, build, cacheLookahead)
}

func (ctx *Context) CachedTextImageByKeyWithLookahead(key TextImageCacheKey, build func() (*image.RGBA, image.Point), lookahead func(<-chan struct{}, func(TextImageCacheKey, func() (*image.RGBA, image.Point)) bool)) (*image.RGBA, image.Point) {
	var cacheLookahead func(<-chan struct{}, func(textImageCacheKey, func() (*image.RGBA, image.Point)) bool)
	if lookahead != nil {
		cacheLookahead = func(done <-chan struct{}, prefetch func(textImageCacheKey, func() (*image.RGBA, image.Point)) bool) {
			lookahead(done, func(futureKey TextImageCacheKey, futureBuild func() (*image.RGBA, image.Point)) bool {
				return prefetch(futureKey.cacheKey(), futureBuild)
			})
		}
	}
	return ctx.cachedTextImage(key.cacheKey(), build, cacheLookahead)
}

func (key TextImageCacheKey) cacheKey() textImageCacheKey {
	return textImageCacheKey{
		kind: 1, text: key.Text, align: key.Align,
		size: math.Float64bits(key.Size), outline: math.Float64bits(key.OutlineWidth),
		x: math.Float64bits(key.X), y: math.Float64bits(key.Y),
		fill: key.Fill, outlineColor: key.Outline,
	}
}

func (ctx *Context) cachedTextImage(key textImageCacheKey, build func() (*image.RGBA, image.Point), lookahead func(<-chan struct{}, func(textImageCacheKey, func() (*image.RGBA, image.Point)) bool)) (*image.RGBA, image.Point) {
	if ctx.textCache == nil {
		return build()
	}
	cache := ctx.textCache
	cache.mu.Lock()
	cache.initialize()
	if cached := cache.items[key]; cached != nil {
		cache.order.MoveToFront(cached.order)
		cache.mu.Unlock()
		return cached.image, cached.offset
	}
	if pending := cache.building[key]; pending != nil {
		cache.mu.Unlock()
		if lookahead != nil {
			lookahead(pending.done, func(futureKey textImageCacheKey, futureBuild func() (*image.RGBA, image.Point)) bool {
				return cache.prefetch(key, pending, futureKey, futureBuild)
			})
		}
		<-pending.done
		return pending.image, pending.offset
	}
	pending := &textImageBuild{done: make(chan struct{})}
	cache.building[key] = pending
	cache.mu.Unlock()

	cache.build(key, pending, build)
	return pending.image, pending.offset
}

func (cache *textCache) prefetch(currentKey textImageCacheKey, current *textImageBuild, key textImageCacheKey, build func() (*image.RGBA, image.Point)) bool {
	cache.mu.Lock()
	cache.initialize()
	if cache.building[currentKey] != current || current.lookaheadCount >= maxTextLookaheadBuilds || key == currentKey || cache.items[key] != nil || cache.building[key] != nil {
		cache.mu.Unlock()
		return false
	}
	current.lookaheadCount++
	pending := &textImageBuild{done: make(chan struct{})}
	cache.building[key] = pending
	cache.mu.Unlock()
	cache.build(key, pending, build)
	return true
}

func (cache *textCache) initialize() {
	if cache.items == nil {
		cache.items = make(map[textImageCacheKey]*cachedTextImage)
	}
	if cache.building == nil {
		cache.building = make(map[textImageCacheKey]*textImageBuild)
	}
}

func (cache *textCache) build(key textImageCacheKey, pending *textImageBuild, build func() (*image.RGBA, image.Point)) {
	pending.image, pending.offset = build()
	cache.mu.Lock()
	limit := cache.maxItems
	if limit <= 0 {
		limit = maxTextCacheItems
	}
	if len(cache.items) == limit {
		oldest := cache.order.Back()
		delete(cache.items, oldest.Value.(textImageCacheKey))
		cache.order.Remove(oldest)
	}
	cached := &cachedTextImage{image: pending.image, offset: pending.offset}
	cached.order = cache.order.PushFront(key)
	cache.items[key] = cached
	delete(cache.building, key)
	close(pending.done)
	cache.mu.Unlock()
}

func (ctx *Context) DrawCachedImage(x, y float64, img image.Image) bool {
	return ctx.drawCachedImage(x, y, img, nil)
}

func (ctx *Context) DrawCachedImageWithMask(x, y float64, img image.Image, mask image.Image) bool {
	return ctx.drawCachedImage(x, y, img, mask)
}

func (ctx *Context) drawCachedImage(x, y float64, img image.Image, mask image.Image) bool {
	if ctx.Canvas == nil || ctx.frame == nil || img == nil {
		return false
	}
	view := ctx.Canvas.View()
	if ctx.Canvas.CoordView() != canvas.Identity || view[0][0] != 1 || view[0][1] != 0 || view[1][0] != 0 || view[1][1] != 1 {
		return false
	}
	bounds := img.Bounds()
	size := bounds.Size()
	if size.X <= 0 || size.Y <= 0 || mask != nil && mask.Bounds().Size() != size || ctx.Canvas.Width() != float64(ctx.frame.Bounds().Dx()) || ctx.Canvas.Height() != float64(ctx.frame.Bounds().Dy()) {
		return false
	}
	coord := ctx.Canvas.CoordView().Dot(canvas.Point{X: x, Y: y})
	matrix := ctx.Canvas.CoordSystemView().Mul(view).Translate(coord.X, coord.Y).ReflectYAbout(float64(size.Y) / 2)
	origin := matrix.Dot(canvas.Point{Y: float64(size.Y)})
	x, y = origin.X, float64(ctx.frame.Bounds().Dy())-origin.Y
	if x != math.Trunc(x) || y != math.Trunc(y) {
		return false
	}
	point := image.Pt(int(x), int(y))
	destination := image.Rectangle{Min: point, Max: point.Add(size)}
	if mask != nil {
		draw.DrawMask(ctx.frame, destination, img, bounds.Min, mask, mask.Bounds().Min, draw.Over)
	} else if rgba, ok := img.(*image.RGBA); ok && ctx.opaque {
		drawRGBAOverOpaque(ctx.frame, rgba, point)
	} else {
		draw.Draw(ctx.frame, destination, img, bounds.Min, draw.Over)
	}
	return true
}

func drawRGBAOverOpaque(dst, src *image.RGBA, point image.Point) {
	srcBounds := src.Bounds()
	r := image.Rectangle{Min: point, Max: point.Add(srcBounds.Size())}.Intersect(dst.Bounds())
	if r.Empty() {
		return
	}
	sx, sy := srcBounds.Min.X+r.Min.X-point.X, srcBounds.Min.Y+r.Min.Y-point.Y
	d0, s0 := dst.PixOffset(r.Min.X, r.Min.Y), src.PixOffset(sx, sy)
	for y := 0; y < r.Dy(); y++ {
		d, s := dst.Pix[d0:], src.Pix[s0:]
		for x := 0; x < r.Dx()*4; x += 4 {
			alpha := s[x+3]
			if alpha == 0 {
				continue
			}
			if alpha == 255 {
				d[x], d[x+1], d[x+2] = s[x], s[x+1], s[x+2]
				continue
			}
			inverseAlpha := uint32(255 - alpha)
			red := uint32(d[x]) * inverseAlpha
			green := uint32(d[x+1]) * inverseAlpha
			blue := uint32(d[x+2]) * inverseAlpha
			d[x] = uint8(uint32(s[x]) + ((red + 2*red/255 + uint32(s[x])) >> 8))
			d[x+1] = uint8(uint32(s[x+1]) + ((green + 2*green/255 + uint32(s[x+1])) >> 8))
			d[x+2] = uint8(uint32(s[x+2]) + ((blue + 2*blue/255 + uint32(s[x+2])) >> 8))
		}
		d0 += dst.Stride
		s0 += src.Stride
	}
}
