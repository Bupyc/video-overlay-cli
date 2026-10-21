package maps

import (
	"fmt"
	"image"
	"math"
)

const (
	TileSize    = 256
	MaxLatitude = 85.05112878
)

type Point struct {
	Lon float64
	Lat float64
}

type Viewport struct {
	Center Point
	Zoom   int
	Size   image.Point
}

type PrivacyZone struct {
	Center       Point
	RadiusMeters float64
}

func NewViewport(center Point, zoom int, size image.Point) (Viewport, error) {
	if !validPoint(center) || center.Lon < -180 || center.Lon > 180 || center.Lat < -90 || center.Lat > 90 {
		return Viewport{}, fmt.Errorf("invalid map center (%g, %g)", center.Lon, center.Lat)
	}
	if zoom < 0 || zoom > 22 {
		return Viewport{}, fmt.Errorf("map zoom %d is outside 0..22", zoom)
	}
	if size.X <= 0 || size.Y <= 0 || size.X > 16384 || size.Y > 16384 {
		return Viewport{}, fmt.Errorf("invalid map size %dx%d", size.X, size.Y)
	}
	center.Lat = clamp(center.Lat, -MaxLatitude, MaxLatitude)
	return Viewport{Center: center, Zoom: zoom, Size: size}, nil
}

func Fit(points []Point, size image.Point, maxZoom int) (Viewport, error) {
	if len(points) == 0 {
		return Viewport{}, fmt.Errorf("cannot fit an empty route")
	}
	if maxZoom < 0 || maxZoom > 22 {
		return Viewport{}, fmt.Errorf("invalid maximum map zoom %d", maxZoom)
	}
	if size.X <= 0 || size.Y <= 0 || size.X > 16384 || size.Y > 16384 {
		return Viewport{}, fmt.Errorf("invalid map size %dx%d", size.X, size.Y)
	}
	firstX, _ := ProjectPixel(points[0], 0)
	minX, maxX := firstX, firstX
	_, firstY := ProjectPixel(points[0], 0)
	minY, maxY := firstY, firstY
	for _, point := range points {
		if !validPoint(point) || point.Lon < -180 || point.Lon > 180 || point.Lat < -90 || point.Lat > 90 {
			return Viewport{}, fmt.Errorf("invalid route point (%g, %g)", point.Lon, point.Lat)
		}
		x, y := ProjectPixel(point, 0)
		for x-firstX > TileSize/2 {
			x -= TileSize
		}
		for x-firstX < -TileSize/2 {
			x += TileSize
		}
		minX, maxX = math.Min(minX, x), math.Max(maxX, x)
		minY, maxY = math.Min(minY, y), math.Max(maxY, y)
	}
	spanX, spanY := maxX-minX, maxY-minY
	zoom := maxZoom
	if spanX > 0 || spanY > 0 {
		ratioX, ratioY := math.Inf(1), math.Inf(1)
		if spanX > 0 {
			ratioX = float64(max(1, size.X-2)) / spanX
		}
		if spanY > 0 {
			ratioY = float64(max(1, size.Y-2)) / spanY
		}
		zoom = int(math.Floor(math.Log2(math.Min(ratioX, ratioY))))
		zoom = max(0, min(maxZoom, zoom))
	}
	center := UnprojectPixel((minX+maxX)/2*math.Pow(2, float64(zoom)), (minY+maxY)/2*math.Pow(2, float64(zoom)), zoom)
	return NewViewport(center, zoom, size)
}

func ProjectPixel(point Point, zoom int) (float64, float64) {
	lat := clamp(point.Lat, -MaxLatitude, MaxLatitude) * math.Pi / 180
	world := float64(TileSize) * math.Pow(2, float64(zoom))
	x := (point.Lon + 180) / 360 * world
	y := (1 - math.Log(math.Tan(lat)+1/math.Cos(lat))/math.Pi) / 2 * world
	return x, y
}

func UnprojectPixel(x, y float64, zoom int) Point {
	world := float64(TileSize) * math.Pow(2, float64(zoom))
	lon := x/world*360 - 180
	n := math.Pi - 2*math.Pi*y/world
	lat := 180 / math.Pi * math.Atan(math.Sinh(n))
	return Point{Lon: lon, Lat: lat}
}

func (v Viewport) Project(point Point) (float64, float64) {
	x, y := ProjectPixel(point, v.Zoom)
	centerX, centerY := ProjectPixel(v.Center, v.Zoom)
	world := float64(TileSize) * math.Pow(2, float64(v.Zoom))
	if x-centerX > world/2 {
		x -= world
	} else if x-centerX < -world/2 {
		x += world
	}
	return x - centerX + float64(v.Size.X)/2, y - centerY + float64(v.Size.Y)/2
}

func (v Viewport) Pixel(point Point) image.Point {
	x, y := v.Project(point)
	return image.Pt(int(math.Round(x)), int(math.Round(y)))
}

func (zone PrivacyZone) Contains(point Point) bool {
	if zone.RadiusMeters <= 0 || math.IsNaN(zone.RadiusMeters) || math.IsInf(zone.RadiusMeters, 0) || !validPoint(zone.Center) || !validPoint(point) || math.Abs(zone.Center.Lat) > 90 || math.Abs(point.Lat) > 90 || math.Abs(zone.Center.Lon) > 180 || math.Abs(point.Lon) > 180 {
		return false
	}
	const earthRadius = 6371008.8
	lat1, lat2 := zone.Center.Lat*math.Pi/180, point.Lat*math.Pi/180
	dLat := lat2 - lat1
	dLon := math.Remainder(point.Lon-zone.Center.Lon, 360) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1)*math.Cos(lat2)*math.Sin(dLon/2)*math.Sin(dLon/2)
	distance := 2 * earthRadius * math.Asin(math.Min(1, math.Sqrt(a)))
	return distance <= zone.RadiusMeters
}

func validPoint(point Point) bool {
	return !math.IsNaN(point.Lon) && !math.IsInf(point.Lon, 0) && !math.IsNaN(point.Lat) && !math.IsInf(point.Lat, 0)
}

func clamp(value, low, high float64) float64 {
	return math.Max(low, math.Min(high, value))
}
