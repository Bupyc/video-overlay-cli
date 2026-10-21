package maps

import (
	"image"
	"math"
	"testing"
)

func TestProjectPixelUsesWebMercator(t *testing.T) {
	for _, test := range []struct {
		point Point
		zoom  int
		wantX float64
		wantY float64
	}{
		{Point{Lon: 0, Lat: 0}, 0, 128, 128},
		{Point{Lon: 180, Lat: 0}, 1, 512, 256},
		{Point{Lon: 0, Lat: MaxLatitude}, 0, 128, 0},
	} {
		x, y := ProjectPixel(test.point, test.zoom)
		if math.Abs(x-test.wantX) > 0.01 || math.Abs(y-test.wantY) > 0.01 {
			t.Errorf("ProjectPixel(%+v, %d) = (%f, %f), want (%f, %f)", test.point, test.zoom, x, y, test.wantX, test.wantY)
		}
	}
}

func TestFitCentersAndContainsRoute(t *testing.T) {
	points := []Point{{Lon: -122, Lat: 37}, {Lon: -121.99, Lat: 37.01}}
	viewport, err := Fit(points, image.Pt(256, 128), 18)
	if err != nil {
		t.Fatal(err)
	}
	if viewport.Zoom <= 0 || viewport.Zoom > 18 {
		t.Fatalf("Fit zoom = %d", viewport.Zoom)
	}
	for _, point := range points {
		pixel := viewport.Pixel(point)
		if pixel.X < 0 || pixel.X >= viewport.Size.X || pixel.Y < 0 || pixel.Y >= viewport.Size.Y {
			t.Errorf("route point %+v projected outside viewport: %v", point, pixel)
		}
	}
	center := viewport.Pixel(Point{Lon: -121.995, Lat: 37.005})
	if math.Abs(float64(center.X-128)) > 1 || math.Abs(float64(center.Y-64)) > 1 {
		t.Fatalf("route midpoint projected to %v", center)
	}
}

func TestNewViewportRejectsInvalidGeometry(t *testing.T) {
	if _, err := NewViewport(Point{Lon: 181, Lat: 0}, 2, image.Pt(256, 256)); err == nil {
		t.Fatal("accepted out-of-range longitude")
	}
	if _, err := NewViewport(Point{Lon: 0, Lat: 0}, 2, image.Pt(0, 256)); err == nil {
		t.Fatal("accepted empty viewport")
	}
}

func TestPrivacyZoneUsesDistanceInMeters(t *testing.T) {
	zone := PrivacyZone{Center: Point{Lon: -122, Lat: 37}, RadiusMeters: 500}
	if !zone.Contains(Point{Lon: -122, Lat: 37}) {
		t.Fatal("privacy zone did not include its center")
	}
	if zone.Contains(Point{Lon: -121.99, Lat: 37}) {
		t.Fatal("privacy zone included a point beyond its radius")
	}
}
