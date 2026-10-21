package maps

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRenderFetchesTilesAndReusesCache(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("User-Agent") != "video-overlay-cli-test" {
			t.Errorf("User-Agent = %q", r.Header.Get("User-Agent"))
		}
		w.Header().Set("Cache-Control", "max-age=3600")
		img := image.NewRGBA(image.Rect(0, 0, TileSize, TileSize))
		for y := 0; y < TileSize; y++ {
			for x := 0; x < TileSize; x++ {
				img.SetRGBA(x, y, color.RGBA{R: 20, G: 90, B: 160, A: 255})
			}
		}
		if err := png.Encode(w, img); err != nil {
			t.Errorf("encode tile: %v", err)
		}
	}))
	defer server.Close()

	config := DefaultConfig()
	config.TileURL = server.URL + "/{z}/{x}/{y}.png"
	config.UserAgent = "video-overlay-cli-test"
	cacheDir := t.TempDir()
	config.Cache = NewCache(cacheDir)
	viewport, err := NewViewport(Point{}, 0, image.Pt(TileSize, TileSize))
	if err != nil {
		t.Fatal(err)
	}
	request := Request{Viewport: viewport, Config: config}
	for attempt := range 2 {
		if attempt == 1 {
			config.Cache = NewCache(cacheDir)
		}
		request.Config = config
		img, err := Render(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		if got := img.RGBAAt(TileSize/2, TileSize/2); got != (color.RGBA{R: 20, G: 90, B: 160, A: 255}) {
			t.Fatalf("rendered center pixel = %v", got)
		}
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("tile requests = %d, want one request served from disk cache", got)
	}
}

func TestRenderIntoReusesAndClearsDestination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		tile := image.NewRGBA(image.Rect(0, 0, TileSize, TileSize))
		for i := 0; i < len(tile.Pix); i += 4 {
			copy(tile.Pix[i:i+4], []byte{20, 90, 160, 255})
		}
		if err := png.Encode(w, tile); err != nil {
			t.Errorf("encode tile: %v", err)
		}
	}))
	defer server.Close()

	config := DefaultConfig()
	config.TileURL = server.URL + "/{z}/{x}/{y}.png"
	config.Cache = NewCache("")
	viewport, err := NewViewport(Point{}, 0, image.Pt(TileSize, TileSize))
	if err != nil {
		t.Fatal(err)
	}
	request := Request{Viewport: viewport, Config: config}
	want, err := Render(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	got := image.NewRGBA(want.Bounds())
	for range 2 {
		for i := range got.Pix {
			got.Pix[i] = 77
		}
		if err := RenderInto(context.Background(), request, got); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got.Pix, want.Pix) {
			t.Fatal("RenderInto output differs from Render")
		}
	}
}

func TestCacheReturnsOnlyUnexpiredMemoryTiles(t *testing.T) {
	cache := NewCache("")
	fresh := image.NewRGBA(image.Rect(0, 0, TileSize, TileSize))
	cache.items["fresh"] = cacheEntry{img: fresh, expires: time.Now().Add(time.Hour)}
	cache.items["expired"] = cacheEntry{img: fresh, expires: time.Now().Add(-time.Second)}

	if got, ok := cache.cached("fresh"); !ok || got != fresh {
		t.Fatalf("cached fresh tile = (%v, %t), want (%p, true)", got, ok, fresh)
	}
	if got, ok := cache.cached("expired"); ok || got != nil {
		t.Fatalf("cached expired tile = (%v, %t), want (nil, false)", got, ok)
	}
}

func TestRenderDoesNotCacheNoStoreTiles(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Cache-Control", "no-store")
		tile := image.NewRGBA(image.Rect(0, 0, TileSize, TileSize))
		if err := png.Encode(w, tile); err != nil {
			t.Errorf("encode tile: %v", err)
		}
	}))
	defer server.Close()
	config := DefaultConfig()
	config.TileURL = server.URL + "/{z}/{x}/{y}.png"
	config.Cache = NewCache(t.TempDir())
	viewport, err := NewViewport(Point{}, 0, image.Pt(TileSize, TileSize))
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := Render(context.Background(), Request{Viewport: viewport, Config: config}); err != nil {
			t.Fatal(err)
		}
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("no-store tile requests = %d, want 2", got)
	}
}

func TestRenderReturnsProviderErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "tile service unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	config := DefaultConfig()
	config.TileURL = server.URL + "/{z}/{x}/{y}.png"
	config.Cache = NewCache(t.TempDir())
	viewport, err := NewViewport(Point{}, 0, image.Pt(TileSize, TileSize))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Render(context.Background(), Request{Viewport: viewport, Config: config}); err == nil {
		t.Fatal("Render swallowed the tile provider error")
	}
}

func TestRenderHonorsCancellation(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	config := DefaultConfig()
	config.TileURL = server.URL + "/{z}/{x}/{y}.png"
	config.Cache = NewCache("")
	viewport, err := NewViewport(Point{}, 0, image.Pt(TileSize, TileSize))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := Render(ctx, Request{Viewport: viewport, Config: config})
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; err == nil {
		t.Fatal("Render ignored canceled context")
	}
}
