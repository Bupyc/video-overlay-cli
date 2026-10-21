package render

import (
	"container/list"
	"context"
	"image"
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
}

type Drawer func(*Context) error

type Factory func(*layout.Element, *Context) (Drawer, error)

type textCache struct {
	mu       sync.Mutex
	items    map[string]*cachedTextImage
	building map[string]*textImageBuild
	order    list.List
	maxItems int
}

const maxTextCacheItems = 1024
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
	return ctx.CachedTextImageWithLookahead(key, build, nil)
}

func (ctx *Context) CachedTextImageWithLookahead(key string, build func() (*image.RGBA, image.Point), lookahead func(<-chan struct{}, func(string, func() (*image.RGBA, image.Point)) bool)) (*image.RGBA, image.Point) {
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
			lookahead(pending.done, func(futureKey string, futureBuild func() (*image.RGBA, image.Point)) bool {
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

func (cache *textCache) prefetch(currentKey string, current *textImageBuild, key string, build func() (*image.RGBA, image.Point)) bool {
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
		cache.items = make(map[string]*cachedTextImage)
	}
	if cache.building == nil {
		cache.building = make(map[string]*textImageBuild)
	}
}

func (cache *textCache) build(key string, pending *textImageBuild, build func() (*image.RGBA, image.Point)) {
	pending.image, pending.offset = build()
	cache.mu.Lock()
	limit := cache.maxItems
	if limit <= 0 {
		limit = maxTextCacheItems
	}
	if len(cache.items) == limit {
		oldest := cache.order.Back()
		delete(cache.items, oldest.Value.(string))
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
			sa := uint32(alpha) * 0x101
			a := (0xffff - sa) * 0x101
			d[x] = uint8((uint32(d[x])*a/0xffff + uint32(s[x])*0x101) >> 8)
			d[x+1] = uint8((uint32(d[x+1])*a/0xffff + uint32(s[x+1])*0x101) >> 8)
			d[x+2] = uint8((uint32(d[x+2])*a/0xffff + uint32(s[x+2])*0x101) >> 8)
		}
		d0 += dst.Stride
		s0 += src.Stride
	}
}
