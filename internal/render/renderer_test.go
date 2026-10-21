package render

import (
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
