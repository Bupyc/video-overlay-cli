package widgets

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/rasterizer"
	"video-overlay-cli/internal/activity"
	"video-overlay-cli/internal/layout"
	"video-overlay-cli/internal/maps"
	"video-overlay-cli/internal/render"
)

func TestPrefetchFutureLabelsSkipsRepeatsAndCapsLookahead(t *testing.T) {
	start := time.Unix(100, 0)
	values := []float64{0, 0, 1, 1, 2, 3, 4, 5, 6}
	samples := make([]activity.Sample, len(values))
	for i, value := range values {
		samples[i] = activity.Sample{
			Timestamp: start.Add(time.Duration(i) * 100 * time.Millisecond),
			Speed:     activity.Value{Number: value, Valid: true},
		}
	}
	ctx := &render.Context{Activity: &activity.Activity{Samples: samples}, Sample: samples[0]}
	var attempted, got []string
	prefetchFutureLabels(ctx, "0", func(sample activity.Sample) string {
		return fmt.Sprintf("%.0f", sample.Speed.Number)
	}, func(text string) bool {
		attempted = append(attempted, text)
		if text == "1" {
			return false
		}
		got = append(got, text)
		return true
	}, make(chan struct{}))

	if want := []string{"1", "2"}; !reflect.DeepEqual(attempted, want) {
		t.Fatalf("attempted labels = %v, want %v", attempted, want)
	}
	if want := []string{"2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("prefetched labels = %v, want %v", got, want)
	}

	attempted = nil
	prefetchFutureLabels(ctx, "0", func(sample activity.Sample) string {
		return fmt.Sprintf("%.0f", sample.Speed.Number)
	}, func(text string) bool {
		attempted = append(attempted, text)
		return false
	}, make(chan struct{}))
	if want := []string{"1", "2", "3", "4", "5"}; !reflect.DeepEqual(attempted, want) {
		t.Fatalf("lookahead attempts = %v, want at most five distinct labels", attempted)
	}
}

func TestRegisterDefaultsAndRenderCoreWidgets(t *testing.T) {
	registry := render.NewRegistry()
	RegisterDefaults(&registry)
	core := []string{
		"asi", "bar", "chart", "gradient_chart", "circuit_map", "compass", "compass_arrow",
		"datetime", "gps_lock_icon", "icon", "metric", "metric_unit", "msi", "msi2", "text", "zone_bar",
	}
	for _, name := range core {
		if registry.Status(name) != "supported" {
			t.Errorf("%s status = %q", name, registry.Status(name))
		}
	}
	for _, name := range []string{"moving_map", "journey_map", "moving_journey_map"} {
		if registry.Status(name) != "supported" {
			t.Errorf("%s status = %q, want supported", name, registry.Status(name))
		}
	}
	for _, name := range []string{"cairo_circuit_map", "cairo_gauge_marker", "cairo_gauge_round_annotated", "cairo_gauge_arc_annotated", "cairo_gauge_donut"} {
		if registry.Status(name) != "supported" {
			t.Errorf("%s status = %q, want supported", name, registry.Status(name))
		}
	}

	var source strings.Builder
	source.WriteString(`<layout><composite x="20" y="20">`)
	for i, component := range core {
		x, y := (i%4)*190, (i/4)*160
		attrs := map[string]string{
			"asi":            `metric="speed" units="knots" size="100"`,
			"bar":            `metric="power" width="150" height="26"`,
			"chart":          `metric="alt" width="150" height="70" seconds="60"`,
			"gradient_chart": `metric="gradient" width="150" height="70" seconds="60"`,
			"circuit_map":    `size="90"`,
			"compass":        `size="90"`,
			"compass_arrow":  `size="90"`,
			"datetime":       `format="%H:%M:%S" size="12"`,
			"icon":           `file="mountain.png" size="42"`,
			"metric":         `metric="power" format=".0f" size="20"`,
			"metric_unit":    `metric="power" units="W" size="14"`,
			"msi":            `metric="speed" units="knots" size="100"`,
			"msi2":           `metric="speed" units="knots" size="100"`,
			"text":           `size="20"`,
			"zone_bar":       `metric="power" width="150" height="26"`,
		}
		text := ""
		if component == "text" {
			text = "TRAIL"
		}
		if component == "metric_unit" {
			text = "{}"
		}
		if component == "gps_lock_icon" {
			attrs[component] = `size="42"`
		}
		fmt.Fprintf(&source, `<component type=%q x=%q y=%q %s>%s</component>`, component, fmt.Sprint(x), fmt.Sprint(y), attrs[component], text)
	}
	source.WriteString(`</composite></layout>`)
	doc, err := layout.Parse(strings.NewReader(source.String()))
	if err != nil {
		t.Fatal(err)
	}
	activity := widgetActivity()
	r, err := render.New(render.Options{Width: 800, Height: 700, Background: render.Transparent}, doc, activity, registry)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := r.FrameAt(0)
	if err != nil {
		t.Fatal(err)
	}
	if got := countVisible(frame); got < 1000 {
		t.Fatalf("gallery drew only %d nontransparent pixels", got)
	}
}

func TestCachedTextMatchesDirectCanvasText(t *testing.T) {
	for _, test := range []struct {
		text  string
		align string
		x, y  float64
	}{
		{"TRAIL", "left", 15, 20},
		{"pace 5:32", "center", 80, 26},
		{"55.59891656", "left", 15, 20},
		{"12:34:56.123456", "left", 15, 20},
		{"м", "left", 20.5, 30.5},
		{"—", "right", 145.5, 20.5},
	} {
		fill := color.RGBA{R: 245, G: 252, B: 255, A: 248}
		outline := color.RGBA{R: 0, G: 35, B: 70, A: 210}
		draw := func(cache bool) *image.RGBA {
			return drawStandaloneFrame(t, standaloneActivity(), 0, image.Pt(200, 70), func(ctx *render.Context) error {
				drawTextWithCache(ctx, test.x, test.y, 20, test.text, test.align, fill, outline, 2, cache)
				return nil
			})
		}
		direct := drawStandaloneFrame(t, standaloneActivity(), 0, image.Pt(200, 70), func(ctx *render.Context) error {
			face := ctx.Fonts.Face(20*72/25.4, nrgba(fill), canvas.FontStroke(2, nrgba(outline)))
			ctx.Canvas.DrawText(test.x, test.y, canvas.NewTextLine(face, test.text, canvasTextAlign(test.align)))
			return nil
		})
		if diff := maxPixelChannelDiff(draw(true), direct); diff > 80 {
			t.Errorf("cached text %q (%s) differs from direct canvas text by %d channel levels", test.text, test.align, diff)
		}
		if diff := maxPixelChannelDiff(draw(false), direct); diff > 80 {
			t.Errorf("uncached text %q (%s) differs from direct canvas text by %d channel levels", test.text, test.align, diff)
		}
	}
}

func TestTextTopToBottomUsesCompactLineSpacing(t *testing.T) {
	fonts := canvas.NewFontFamily("test")
	if err := fonts.LoadSystemFont("Roboto", canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	text := "Vertical Message"
	element := &layout.Element{Text: text, Attributes: []layout.Attribute{
		{Name: "size", Value: "32"},
		{Name: "direction", Value: "ttb"},
		{Name: "outline_width", Value: "0"},
	}}
	drawer, err := textFactory(element, &render.Context{Fonts: fonts})
	if err != nil {
		t.Fatal(err)
	}
	frame := drawStandaloneFrame(t, standaloneActivity(), 0, image.Pt(100, 700), drawer)

	bounds := image.Rectangle{}
	for y := 0; y < frame.Bounds().Dy(); y++ {
		for x := 0; x < frame.Bounds().Dx(); x++ {
			if frame.RGBAAt(x, y).A > 0 {
				bounds = bounds.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	if bounds.Empty() || bounds.Dy() <= bounds.Dx() || bounds.Dy() < 450 || bounds.Dy() > 540 {
		t.Fatalf("ttb text bounds = %v, want visible text taller than it is wide and 450-540px high", bounds)
	}
}

func TestUpstreamExampleLayoutPrepares(t *testing.T) {
	source, err := os.ReadFile("../../../examples/reference-layouts/example.xml")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := layout.Parse(bytes.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	registry := render.NewRegistry()
	RegisterDefaults(&registry)
	if _, err := render.New(render.Options{Width: 1920, Height: 1080, Background: render.Transparent, AssetDir: "../../../examples/reference-layouts"}, doc, widgetActivity(), registry); err != nil {
		t.Fatal(err)
	}
}

func TestRasterizedGlyphMatchesDirectCanvasText(t *testing.T) {
	fonts := canvas.NewFontFamily("test")
	if err := fonts.LoadSystemFont("Roboto", canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	fill := color.RGBA{R: 245, G: 252, B: 255, A: 248}
	outline := color.RGBA{R: 0, G: 35, B: 70, A: 210}
	face := fonts.Face(20*72/25.4, nrgba(fill), canvas.FontStroke(2, nrgba(outline)))
	glyphs := face.Glyphs("A")
	if len(glyphs) != 1 {
		t.Fatalf("shaped glyph count = %d, want 1", len(glyphs))
	}
	got, gotOffset := rasterizeGlyphBitmap(face, glyphs[0].ID, 0, 0, fill, outline, 2)
	want, wantOffset := rasterizeTextCanvas(canvas.NewTextLine(face, "A", canvas.Left), 0, 0, 2)

	const origin = 40
	gotFrame, wantFrame := image.NewRGBA(image.Rect(0, 0, 100, 100)), image.NewRGBA(image.Rect(0, 0, 100, 100))
	draw.Draw(gotFrame, got.Bounds().Add(image.Pt(origin+gotOffset.X, origin+gotOffset.Y)), got, got.Bounds().Min, draw.Over)
	draw.Draw(wantFrame, want.Bounds().Add(image.Pt(origin+wantOffset.X, origin+wantOffset.Y)), want, want.Bounds().Min, draw.Over)
	if diff := maxPixelChannelDiff(gotFrame, wantFrame); diff > 4 {
		t.Fatalf("rasterized glyph differs from direct Canvas text by %d channel levels", diff)
	}
}

func TestRasterizedGlyphLineMatchesDirectCanvasText(t *testing.T) {
	fonts := canvas.NewFontFamily("test")
	if err := fonts.LoadSystemFont("Roboto", canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	fill := color.RGBA{R: 245, G: 252, B: 255, A: 248}
	outline := color.RGBA{R: 0, G: 35, B: 70, A: 210}
	face := fonts.Face(20*72/25.4, nrgba(fill), canvas.FontStroke(2, nrgba(outline)))
	line := canvas.NewTextLine(face, "—", canvas.Right)
	ctx := &render.Context{}
	got, gotOffset, ok := rasterizeTextGlyphs(ctx, line, .5, .5, fill, outline, 2)
	want, wantOffset := rasterizeTextCanvas(line, .5, .5, 2)
	if !ok {
		t.Fatal("glyph rasterizer rejected an unrotated, single-glyph line")
	}
	gotFrame, wantFrame := image.NewRGBA(image.Rect(0, 0, 100, 100)), image.NewRGBA(image.Rect(0, 0, 100, 100))
	draw.Draw(gotFrame, got.Bounds().Add(image.Pt(40+gotOffset.X, 40+gotOffset.Y)), got, got.Bounds().Min, draw.Over)
	draw.Draw(wantFrame, want.Bounds().Add(image.Pt(40+wantOffset.X, 40+wantOffset.Y)), want, want.Bounds().Min, draw.Over)
	if diff := maxPixelChannelDiff(gotFrame, wantFrame); diff > 4 {
		t.Fatalf("rasterized glyph line differs by %d channel levels (glyph offset=%v size=%v, canvas offset=%v size=%v)", diff, gotOffset, got.Bounds().Size(), wantOffset, want.Bounds().Size())
	}
}

func TestOutlinedDynamicTextUsesBitmapGlyphs(t *testing.T) {
	fonts := canvas.NewFontFamily("test")
	if err := fonts.LoadSystemFont("Roboto", canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	fill := color.RGBA{R: 245, G: 252, B: 255, A: 248}
	outline := color.RGBA{R: 0, G: 35, B: 70, A: 210}
	for _, test := range []struct {
		text string
		size float64
	}{{"55.59891656", 16}, {"12:34:56.123456", 32}} {
		face := fonts.Face(test.size*72/25.4, nrgba(fill), canvas.FontStroke(2, nrgba(outline)))
		line := canvas.NewTextLine(face, test.text, canvas.Left)
		got, gotOffset, ok := rasterizeTextGlyphs(&render.Context{}, line, 0.25, 0.5, fill, outline, 2)
		if !ok {
			t.Fatalf("glyph rasterizer rejected outlined text %q", test.text)
		}
		want, wantOffset := rasterizeTextCanvas(line, 0.25, 0.5, 2)
		gotFrame, wantFrame := image.NewRGBA(image.Rect(0, 0, 500, 100)), image.NewRGBA(image.Rect(0, 0, 500, 100))
		draw.Draw(gotFrame, got.Bounds().Add(image.Pt(40+gotOffset.X, 40+gotOffset.Y)), got, got.Bounds().Min, draw.Over)
		draw.Draw(wantFrame, want.Bounds().Add(image.Pt(40+wantOffset.X, 40+wantOffset.Y)), want, want.Bounds().Min, draw.Over)
		if diff := maxPixelChannelDiff(gotFrame, wantFrame); diff > 80 {
			t.Errorf("outlined text %q differs from Canvas by %d channel levels", test.text, diff)
		}
	}
}

func TestColorAttrPreservesStraightAlphaWhenDrawing(t *testing.T) {
	element := &layout.Element{Attributes: []layout.Attribute{{Name: "fill", Value: "120,130,140,75"}}}
	fill, err := colorAttr(element, "fill", color.RGBA{})
	if err != nil {
		t.Fatal(err)
	}
	frame := drawStandaloneFrame(t, standaloneActivity(), 0, image.Pt(1, 1), func(ctx *render.Context) error {
		drawShape(ctx, canvas.Rectangle(1, 1), fill, color.RGBA{}, 0)
		return nil
	})
	if got, want := frame.RGBAAt(0, 0), (color.RGBA{R: 35, G: 38, B: 41, A: 75}); got != want {
		t.Fatalf("semi-transparent XML fill pixel = %#v, want %#v", got, want)
	}
}

func TestTimeChartWindowIsCenteredAndInterpolated(t *testing.T) {
	start := time.Unix(0, 0).UTC()
	activity := &activity.Activity{Samples: []activity.Sample{
		{Timestamp: start, Altitude: activity.Value{Number: 0, Valid: true}},
		{Timestamp: start.Add(10 * time.Second), Altitude: activity.Value{Number: 10, Valid: true}},
		{Timestamp: start.Add(20 * time.Second), Altitude: activity.Value{Number: 20, Valid: true}},
	}}
	values, marker := timeChartValues(activity, start.Add(10*time.Second), 20*time.Second, 4, "alt", "m")
	for i, want := range []float64{0, 5, 10, 15} {
		if !values[i].Valid || values[i].Number != want {
			t.Errorf("chart value %d = %#v, want %v", i, values[i], want)
		}
	}
	if marker != 2 {
		t.Errorf("marker index = %v, want 2", marker)
	}
}

func TestDistanceChartRequiresRecordedDistance(t *testing.T) {
	a := &activity.Activity{Samples: []activity.Sample{{CumulativeDistance: activity.Value{Number: 10, Valid: true}}}}
	if useDistanceWindow(a, 100) {
		t.Fatal("calculated cumulative distance selected the distance window")
	}
	a.Samples[0].Distance = activity.Value{Number: 10, Valid: true}
	if !useDistanceWindow(a, 100) {
		t.Fatal("recorded activity distance did not select the distance window")
	}
}

func maxPixelChannelDiff(a, b *image.RGBA) uint8 {
	var maxDiff uint8
	for i := range a.Pix {
		if diff := uint8(math.Abs(float64(a.Pix[i]) - float64(b.Pix[i]))); diff > maxDiff {
			maxDiff = diff
		}
	}
	return maxDiff
}

func TestCairoWidgetGalleryRendersAllVectorWidgets(t *testing.T) {
	source, err := os.ReadFile("../../../examples/widget-gallery.xml")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := layout.Parse(bytes.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	registry := render.NewRegistry()
	RegisterDefaults(&registry)
	r, err := render.New(render.Options{Width: 1920, Height: 1080, Background: render.Transparent}, doc, widgetActivity(), registry)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := r.FrameAt(20 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got := countVisible(frame); got < 20000 {
		t.Fatalf("gauge gallery drew only %d visible pixels", got)
	}
}

func TestCairoMarkerRejectsUnknownCapDuringPrepare(t *testing.T) {
	doc, err := layout.Parse(strings.NewReader(`<layout><component type="cairo_gauge_marker" cap="diagonal"/></layout>`))
	if err != nil {
		t.Fatal(err)
	}
	registry := render.NewRegistry()
	RegisterDefaults(&registry)
	if _, err := render.New(render.Options{Width: 128, Height: 128, Background: render.Transparent}, doc, widgetActivity(), registry); err == nil || !strings.Contains(err.Error(), `invalid Cairo gauge cap "diagonal"`) {
		t.Fatalf("render.New() error = %v, want invalid cap", err)
	}
}

func TestNewSparkLineTracksCurrentCadenceSample(t *testing.T) {
	a := standaloneActivity()
	drawer := NewSparkLine(a, image.Pt(10, 5), image.Pt(100, 25))
	first := drawStandaloneFrame(t, a, 0, image.Pt(140, 40), drawer)
	second := drawStandaloneFrame(t, a, 1500*time.Millisecond, image.Pt(140, 40), drawer)
	if countVisible(first) == 0 || bytes.Equal(first.Pix, second.Pix) {
		t.Fatal("sparkline should draw a chart and move its marker with time")
	}
}

func TestKineticEnergyLabelUsesSpeedInMetersPerSecond(t *testing.T) {
	valid := activity.Value{Number: 10, Valid: true}
	if got := kineticEnergyLabel(valid, 84); got != "4.2 kJ" {
		t.Fatalf("kineticEnergyLabel(10 m/s, 84 kg) = %q, want %q", got, "4.2 kJ")
	}
	if got := kineticEnergyLabel(activity.Value{Number: 200, Valid: true}, 84); got != "1,680 kJ" {
		t.Fatalf("large energy label = %q, want %q", got, "1,680 kJ")
	}
	if got := kineticEnergyLabel(activity.Value{}, 84); got != "-" {
		t.Fatalf("invalid speed label = %q, want dash", got)
	}
}

func TestEnergyMassAndMultiplierLabelsGroupDigits(t *testing.T) {
	if got := energyMassLabel(2084); got != "2,084 kg" {
		t.Fatalf("energyMassLabel(2084) = %q", got)
	}
	if got := energyMultiplier(96, 84); got != "1.1x" {
		t.Fatalf("energyMultiplier(96, 84) = %q", got)
	}
}

func TestNewComparativeEnergyChangesWhenSpeedChanges(t *testing.T) {
	a := standaloneActivity()
	speed := activity.Value{Number: 10, Valid: true}
	drawer := NewComparativeEnergy(func() activity.Value { return speed }, 84, 12, 2000, 3500)
	first := drawStandaloneFrame(t, a, 0, image.Pt(1200, 100), drawer)
	speed.Number = 20
	second := drawStandaloneFrame(t, a, 0, image.Pt(1200, 100), drawer)
	if countVisible(first) < 100 || bytes.Equal(first.Pix, second.Pix) {
		t.Fatal("comparative energy should draw its cards and update with speed")
	}
}

func TestStandaloneWidgetGalleryRendersBothConstructors(t *testing.T) {
	a := standaloneActivity()
	spark := NewSparkLine(a, image.Pt(10, 10), image.Pt(220, 40))
	energy := NewComparativeEnergy(func() activity.Value { return activity.Value{Number: 10, Valid: true} }, 84, 12, 2000, 3500)
	gallery := func(ctx *render.Context) error {
		if err := spark(ctx); err != nil {
			return err
		}
		ctx.Canvas.Push()
		defer ctx.Canvas.Pop()
		ctx.Canvas.Translate(250, 5)
		return energy(ctx)
	}
	frame := drawStandaloneFrame(t, a, time.Second, image.Pt(1500, 120), gallery)
	if countVisible(frame) < 1000 {
		t.Fatalf("standalone widget gallery drew only %d pixels", countVisible(frame))
	}
}

func standaloneActivity() *activity.Activity {
	start := time.Unix(0, 0).UTC()
	return &activity.Activity{
		Start: start,
		End:   start.Add(2 * time.Second),
		Samples: []activity.Sample{
			{Timestamp: start, Cadence: activity.Value{Number: 80, Valid: true}},
			{Timestamp: start.Add(time.Second), Cadence: activity.Value{Number: 100, Valid: true}},
			{Timestamp: start.Add(2 * time.Second), Cadence: activity.Value{Number: 70, Valid: true}},
		},
	}
}

func drawStandaloneFrame(t *testing.T, a *activity.Activity, at time.Duration, size image.Point, drawer render.Drawer) *image.RGBA {
	t.Helper()
	fonts := canvas.NewFontFamily("test")
	if err := fonts.LoadSystemFont("Roboto", canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	frame := image.NewRGBA(image.Rect(0, 0, size.X, size.Y))
	raster := rasterizer.FromImage(frame, canvas.DPMM(1), nil)
	ctx := &render.Context{At: at, Activity: a, Sample: a.Samples[0], Size: size, Fonts: fonts, Canvas: canvas.NewContext(raster)}
	ctx.Canvas.SetCoordSystem(canvas.CartesianIV)
	if err := drawer(ctx); err != nil {
		raster.Close()
		t.Fatal(err)
	}
	raster.Close()
	return frame
}

func TestTileMapWidgetsRenderFromConfiguredProvider(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Cache-Control", "max-age=3600")
		tile := image.NewRGBA(image.Rect(0, 0, maps.TileSize, maps.TileSize))
		for y := 0; y < maps.TileSize; y++ {
			for x := 0; x < maps.TileSize; x++ {
				tile.SetRGBA(x, y, color.RGBA{R: 60, G: 120, B: 170, A: 255})
			}
		}
		if err := png.Encode(w, tile); err != nil {
			t.Errorf("encode tile: %v", err)
		}
	}))
	defer server.Close()
	config := maps.DefaultConfig()
	config.TileURL = server.URL + "/{z}/{x}/{y}.png"
	config.Attribution = "test tiles"
	config.Cache = maps.NewCache(t.TempDir())
	gallery, err := os.ReadFile("../../../examples/map-gallery.xml")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := layout.Parse(bytes.NewReader(gallery))
	if err != nil {
		t.Fatal(err)
	}
	registry := render.NewRegistry()
	RegisterDefaults(&registry)
	r, err := render.New(render.Options{Width: 864, Height: 304, Background: render.Transparent, MapConfig: config}, doc, widgetActivity(), registry)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := r.FrameAt(0)
	if err != nil {
		t.Fatal(err)
	}
	if got := countVisible(frame); got < 12000 {
		t.Fatalf("tile map widgets drew only %d visible pixels", got)
	}
	firstRequestCount := requests.Load()
	if firstRequestCount == 0 {
		t.Fatal("map widgets did not request their configured tile provider")
	}
	if _, err := r.FrameAt(0); err != nil {
		t.Fatal(err)
	}
	if got := requests.Load(); got != firstRequestCount {
		t.Fatalf("second gallery frame made %d tile requests; warm cache should make none", got-firstRequestCount)
	}
}

func TestMapWidgetsCanHideAttributionInTemplate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		tile := image.NewRGBA(image.Rect(0, 0, maps.TileSize, maps.TileSize))
		for y := 0; y < maps.TileSize; y++ {
			for x := 0; x < maps.TileSize; x++ {
				tile.SetRGBA(x, y, color.RGBA{R: 60, G: 120, B: 170, A: 255})
			}
		}
		if err := png.Encode(w, tile); err != nil {
			t.Errorf("encode tile: %v", err)
		}
	}))
	defer server.Close()
	config := maps.DefaultConfig()
	config.TileURL = server.URL + "/{z}/{x}/{y}.png"
	config.Attribution = "test tiles"
	config.Cache = maps.NewCache("")

	components := make([]string, 0, 9)
	for _, widget := range []string{"moving_map", "journey_map", "moving_journey_map"} {
		for _, setting := range []string{"", ` show-attribution="true"`, ` show-attribution="false"`} {
			x := len(components) * 80
			components = append(components, fmt.Sprintf(`<component type="%s" x="%d" size="80"%s/>`, widget, x, setting))
		}
	}
	doc, err := layout.Parse(strings.NewReader(`<layout>` + strings.Join(components, "") + `</layout>`))
	if err != nil {
		t.Fatal(err)
	}
	registry := render.NewRegistry()
	RegisterDefaults(&registry)
	r, err := render.New(render.Options{Width: 720, Height: 80, Background: render.Transparent, MapConfig: config}, doc, widgetActivity(), registry)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := r.FrameAt(0)
	if err != nil {
		t.Fatal(err)
	}
	regionsEqual := func(leftX, rightX int) bool {
		for y := 0; y < 80; y++ {
			for x := 0; x < 80; x++ {
				if frame.RGBAAt(leftX+x, y) != frame.RGBAAt(rightX+x, y) {
					return false
				}
			}
		}
		return true
	}
	for i, widget := range []string{"moving_map", "journey_map", "moving_journey_map"} {
		defaultX := i * 240
		if !regionsEqual(defaultX, defaultX+80) {
			t.Errorf("%s with omitted show-attribution differs from explicit true", widget)
		}
		if regionsEqual(defaultX+80, defaultX+160) {
			t.Errorf("%s with show-attribution=false still rendered attribution", widget)
		}
	}
}

func TestDrawMapImageMatchesCanvasAtPixelAlignedAndFractionalPositions(t *testing.T) {
	sprite := image.NewRGBA(image.Rect(0, 0, 3, 2))
	sprite.SetRGBA(0, 0, color.RGBA{R: 220, G: 30, B: 70, A: 255})
	sprite.SetRGBA(1, 0, color.RGBA{R: 60, G: 80, B: 40, A: 120})
	sprite.SetRGBA(2, 0, color.RGBA{B: 40, A: 80})
	sprite.SetRGBA(0, 1, color.RGBA{R: 60, G: 70, B: 80, A: 100})

	for _, point := range []struct{ x, y float64 }{{2, 3}, {2.5, 3.25}} {
		renderFrame := func(fast bool) *image.RGBA {
			registry := render.NewRegistry()
			if err := registry.Register("image_probe", func(*layout.Element, *render.Context) (render.Drawer, error) {
				return func(ctx *render.Context) error {
					if fast {
						drawMapImage(ctx, sprite)
					} else {
						ctx.Canvas.DrawImage(0, 0, sprite, canvas.DPMM(1))
					}
					return nil
				}, nil
			}); err != nil {
				t.Fatal(err)
			}
			doc, err := layout.Parse(strings.NewReader(fmt.Sprintf(`<layout><component type="image_probe" x="%g" y="%g"/></layout>`, point.x, point.y)))
			if err != nil {
				t.Fatal(err)
			}
			r, err := render.New(render.Options{Width: 8, Height: 8, Background: render.Transparent}, doc, widgetActivity(), registry)
			if err != nil {
				t.Fatal(err)
			}
			frame, err := r.FrameAt(0)
			if err != nil {
				t.Fatal(err)
			}
			return frame
		}
		if got, want := renderFrame(true), renderFrame(false); !bytes.Equal(got.Pix, want.Pix) {
			for i := range got.Pix {
				if got.Pix[i] != want.Pix[i] {
					t.Fatalf("map image at (%g, %g) differs at byte %d: got %d, want %d", point.x, point.y, i, got.Pix[i], want.Pix[i])
				}
			}
		}
	}
}

func TestScaleIconMatchesPerFrameCanvasScaling(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			source.SetRGBA(x, y, color.RGBA{R: uint8(x * 20), G: uint8(y * 20), B: 80, A: 255})
		}
	}
	got := scaleIcon(source, 4)
	want := image.NewRGBA(image.Rect(0, 0, 4, 4))
	raster := rasterizer.FromImage(want, canvas.DPMM(1), nil)
	context := canvas.NewContext(raster)
	context.SetCoordSystem(canvas.CartesianIV)
	context.Scale(0.5, 0.5)
	context.DrawImage(0, 0, source, canvas.DPMM(1))
	raster.Close()
	if !bytes.Equal(got.Pix, want.Pix) {
		t.Fatal("pre-scaled icon differs from Canvas scaling")
	}
}

func TestCircuitMapCachesStaticRouteRaster(t *testing.T) {
	act := widgetActivity()
	element := &layout.Element{Attributes: []layout.Attribute{{Name: "size", Value: "80"}}}
	drawer, err := circuitMapFactory(element, &render.Context{Activity: act})
	if err != nil {
		t.Fatal(err)
	}
	var paths, images int
	for i := 0; i < 2; i++ {
		target := image.NewRGBA(image.Rect(0, 0, 80, 80))
		raster := rasterizer.FromImage(target, canvas.DPMM(1), nil)
		counter := &renderCallCounter{Renderer: raster}
		ctx := &render.Context{Activity: act, Sample: act.Samples[i], Canvas: canvas.NewContext(counter)}
		ctx.Canvas.SetCoordSystem(canvas.CartesianIV)
		if err := drawer(ctx); err != nil {
			t.Fatal(err)
		}
		raster.Close()
		paths += counter.paths
		images += counter.images
	}
	if paths != 2 || images != 2 {
		t.Fatalf("two frames rendered %d vector paths and %d images; want one dynamic marker and one cached route image per frame", paths, images)
	}
}

type renderCallCounter struct {
	canvas.Renderer
	paths, images int
}

func (r *renderCallCounter) RenderPath(path *canvas.Path, style canvas.Style, matrix canvas.Matrix) {
	r.paths++
	r.Renderer.RenderPath(path, style, matrix)
}

func (r *renderCallCounter) RenderImage(img image.Image, matrix canvas.Matrix) {
	r.images++
	r.Renderer.RenderImage(img, matrix)
}

func TestPrecomputedMapMaskPreservesRoundedOpacity(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 5, 5))
	for y := 0; y < 5; y++ {
		for x := 0; x < 5; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 80, G: 40, B: 20, A: uint8((y*5 + x + 1) * 10)})
		}
	}
	applyPrecomputedMapMask(img, newMapMask(5, 5, 2, 0.5))
	want := [][]uint8{
		{0, 10, 15, 20, 0},
		{30, 35, 40, 45, 50},
		{55, 60, 65, 70, 75},
		{80, 85, 90, 95, 100},
		{0, 110, 115, 120, 0},
	}
	for y, row := range want {
		for x, alpha := range row {
			if got := img.RGBAAt(x, y).A; got != alpha {
				t.Errorf("alpha at (%d, %d) = %d, want %d", x, y, got, alpha)
			}
		}
	}
}

func TestPrecomputedMapMaskScalesPremultipliedColor(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.SetRGBA(0, 0, color.RGBA{R: 80, G: 60, B: 40, A: 160})
	applyPrecomputedMapMask(img, newMapMask(1, 1, 0, 0.5))
	if got, want := img.RGBAAt(0, 0), (color.RGBA{R: 40, G: 30, B: 20, A: 80}); got != want {
		t.Fatalf("masked premultiplied color = %v, want %v", got, want)
	}
}

func TestPrecomputedMapBorderMatchesCanvasRasterization(t *testing.T) {
	base := image.NewRGBA(image.Rect(0, 0, 20, 14))
	for y := 0; y < base.Bounds().Dy(); y++ {
		for x := 0; x < base.Bounds().Dx(); x++ {
			base.SetRGBA(x, y, color.RGBA{R: 30, G: 40, B: 50, A: 128})
		}
	}
	want := cloneRGBA(base)
	raster := rasterizer.FromImage(want, canvas.DPMM(1), nil)
	canvasContext := canvas.NewContext(raster)
	canvasContext.SetCoordSystem(canvas.CartesianIV)
	canvasContext.SetFillColor(color.RGBA{})
	canvasContext.SetStrokeColor(color.RGBA{A: 255})
	canvasContext.SetStrokeWidth(1)
	canvasContext.DrawPath(0, 0, canvas.RoundedRectangle(20, 14, 4))
	raster.Close()

	got := cloneRGBA(base)
	applyMapBorder(got, newMapBorder(20, 14, 4))
	if diff := maxPixelChannelDiff(got, want); diff > 1 {
		t.Fatalf("precomputed border differs from canvas rasterization by %d channel levels", diff)
	}
}

func TestCropCenterUsesSourceBounds(t *testing.T) {
	source := image.NewRGBA(image.Rect(5, 7, 9, 11))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			source.SetRGBA(5+x, 7+y, color.RGBA{R: uint8(x + 1), G: uint8(y + 1), B: uint8(x + y), A: 255})
		}
	}
	got := cropCenter(source, 2)
	want := []color.RGBA{
		{R: 2, G: 2, B: 2, A: 255}, {R: 3, G: 2, B: 3, A: 255},
		{R: 2, G: 3, B: 3, A: 255}, {R: 3, G: 3, B: 4, A: 255},
	}
	for i, pixel := range want {
		if actual := got.RGBAAt(i%2, i/2); actual != pixel {
			t.Errorf("cropped pixel %d = %#v, want %#v", i, actual, pixel)
		}
	}
}

func TestRotateImagePreservesCenterForSourceBounds(t *testing.T) {
	source := image.NewRGBA(image.Rect(5, 7, 8, 10))
	source.SetRGBA(6, 8, color.RGBA{R: 30, G: 40, B: 50, A: 255})
	got := rotateImage(source, 30)
	if got.Bounds() != source.Bounds() {
		t.Fatalf("rotated bounds = %v, want %v", got.Bounds(), source.Bounds())
	}
	if actual, want := got.RGBAAt(6, 8), (color.RGBA{R: 30, G: 40, B: 50, A: 255}); actual != want {
		t.Fatalf("rotated center = %#v, want %#v", actual, want)
	}
}

func TestRotateImageIntoMatchesPreviousSampling(t *testing.T) {
	source := image.NewRGBA(image.Rect(5, 7, 38, 31))
	for y := source.Bounds().Min.Y; y < source.Bounds().Max.Y; y++ {
		for x := source.Bounds().Min.X; x < source.Bounds().Max.X; x++ {
			source.SetRGBA(x, y, color.RGBA{R: uint8(x * 7), G: uint8(y * 9), B: uint8(x*y + 30), A: uint8((x + y) * 5)})
		}
	}
	for _, angle := range []float64{0.3, 30, 90, 180, 359.9} {
		got := image.NewRGBA(source.Bounds())
		for i := range got.Pix {
			got.Pix[i] = 255
		}
		rotateImageInto(got, source, angle)
		want := rotateImagePreviousSampling(source, angle)
		if diff := maxPixelChannelDiff(got, want); diff != 0 {
			t.Errorf("rotation at %g° differs by %d channel levels", angle, diff)
		}
	}
}

func TestRotateCropIntoMatchesFullRotateAndCrop(t *testing.T) {
	source := image.NewRGBA(image.Rect(5, 7, 38, 40))
	for y := source.Bounds().Min.Y; y < source.Bounds().Max.Y; y++ {
		for x := source.Bounds().Min.X; x < source.Bounds().Max.X; x++ {
			source.SetRGBA(x, y, color.RGBA{R: uint8(x * 7), G: uint8(y * 9), B: uint8(x*y + 30), A: uint8((x + y) * 5)})
		}
	}
	for _, angle := range []float64{0, 0.3, 30, 90, 180, 359.9} {
		got := image.NewRGBA(image.Rect(0, 0, 24, 24))
		for i := range got.Pix {
			got.Pix[i] = 255
		}
		rotateCropInto(got, source, angle, 24)
		want := cropCenter(rotateImage(source, angle), 24)
		if diff := maxPixelChannelDiff(got, want); diff != 0 {
			t.Errorf("rotation crop at %g° differs by %d channel levels", angle, diff)
		}
	}
}

func rotateImagePreviousSampling(source *image.RGBA, degrees float64) *image.RGBA {
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	result := image.NewRGBA(bounds)
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
	return result
}

func TestSharedRendererSupportsConcurrentWidgetCaches(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		tile := image.NewRGBA(image.Rect(0, 0, maps.TileSize, maps.TileSize))
		if err := png.Encode(w, tile); err != nil {
			t.Errorf("encode tile: %v", err)
		}
	}))
	defer server.Close()
	config := maps.DefaultConfig()
	config.TileURL = server.URL + "/{z}/{x}/{y}.png"
	config.Cache = maps.NewCache("")
	doc, err := layout.Parse(strings.NewReader(`<layout><component type="moving_map" size="80"/><component type="journey_map" x="85" size="80"/><component type="cairo_circuit_map" x="170" size="80"/><component type="cairo_gauge_round_annotated" x="255" size="80" metric="speed" min="0" max="15"/></layout>`))
	if err != nil {
		t.Fatal(err)
	}
	registry := render.NewRegistry()
	RegisterDefaults(&registry)
	workers, err := render.NewWorkers(render.Options{Width: 340, Height: 90, Background: render.Transparent, MapConfig: config}, doc, widgetActivity(), registry, 4)
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	for i, worker := range workers {
		wait.Add(1)
		go func(i int, worker *render.Renderer) {
			defer wait.Done()
			frame, err := worker.FrameAt(time.Duration(i*20) * time.Second)
			if err != nil {
				t.Errorf("FrameAt() error = %v", err)
				return
			}
			if countVisible(frame) == 0 {
				t.Error("FrameAt() drew an empty frame")
			}
		}(i, worker)
	}
	wait.Wait()
}

func TestPrivacyZonePreventsMapRequestsAroundCurrentLocation(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		tile := image.NewRGBA(image.Rect(0, 0, maps.TileSize, maps.TileSize))
		_ = png.Encode(w, tile)
	}))
	defer server.Close()
	config := maps.DefaultConfig()
	config.TileURL = server.URL + "/{z}/{x}/{y}.png"
	config.Cache = maps.NewCache("")
	doc, err := layout.Parse(strings.NewReader(`<layout><component type="moving_map" size="80"/><component type="journey_map" x="90" size="80"/><component type="moving_journey_map" x="180" size="80"/></layout>`))
	if err != nil {
		t.Fatal(err)
	}
	registry := render.NewRegistry()
	RegisterDefaults(&registry)
	zone := maps.PrivacyZone{Center: maps.Point{Lat: 37, Lon: -122}, RadiusMeters: 1000}
	r, err := render.New(render.Options{Width: 270, Height: 90, Background: render.Transparent, MapConfig: config, Privacy: []maps.PrivacyZone{zone}}, doc, widgetActivity(), registry)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := r.FrameAt(0)
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 0 || countVisible(frame) != 0 {
		t.Fatalf("privacy zone requested %d tiles and drew %d pixels", requests.Load(), countVisible(frame))
	}
}

func TestPrivacyZoneBreaksRouteAtHiddenSamples(t *testing.T) {
	activity := widgetActivity()
	hidden := maps.Point{Lat: activity.Samples[1].Latitude.Number, Lon: activity.Samples[1].Longitude.Number}
	segments := publicRouteSegments(activity, []maps.PrivacyZone{{Center: hidden, RadiusMeters: 1}})
	if len(segments) != 2 || len(segments[0]) != 1 || len(segments[1]) != 2 {
		t.Fatalf("privacy filtering joined visible route across hidden sample: %+v", segments)
	}
}

func TestSimplifyRouteKeepsCornersAndDropsCollinearSamples(t *testing.T) {
	got := simplifyRoute([][2]float64{{0, 0}, {1, 0.1}, {2, 0}, {2, 2}}, 1)
	if len(got) != 3 || got[0] != [2]float64{0, 0} || got[1] != [2]float64{2, 0} || got[2] != [2]float64{2, 2} {
		t.Fatalf("simplifyRoute() = %v", got)
	}
}

func TestCompositeAndFramePreserveCoordinatesAndAlpha(t *testing.T) {
	registry := render.NewRegistry()
	RegisterDefaults(&registry)
	doc, err := layout.Parse(strings.NewReader(`<layout><frame x="8" y="8" width="80" height="48" bg="0,0,0,0" opacity="0.5"><composite x="4" y="4"><component type="text" size="20" rgb="255,255,255,255" outline_width="0">X</component></composite></frame></layout>`))
	if err != nil {
		t.Fatal(err)
	}
	r, err := render.New(render.Options{Width: 100, Height: 70, Background: render.Transparent}, doc, widgetActivity(), registry)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := r.FrameAt(0)
	if err != nil {
		t.Fatal(err)
	}
	if frame.RGBAAt(0, 0).A != 0 || frame.RGBAAt(98, 68).A != 0 || countVisible(frame) == 0 {
		t.Fatal("frame clipping or translated text was not rendered")
	}
}

func TestFrameOpacityPreservesMapColors(t *testing.T) {
	registry := render.NewRegistry()
	RegisterDefaults(&registry)
	doc, err := layout.Parse(strings.NewReader(`<layout><frame width="2" height="1" bg="200,100,50,255" opacity="0.5"/></layout>`))
	if err != nil {
		t.Fatal(err)
	}
	r, err := render.New(render.Options{Width: 2, Height: 1, Background: render.Transparent}, doc, widgetActivity(), registry)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := r.FrameAt(0)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := frame.RGBAAt(0, 0), (color.RGBA{R: 100, G: 50, B: 25, A: 128}); got != want {
		t.Fatalf("half-opacity frame pixel = %#v, want %#v", got, want)
	}
}

func TestMetricUnitExpandsGoProUnitPlaceholders(t *testing.T) {
	registry := render.NewRegistry()
	RegisterDefaults(&registry)
	for _, test := range []struct {
		component string
		want      string
	}{
		{`<component type="metric_unit" metric="speed" units="speed" size="16" outline_width="0">{:~c}</component>`, `MPH`},
		{`<component type="metric_unit" metric="temp" units="temp" size="16" outline_width="0">TEMP({:~C})</component>`, `TEMP(°C)`},
	} {
		renderComponent := func(component string) *image.RGBA {
			doc, err := layout.Parse(strings.NewReader(`<layout>` + component + `</layout>`))
			if err != nil {
				t.Fatal(err)
			}
			r, err := render.New(render.Options{Width: 200, Height: 40, Background: render.Transparent}, doc, widgetActivity(), registry)
			if err != nil {
				t.Fatal(err)
			}
			frame, err := r.FrameAt(0)
			if err != nil {
				t.Fatal(err)
			}
			return frame
		}
		got := renderComponent(test.component)
		want := renderComponent(`<component type="text" size="16" outline_width="0">` + test.want + `</component>`)
		if !bytes.Equal(got.Pix, want.Pix) {
			t.Errorf("metric_unit %q did not render as %q", test.component, test.want)
		}
	}
}

func TestTimeFormatSupportsMicroseconds(t *testing.T) {
	stamp := time.Date(2026, time.October, 21, 12, 34, 56, 123456000, time.UTC)
	if got, want := stamp.Format(timeFormat("%H:%M:%S.%f")), "12:34:56.123456"; got != want {
		t.Fatalf("formatted microseconds = %q, want %q", got, want)
	}
}

func TestCompassDrawsInnerRingAndHeadingPointer(t *testing.T) {
	act := widgetActivity()
	act.Samples[0].Course = activity.Value{Number: 37, Valid: true}
	element := &layout.Element{Attributes: []layout.Attribute{{Name: "size", Value: "100"}, {Name: "textsize", Value: "10"}}}
	drawer, err := compassFactory(element, nil)
	if err != nil {
		t.Fatal(err)
	}
	frame := drawStandaloneFrame(t, act, 0, image.Pt(100, 100), drawer)
	if frame.RGBAAt(65, 50).A == 0 {
		t.Fatal("compass inner ring is missing")
	}
	if frame.RGBAAt(47, 8).A == 0 {
		t.Fatal("compass north pointer triangle is missing")
	}
}

func TestTextUsesLayoutColorAndPixelSizedFont(t *testing.T) {
	registry := render.NewRegistry()
	RegisterDefaults(&registry)
	doc, err := layout.Parse(strings.NewReader(`<layout><component type="text" x="20" y="25" size="20" rgb="255,0,0,255" outline_width="0">X</component></layout>`))
	if err != nil {
		t.Fatal(err)
	}
	r, err := render.New(render.Options{Width: 100, Height: 60, Background: render.Transparent}, doc, widgetActivity(), registry)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := r.FrameAt(0)
	if err != nil {
		t.Fatal(err)
	}
	redPixels := 0
	for y := 0; y < frame.Bounds().Dy(); y++ {
		for x := 0; x < frame.Bounds().Dx(); x++ {
			pixel := frame.RGBAAt(x, y)
			if pixel.R > 200 && pixel.G < 30 && pixel.B < 30 && pixel.A > 0 {
				redPixels++
			}
		}
	}
	if redPixels < 20 {
		t.Fatalf("rendered only %d red text pixels", redPixels)
	}
}

func TestMetricFormattingConvertsUnitsAndSuppressesStoppedPace(t *testing.T) {
	tests := []struct {
		name   string
		value  activity.Value
		units  string
		format string
		metric string
		want   string
	}{
		{"speed km/h", activity.Value{Number: 2.7777778, Valid: true}, "km/h", ".1f", "speed", "10.0"},
		{"default speed", activity.Value{Number: 2.7777778, Valid: true}, "speed", ".1f", "speed", "6.2"},
		{"speed pace", activity.Value{Number: 2.7777778, Valid: true}, "pace_km", "pace", "speed", "6:00"},
		{"pace mile", activity.Value{Number: 5.5, Valid: true}, "pace_mi", "pace", "pace", "8:52"},
		{"missing pace", activity.Value{}, "pace_km", "pace", "speed", "—"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := metricText(test.value, test.units, test.format, test.metric); got != test.want {
				t.Fatalf("metricText() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestParseDistanceSupportsCommonUnits(t *testing.T) {
	for _, test := range []struct {
		input string
		want  float64
	}{
		{"100 m", 100},
		{"1.5 km", 1500},
		{"1 mi", 1609.344},
		{"10 ft", 3.048},
	} {
		t.Run(test.input, func(t *testing.T) {
			got, err := parseDistance(test.input)
			if err != nil {
				t.Fatal(err)
			}
			if math.Abs(got-test.want) > 0.0001 {
				t.Fatalf("parseDistance() = %v, want %v", got, test.want)
			}
		})
	}
	if _, err := parseDistance("5 furlongs"); err == nil {
		t.Fatal("parseDistance accepted an unsupported unit")
	}
}

func widgetActivity() *activity.Activity {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	samples := make([]activity.Sample, 4)
	for i := range samples {
		sample := activity.Sample{Timestamp: start.Add(time.Duration(i*20) * time.Second)}
		sample.Latitude = activity.Value{Number: 37 + float64(i)*0.0002, Valid: true}
		sample.Longitude = activity.Value{Number: -122 + float64(i)*0.0003, Valid: true}
		sample.GPSFix = activity.Value{Number: 3, Valid: true}
		sample.GPSLock = activity.Value{Number: 3, Valid: true}
		sample.Speed = activity.Value{Number: 5 + float64(i), Valid: true}
		sample.RawSpeed = sample.Speed
		sample.CSpeed = sample.Speed
		sample.LongSpeed = sample.Speed
		sample.HR = activity.Value{Number: 120 + float64(i), Valid: true}
		sample.Cadence = activity.Value{Number: 88, Valid: true}
		sample.RunningCadence = activity.Value{Number: 176, Valid: true}
		sample.Power = activity.Value{Number: 200 + float64(i*20), Valid: true}
		sample.Temperature = activity.Value{Number: 18, Valid: true}
		sample.Altitude = activity.Value{Number: 300 + float64(i*3), Valid: true}
		sample.Grade = activity.Value{Number: 4, Valid: true}
		sample.CGradient = sample.Grade
		sample.LGradient = sample.Grade
		sample.XLGradient = sample.Grade
		sample.DistanceGradient = sample.Grade
		sample.Odometer = activity.Value{Number: float64(i * 100), Valid: true}
		sample.CumulativeDistance = sample.Odometer
		sample.Course = activity.Value{Number: float64(i * 30), Valid: true}
		sample.Azimuth = sample.Course
		samples[i] = sample
	}
	return &activity.Activity{Samples: samples, Start: samples[0].Timestamp, End: samples[len(samples)-1].Timestamp}
}

func countVisible(frame *image.RGBA) int {
	count := 0
	for y := frame.Bounds().Min.Y; y < frame.Bounds().Max.Y; y++ {
		for x := frame.Bounds().Min.X; x < frame.Bounds().Max.X; x++ {
			if frame.RGBAAt(x, y).A != 0 {
				count++
			}
		}
	}
	return count
}
