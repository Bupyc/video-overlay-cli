package render

import (
	"bytes"
	"image"
	"image/color"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tdewolff/canvas"
	"video-overlay-cli/internal/activity"
	"video-overlay-cli/internal/layout"
)

func TestFrameAtReturnsTransparentAndBlackRGBA(t *testing.T) {
	doc := mustParse(t, `<layout/>`)
	act := &activity.Activity{Samples: []activity.Sample{{Timestamp: time.Unix(0, 0)}}, Start: time.Unix(0, 0), End: time.Unix(0, 0)}
	for _, test := range []struct {
		background Background
		want       color.RGBA
	}{
		{Transparent, color.RGBA{}},
		{Black, color.RGBA{A: 255}},
	} {
		r, err := New(Options{Width: 2, Height: 1, Background: test.background}, doc, act, NewRegistry())
		if err != nil {
			t.Fatal(err)
		}
		frame, err := r.FrameAt(0)
		if err != nil {
			t.Fatal(err)
		}
		if got := frame.RGBAAt(0, 0); got != test.want {
			t.Errorf("background %q pixel = %#v, want %#v", test.background, got, test.want)
		}
		reused := image.NewRGBA(image.Rect(0, 0, 2, 1))
		reused.SetRGBA(0, 0, color.RGBA{R: 255, A: 255})
		if err := r.FrameAtContextInto(nil, 0, reused); err != nil {
			t.Fatal(err)
		}
		if got := reused.RGBAAt(0, 0); got != test.want {
			t.Errorf("reused frame background %q pixel = %#v, want %#v", test.background, got, test.want)
		}
	}
}

func TestYUV420PBlackFrameMatchesOpaqueRGBOnReuse(t *testing.T) {
	start := time.Unix(0, 0)
	act := &activity.Activity{Samples: []activity.Sample{{Timestamp: start}}, Start: start, End: start}
	registry := NewRegistry()
	if err := registry.Register("semi_transparent", func(*layout.Element, *Context) (Drawer, error) {
		img := image.NewRGBA(image.Rect(0, 0, 2, 2))
		img.SetRGBA(0, 0, color.RGBA{R: 96, G: 32, A: 128})
		return func(ctx *Context) error {
			ctx.Canvas.SetFillColor(color.RGBA{R: 64, G: 24, B: 40, A: 128})
			ctx.Canvas.DrawPath(1, 1, canvas.Rectangle(3, 3))
			ctx.DrawCachedImage(4, 4, img)
			return nil
		}, nil
	}); err != nil {
		t.Fatal(err)
	}
	r, err := New(Options{Width: 8, Height: 8, Background: Black}, mustParse(t, `<layout><component type="semi_transparent"/></layout>`), act, registry)
	if err != nil {
		t.Fatal(err)
	}
	want, err := r.FrameAt(0)
	if err != nil {
		t.Fatal(err)
	}
	buffer := r.NewFrameBuffer()
	for attempt := 0; attempt < 2; attempt++ {
		if err := r.FrameAtContextIntoBufferForYUV420P(nil, 0, buffer); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < len(want.Pix); i += 4 {
			if got := buffer.RGBA().Pix[i : i+3]; got[0] != want.Pix[i] || got[1] != want.Pix[i+1] || got[2] != want.Pix[i+2] {
				t.Fatalf("RGB differs at pixel %d: got %v, want %v", i/4, got, want.Pix[i:i+3])
			}
		}
		for i := range buffer.RGBA().Pix {
			buffer.RGBA().Pix[i] = 0x9a
		}
	}
}

func TestFramePreservesStraightAlphaColor(t *testing.T) {
	act := &activity.Activity{Samples: []activity.Sample{{Timestamp: time.Unix(0, 0)}}, Start: time.Unix(0, 0), End: time.Unix(0, 0)}
	r, err := New(Options{Width: 2, Height: 1, Background: Transparent}, mustParse(t, `<layout><frame width="2" height="1" bg="120,130,140,75"/></layout>`), act, NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	frame, err := r.FrameAt(0)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := frame.RGBAAt(0, 0), (color.RGBA{R: 35, G: 38, B: 41, A: 75}); got != want {
		t.Fatalf("semi-transparent frame color = %#v, want %#v", got, want)
	}
}

func TestReusableFrameBufferMatchesDirectRendering(t *testing.T) {
	start := time.Unix(0, 0)
	act := &activity.Activity{
		Samples: []activity.Sample{{Timestamp: start}, {Timestamp: start.Add(time.Second)}},
		Start:   start,
		End:     start.Add(time.Second),
	}
	registry := NewRegistry()
	if err := registry.Register("time_color", func(*layout.Element, *Context) (Drawer, error) {
		return func(ctx *Context) error {
			fill := color.RGBA{R: uint8(ctx.At / time.Second * 80), A: 255}
			ctx.Canvas.SetFillColor(fill)
			ctx.Canvas.DrawPath(0, 0, canvas.Rectangle(8, 8))
			return nil
		}, nil
	}); err != nil {
		t.Fatal(err)
	}
	r, err := New(Options{Width: 8, Height: 8, Background: Transparent}, mustParse(t, `<layout><component type="time_color"/></layout>`), act, registry)
	if err != nil {
		t.Fatal(err)
	}
	buffer := r.NewFrameBuffer()
	for _, at := range []time.Duration{time.Second, 0, time.Second} {
		want, err := r.FrameAt(at)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.FrameAtContextIntoBuffer(nil, at, buffer); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(buffer.RGBA().Pix, want.Pix) {
			t.Fatalf("reused buffer differs from direct rendering at %s", at)
		}
	}
}

func TestReusableFrameBufferDoesNotAllocateDrawContextPerFrame(t *testing.T) {
	start := time.Unix(0, 0)
	act := &activity.Activity{Samples: []activity.Sample{{Timestamp: start}}, Start: start, End: start}
	r, err := New(Options{Width: 8, Height: 8, Background: Transparent}, mustParse(t, `<layout/>`), act, NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	buffer := r.NewFrameBuffer()
	if err := r.FrameAtContextIntoBuffer(nil, 0, buffer); err != nil {
		t.Fatal(err)
	}
	var renderErr error
	allocs := testing.AllocsPerRun(100, func() {
		renderErr = r.FrameAtContextIntoBuffer(nil, 0, buffer)
	})
	if renderErr != nil {
		t.Fatal(renderErr)
	}
	if allocs != 0 {
		t.Fatalf("reused frame buffer allocated %.1f objects per frame, want 0", allocs)
	}
}

func TestReusableFrameBufferResetsCanvasStateBetweenFrames(t *testing.T) {
	start := time.Unix(0, 0)
	act := &activity.Activity{
		Samples: []activity.Sample{{Timestamp: start}, {Timestamp: start.Add(time.Second)}},
		Start:   start,
		End:     start.Add(time.Second),
	}
	registry := NewRegistry()
	if err := registry.Register("canvas_state", func(*layout.Element, *Context) (Drawer, error) {
		return func(ctx *Context) error {
			if ctx.At > 0 {
				ctx.Canvas.SetFillColor(color.RGBA{R: 255, A: 255})
				ctx.Canvas.Translate(3, 0)
				ctx.Canvas.MoveTo(0, 0)
				ctx.Canvas.LineTo(5, 5)
				return nil
			}
			ctx.Canvas.Fill()
			ctx.Canvas.DrawPath(0, 0, canvas.Rectangle(2, 2))
			return nil
		}, nil
	}); err != nil {
		t.Fatal(err)
	}
	r, err := New(Options{Width: 8, Height: 8, Background: Transparent}, mustParse(t, `<layout><component type="canvas_state"/></layout>`), act, registry)
	if err != nil {
		t.Fatal(err)
	}
	buffer := r.NewFrameBuffer()
	if err := r.FrameAtContextIntoBuffer(nil, time.Second, buffer); err != nil {
		t.Fatal(err)
	}
	if err := r.FrameAtContextIntoBuffer(nil, 0, buffer); err != nil {
		t.Fatal(err)
	}
	want, err := r.FrameAt(0)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buffer.RGBA().Pix, want.Pix) {
		t.Fatal("reused frame buffer retained Canvas state from the previous frame")
	}
}

func TestDefaultFontPrefersRobotoMediumWhenInstalled(t *testing.T) {
	roboto, ok := canvas.FindSystemFont("Roboto", canvas.FontMedium)
	if !ok {
		t.Skip("Roboto Medium is not installed")
	}
	if got := defaultFontFile(); got != roboto {
		t.Fatalf("default font = %q, want %q", got, roboto)
	}
}

func TestNewReportsUnsupportedComponentAndLocation(t *testing.T) {
	doc := mustParse(t, "<layout>\n  <component type=\"missing\"/>\n</layout>")
	act := &activity.Activity{Samples: []activity.Sample{{Timestamp: time.Unix(0, 0)}}, Start: time.Unix(0, 0), End: time.Unix(0, 0)}
	_, err := New(Options{Width: 1, Height: 1, Background: Transparent}, doc, act, NewRegistry())
	if err == nil || !strings.Contains(err.Error(), "line 2, column 3") {
		t.Fatalf("New error = %v", err)
	}
}

func TestNewWorkersSharePreparedLayoutAndTextCache(t *testing.T) {
	var prepared, builds atomic.Int32
	registry := NewRegistry()
	if err := registry.Register("probe", func(*layout.Element, *Context) (Drawer, error) {
		prepared.Add(1)
		return func(ctx *Context) error {
			bitmap, _ := ctx.CachedTextImage("shared", func() (*image.RGBA, image.Point) {
				builds.Add(1)
				time.Sleep(10 * time.Millisecond)
				img := image.NewRGBA(image.Rect(0, 0, 1, 1))
				img.SetRGBA(0, 0, color.RGBA{G: 255, A: 255})
				return img, image.Point{}
			})
			ctx.DrawCachedImage(0, 0, bitmap)
			return nil
		}, nil
	}); err != nil {
		t.Fatal(err)
	}

	start := time.Unix(100, 0)
	act := &activity.Activity{Samples: []activity.Sample{{Timestamp: start}}, Start: start, End: start}
	options := Options{Width: 1, Height: 1, Background: Transparent}
	doc := mustParse(t, `<layout><component type="probe"/></layout>`)
	if _, err := NewWorkers(options, doc, act, registry, 0); err == nil {
		t.Fatal("NewWorkers() accepted zero workers")
	}
	workers, err := NewWorkers(options, doc, act, registry, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(workers) != 4 {
		t.Fatalf("NewWorkers() returned %d workers, want 4", len(workers))
	}
	for _, worker := range workers[1:] {
		if worker != workers[0] {
			t.Fatal("workers do not share the prepared renderer")
		}
	}
	if got := prepared.Load(); got != 1 {
		t.Fatalf("layout prepared %d times, want 1", got)
	}

	var wait sync.WaitGroup
	for _, worker := range workers {
		wait.Add(1)
		go func(worker *Renderer) {
			defer wait.Done()
			frame, err := worker.FrameAt(0)
			if err != nil {
				t.Errorf("FrameAt() error = %v", err)
				return
			}
			if got, want := frame.RGBAAt(0, 0), (color.RGBA{G: 255, A: 255}); got != want {
				t.Errorf("FrameAt() pixel = %#v, want %#v", got, want)
			}
		}(worker)
	}
	wait.Wait()
	if got := builds.Load(); got != 1 {
		t.Fatalf("shared text image built %d times, want 1", got)
	}
}

func TestNewWorkersBoundSharedTextCache(t *testing.T) {
	start := time.Unix(0, 0)
	act := &activity.Activity{Samples: []activity.Sample{{Timestamp: start}}, Start: start, End: start}
	workers, err := NewWorkers(Options{Width: 1, Height: 1, Background: Transparent}, mustParse(t, `<layout/>`), act, NewRegistry(), 8)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := workers[0].textCache.maxItems, maxSharedTextCacheItems; got != want {
		t.Fatalf("shared text cache limit = %d, want %d", got, want)
	}
}

func TestFrameAtRejectsTimeOutsideActivity(t *testing.T) {
	start := time.Unix(100, 0)
	act := &activity.Activity{Samples: []activity.Sample{{Timestamp: start}}, Start: start, End: start}
	r, err := New(Options{Width: 1, Height: 1, Background: Transparent}, mustParse(t, `<layout/>`), act, NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.FrameAt(time.Nanosecond); err == nil {
		t.Fatal("FrameAt accepted time after the activity")
	}
}

func mustParse(t *testing.T, source string) *layout.Document {
	t.Helper()
	doc, err := layout.Parse(strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}
