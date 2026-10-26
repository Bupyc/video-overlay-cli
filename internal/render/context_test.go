package render

import (
	"bytes"
	"image"
	"image/color"
	"sync"
	"testing"
	"time"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/rasterizer"
)

func TestCachedTextImageBuildsAndReusesBitmap(t *testing.T) {
	ctx := &Context{textCache: &textCache{items: make(map[textImageCacheKey]*cachedTextImage)}}
	wantImage := image.NewRGBA(image.Rect(0, 0, 2, 2))
	wantOffset := image.Pt(-1, 2)
	builds := 0
	build := func() (*image.RGBA, image.Point) {
		builds++
		return wantImage, wantOffset
	}
	if _, _, ok := ctx.LookupCachedTextImage("same text"); ok {
		t.Fatal("LookupCachedTextImage() found an image before it was built")
	}

	firstImage, firstOffset := ctx.CachedTextImage("same text", build)
	secondImage, secondOffset := ctx.CachedTextImage("same text", build)
	if builds != 1 || firstImage != wantImage || secondImage != wantImage || firstOffset != wantOffset || secondOffset != wantOffset {
		t.Fatalf("CachedTextImage() builds=%d first=(%p,%v) second=(%p,%v)", builds, firstImage, firstOffset, secondImage, secondOffset)
	}
	if gotImage, gotOffset, ok := ctx.LookupCachedTextImage("same text"); !ok || gotImage != wantImage || gotOffset != wantOffset {
		t.Fatalf("LookupCachedTextImage() = (%p, %v, %t), want (%p, %v, true)", gotImage, gotOffset, ok, wantImage, wantOffset)
	}
}

func TestCachedTextImageByKeyUsesStructuredFieldsWithoutAllocations(t *testing.T) {
	ctx := &Context{textCache: &textCache{maxItems: 2}}
	key := TextImageCacheKey{Text: "speed", Align: "center", Size: 18, Fill: color.RGBA{R: 255, A: 255}, X: 0.25}
	want := image.NewRGBA(image.Rect(0, 0, 2, 2))
	got, gotOffset := ctx.CachedTextImageByKey(key, func() (*image.RGBA, image.Point) { return want, image.Pt(-1, 2) })
	if got != want || gotOffset != image.Pt(-1, 2) {
		t.Fatal("CachedTextImageByKey() did not return the built image and offset")
	}
	if _, _, ok := ctx.LookupCachedTextImageByKey(key); !ok {
		t.Fatal("LookupCachedTextImageByKey() missed an existing image")
	}
	differentKey := key
	differentKey.X = 0.5
	if _, _, ok := ctx.LookupCachedTextImageByKey(differentKey); ok {
		t.Fatal("LookupCachedTextImageByKey() matched a different subpixel phase")
	}
	allocs := testing.AllocsPerRun(100, func() {
		if _, _, ok := ctx.LookupCachedTextImageByKey(key); !ok {
			t.Fatal("LookupCachedTextImageByKey() missed a warmed image")
		}
	})
	if allocs != 0 {
		t.Fatalf("structured key lookup allocated %.1f objects, want 0", allocs)
	}
}

func TestDrawOwnedFilledPathMatchesCanvas(t *testing.T) {
	for _, background := range []color.RGBA{{}, {A: 255}} {
		want := image.NewRGBA(image.Rect(0, 0, 80, 60))
		got := image.NewRGBA(want.Bounds())
		for i := 3; i < len(want.Pix); i += 4 {
			want.Pix[i], got.Pix[i] = background.A, background.A
		}
		path := canvas.NewPathFromData(nil)
		path.MoveTo(4.25, 5.5)
		path.LineTo(54.75, 8.25)
		path.LineTo(30.5, 40.75)
		path.LineTo(16, 28)
		path.Close()
		fill := color.NRGBA{R: 91, G: 113, B: 146, A: 173}

		wantRaster := rasterizer.FromImage(want, canvas.DPMM(1), nil)
		wantCanvas := canvas.NewContext(wantRaster)
		wantCanvas.SetCoordSystem(canvas.CartesianIV)
		wantCanvas.Translate(3.25, 2.5)
		wantCanvas.Scale(1.1, 0.9)
		wantCanvas.SetFillColor(fill)
		wantCanvas.DrawPath(0, 0, path)
		wantRaster.Close()

		gotRaster := rasterizer.FromImage(got, canvas.DPMM(1), nil)
		gotCanvas := canvas.NewContext(gotRaster)
		gotCanvas.SetCoordSystem(canvas.CartesianIV)
		gotCanvas.Translate(3.25, 2.5)
		gotCanvas.Scale(1.1, 0.9)
		ctx := &Context{Canvas: gotCanvas, pathFill: newPathFillRenderer(got)}
		if !ctx.DrawOwnedFilledPath(0, 0, path, fill) {
			t.Fatal("DrawOwnedFilledPath() did not use the reusable path renderer")
		}
		gotRaster.Close()
		if !bytes.Equal(got.Pix, want.Pix) {
			t.Fatal("owned path rendering differs from Canvas")
		}
	}
}

func TestDrawOwnedFilledPathReusesInputStorage(t *testing.T) {
	frame := image.NewRGBA(image.Rect(0, 0, 80, 60))
	raster := rasterizer.FromImage(frame, canvas.DPMM(1), nil)
	canvasContext := canvas.NewContext(raster)
	canvasContext.SetCoordSystem(canvas.CartesianIV)
	ctx := &Context{Canvas: canvasContext, pathFill: newPathFillRenderer(frame)}
	path := canvas.NewPathFromData(nil)
	path.MoveTo(1, 1)
	path.LineTo(70, 3)
	path.LineTo(40, 55)
	path.LineTo(5, 45)
	path.Close()
	fill := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	firstY := path.Data()[2]
	if !ctx.DrawOwnedFilledPath(0, 0, path, fill) {
		t.Fatal("DrawOwnedFilledPath() did not use the reusable path renderer")
	}
	if path.Data()[2] == firstY {
		t.Fatal("DrawOwnedFilledPath() did not transform its input path in place")
	}
	allocs := testing.AllocsPerRun(100, func() {
		ctx.DrawOwnedFilledPath(0, 0, path, fill)
	})
	if allocs > 2 {
		t.Fatalf("DrawOwnedFilledPath() allocations after warmup = %.1f, want at most 2", allocs)
	}
}

func TestCachedTextImageEvictsLeastRecentlyUsed(t *testing.T) {
	ctx := &Context{textCache: &textCache{items: make(map[textImageCacheKey]*cachedTextImage), maxItems: 2}}
	var built []string
	get := func(key string) {
		ctx.CachedTextImage(key, func() (*image.RGBA, image.Point) {
			built = append(built, key)
			return image.NewRGBA(image.Rect(0, 0, 1, 1)), image.Point{}
		})
	}
	get("first")
	get("second")
	get("first")
	get("third")
	get("first")
	get("second")
	if len(built) != 4 || built[3] != "second" {
		t.Fatalf("cache builds = %v, want first/second/third/second after evicting least-recently-used entry", built)
	}
}

func TestCachedTextImageBuildsDifferentKeysConcurrently(t *testing.T) {
	ctx := &Context{textCache: &textCache{items: make(map[textImageCacheKey]*cachedTextImage)}}
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var wait sync.WaitGroup
	for _, key := range []string{"first", "second"} {
		wait.Add(1)
		go func(key string) {
			defer wait.Done()
			ctx.CachedTextImage(key, func() (*image.RGBA, image.Point) {
				entered <- struct{}{}
				<-release
				return image.NewRGBA(image.Rect(0, 0, 1, 1)), image.Point{}
			})
		}(key)
	}

	concurrent := true
	for range 2 {
		select {
		case <-entered:
		case <-time.After(time.Second):
			concurrent = false
		}
	}
	close(release)
	wait.Wait()
	if !concurrent {
		t.Fatal("different cache keys blocked on one cache-wide build lock")
	}
}

func TestCachedTextImageLookaheadWorkersShareFiveFutureBuilds(t *testing.T) {
	ctx := &Context{textCache: &textCache{items: make(map[textImageCacheKey]*cachedTextImage)}}
	currentStarted := make(chan struct{})
	currentRelease := make(chan struct{})
	currentReady := make(chan struct{})
	currentImage := image.NewRGBA(image.Rect(0, 0, 1, 1))
	imageFor := func() (*image.RGBA, image.Point) {
		return image.NewRGBA(image.Rect(0, 0, 1, 1)), image.Point{}
	}
	var wait sync.WaitGroup
	wait.Add(1)
	go func() {
		defer wait.Done()
		ctx.CachedTextImageWithLookahead("current", func() (*image.RGBA, image.Point) {
			close(currentStarted)
			<-currentRelease
			return currentImage, image.Point{}
		}, nil)
		close(currentReady)
	}()
	<-currentStarted

	prefetchStarted := make(chan string, 6)
	prefetchRelease := make(chan struct{})
	keys := []string{"next-1", "next-2", "next-3", "next-4", "next-5", "next-6"}
	lookahead := func(done <-chan struct{}, prefetch func(string, func() (*image.RGBA, image.Point)) bool) {
		for _, key := range keys {
			select {
			case <-done:
				return
			default:
			}
			candidate := key
			if prefetch(candidate, func() (*image.RGBA, image.Point) {
				prefetchStarted <- candidate
				<-prefetchRelease
				return imageFor()
			}) {
				return
			}
		}
	}
	waiterImages := make(chan *image.RGBA, 6)
	for range 6 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			got, _ := ctx.CachedTextImageWithLookahead("current", imageFor, lookahead)
			waiterImages <- got
		}()
	}

	started := make(map[string]bool)
	for range 5 {
		select {
		case key := <-prefetchStarted:
			if started[key] {
				t.Errorf("future key %q was built more than once", key)
			}
			started[key] = true
		case <-time.After(time.Second):
			close(currentRelease)
			close(prefetchRelease)
			wait.Wait()
			t.Fatal("waiting workers did not claim separate future keys")
		}
	}
	close(currentRelease)
	<-currentReady
	close(prefetchRelease)
	wait.Wait()
	if len(started) != 5 {
		t.Fatalf("future keys started = %v, want five distinct keys", started)
	}
	for range 6 {
		if got := <-waiterImages; got != currentImage {
			t.Fatalf("waiter received image %p, want current image %p", got, currentImage)
		}
	}
	ctx.textCache.mu.Lock()
	lastWasBuilt := ctx.textCache.items[textImageCacheKey{raw: "next-6"}] != nil
	ctx.textCache.mu.Unlock()
	if lastWasBuilt {
		t.Fatal("lookahead built more than five future labels for one current label")
	}
}

func TestDrawCachedImageUsesExactPixelBlitWhenAligned(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 3, 2))
	source.SetRGBA(1, 0, color.RGBA{R: 70, G: 20, B: 10, A: 120})
	source.SetRGBA(2, 1, color.RGBA{R: 40, G: 60, B: 20, A: 80})

	got := image.NewRGBA(image.Rect(0, 0, 16, 16))
	gotRaster := rasterizer.FromImage(got, canvas.DPMM(1), nil)
	gotCanvas := canvas.NewContext(gotRaster)
	gotCanvas.SetCoordSystem(canvas.CartesianIV)
	gotCanvas.Translate(4, 6)
	ctx := &Context{Canvas: gotCanvas, frame: got}
	if !ctx.DrawCachedImage(2, 3, source) {
		t.Fatal("DrawCachedImage() did not use the aligned pixel blit")
	}
	gotRaster.Close()

	want := image.NewRGBA(image.Rect(0, 0, 16, 16))
	wantRaster := rasterizer.FromImage(want, canvas.DPMM(1), nil)
	wantCanvas := canvas.NewContext(wantRaster)
	wantCanvas.SetCoordSystem(canvas.CartesianIV)
	wantCanvas.Translate(4, 6)
	wantCanvas.DrawImage(2, 3, source, canvas.DPMM(1))
	wantRaster.Close()

	if !bytes.Equal(got.Pix, want.Pix) {
		t.Fatal("aligned pixel blit differs from Canvas.DrawImage")
	}
}

func TestDrawCachedImageOnOpaqueFrameMatchesCanvas(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 3, 2))
	source.SetRGBA(0, 0, color.RGBA{R: 70, G: 20, B: 10, A: 120})
	source.SetRGBA(2, 1, color.RGBA{R: 40, G: 60, B: 20, A: 80})

	got := image.NewRGBA(image.Rect(0, 0, 16, 16))
	want := image.NewRGBA(got.Bounds())
	for i := 3; i < len(got.Pix); i += 4 {
		got.Pix[i], want.Pix[i] = 255, 255
	}
	gotRaster := rasterizer.FromImage(got, canvas.DPMM(1), nil)
	gotCanvas := canvas.NewContext(gotRaster)
	gotCanvas.SetCoordSystem(canvas.CartesianIV)
	gotCanvas.Translate(4, 6)
	ctx := &Context{Canvas: gotCanvas, frame: got, opaque: true}
	if !ctx.DrawCachedImage(2, 3, source) {
		t.Fatal("DrawCachedImage() did not use the aligned pixel blit")
	}
	gotRaster.Close()

	wantRaster := rasterizer.FromImage(want, canvas.DPMM(1), nil)
	wantCanvas := canvas.NewContext(wantRaster)
	wantCanvas.SetCoordSystem(canvas.CartesianIV)
	wantCanvas.Translate(4, 6)
	wantCanvas.DrawImage(2, 3, source, canvas.DPMM(1))
	wantRaster.Close()

	if !bytes.Equal(got.Pix, want.Pix) {
		t.Fatal("opaque pixel blit differs from Canvas.DrawImage")
	}
}

func BenchmarkDrawRGBAOverOpaque(b *testing.B) {
	source := image.NewRGBA(image.Rect(0, 0, 512, 128))
	destination := image.NewRGBA(source.Bounds())
	for i := 3; i < len(destination.Pix); i += 4 {
		destination.Pix[i] = 255
	}
	for y := 0; y < source.Bounds().Dy(); y++ {
		for x := 0; x < source.Bounds().Dx(); x++ {
			alpha := byte((x*13 + y*7) % 256)
			offset := source.PixOffset(x, y)
			source.Pix[offset] = byte(uint16(alpha) * 180 / 255)
			source.Pix[offset+1] = byte(uint16(alpha) * 120 / 255)
			source.Pix[offset+2] = byte(uint16(alpha) * 60 / 255)
			source.Pix[offset+3] = alpha
		}
	}
	b.SetBytes(int64(len(source.Pix)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		drawRGBAOverOpaque(destination, source, image.Point{})
	}
}

func TestDrawRGBAOverOpaqueMatchesPreviousBlend(t *testing.T) {
	const side = 256
	bounds := image.Rect(0, 0, side, side)
	source, destination, want := image.NewRGBA(bounds), image.NewRGBA(bounds), image.NewRGBA(bounds)
	for alpha := 0; alpha <= 255; alpha++ {
		for pixel := 0; pixel < side*side; pixel++ {
			offset := pixel * 4
			low, high := byte(pixel), byte(pixel>>8)
			source.Pix[offset], source.Pix[offset+1], source.Pix[offset+2] = high, high+89, high*3+7
			destination.Pix[offset], destination.Pix[offset+1], destination.Pix[offset+2] = low, low+37, low*5+11
			source.Pix[offset+3], destination.Pix[offset+3], want.Pix[offset+3] = byte(alpha), 255, 255
			for channel := 0; channel < 3; channel++ {
				s, d := uint32(source.Pix[offset+channel]), uint32(destination.Pix[offset+channel])
				value := byte(d)
				if alpha == 255 {
					value = byte(s)
				} else if alpha != 0 {
					sa := uint32(alpha) * 0x101
					a := (0xffff - sa) * 0x101
					value = uint8((d*a/0xffff + s*0x101) >> 8)
				}
				want.Pix[offset+channel] = value
			}
		}
		drawRGBAOverOpaque(destination, source, image.Point{})
		if !bytes.Equal(destination.Pix, want.Pix) {
			t.Fatalf("blend differs from previous implementation at alpha %d", alpha)
		}
	}
}

func TestDrawCachedImageDefersNonPixelAlignedImage(t *testing.T) {
	frame := image.NewRGBA(image.Rect(0, 0, 16, 16))
	raster := rasterizer.FromImage(frame, canvas.DPMM(1), nil)
	canvasContext := canvas.NewContext(raster)
	canvasContext.SetCoordSystem(canvas.CartesianIV)
	ctx := &Context{Canvas: canvasContext, frame: frame}
	if ctx.DrawCachedImage(2.5, 3, image.NewRGBA(image.Rect(0, 0, 1, 1))) {
		t.Fatal("DrawCachedImage() used pixel blit for a fractional position")
	}
	raster.Close()
}
