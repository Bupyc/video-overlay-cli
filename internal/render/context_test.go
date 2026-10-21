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
	ctx := &Context{textCache: &textCache{items: make(map[string]*cachedTextImage)}}
	wantImage := image.NewRGBA(image.Rect(0, 0, 2, 2))
	wantOffset := image.Pt(-1, 2)
	builds := 0
	build := func() (*image.RGBA, image.Point) {
		builds++
		return wantImage, wantOffset
	}

	firstImage, firstOffset := ctx.CachedTextImage("same text", build)
	secondImage, secondOffset := ctx.CachedTextImage("same text", build)
	if builds != 1 || firstImage != wantImage || secondImage != wantImage || firstOffset != wantOffset || secondOffset != wantOffset {
		t.Fatalf("CachedTextImage() builds=%d first=(%p,%v) second=(%p,%v)", builds, firstImage, firstOffset, secondImage, secondOffset)
	}
}

func TestCachedTextImageEvictsLeastRecentlyUsed(t *testing.T) {
	ctx := &Context{textCache: &textCache{items: make(map[string]*cachedTextImage), maxItems: 2}}
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
	ctx := &Context{textCache: &textCache{items: make(map[string]*cachedTextImage)}}
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
	ctx := &Context{textCache: &textCache{items: make(map[string]*cachedTextImage)}}
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
	lastWasBuilt := ctx.textCache.items["next-6"] != nil
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
