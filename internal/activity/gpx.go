package activity

import (
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

type gpxTrackPoint struct {
	Latitude   string       `xml:"lat,attr"`
	Longitude  string       `xml:"lon,attr"`
	Elevation  *float64     `xml:"ele"`
	Time       string       `xml:"time"`
	Extensions []gpxElement `xml:"extensions"`
}

type gpxElement struct {
	XMLName  xml.Name
	Text     string       `xml:",chardata"`
	Children []gpxElement `xml:",any"`
}

func loadGPX(r io.Reader) (*Activity, error) {
	dec := xml.NewDecoder(r)
	var samples []Sample
	for {
		token, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse GPX: %w", err)
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "trkpt" {
			continue
		}
		var point gpxTrackPoint
		if err := dec.DecodeElement(&point, &start); err != nil {
			return nil, fmt.Errorf("parse track point: %w", err)
		}
		sample, err := parseGPXPoint(point, len(samples)+1)
		if err != nil {
			return nil, err
		}
		samples = append(samples, sample)
	}
	return orderedActivity(samples)
}

func parseGPXPoint(point gpxTrackPoint, index int) (Sample, error) {
	lat, err := strconv.ParseFloat(strings.TrimSpace(point.Latitude), 64)
	if err != nil || lat < -90 || lat > 90 {
		return Sample{}, fmt.Errorf("GPX track point %d has invalid latitude %q", index, point.Latitude)
	}
	lon, err := strconv.ParseFloat(strings.TrimSpace(point.Longitude), 64)
	if err != nil || lon < -180 || lon > 180 {
		return Sample{}, fmt.Errorf("GPX track point %d has invalid longitude %q", index, point.Longitude)
	}
	timestamp, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(point.Time))
	if err != nil {
		return Sample{}, fmt.Errorf("GPX track point %d has invalid or missing time %q", index, point.Time)
	}
	sample := Sample{
		Timestamp:      timestamp.UTC(),
		Latitude:       value(lat),
		Longitude:      value(lon),
		GPSFix:         value(3),
		GPSLock:        value(3),
		GPSPacket:      value(float64(index - 1)),
		GPSPacketIndex: value(0),
	}
	if point.Elevation != nil {
		sample.Altitude = value(*point.Elevation)
	}
	for _, extension := range point.Extensions {
		if err := readGPXExtensions(&sample, extension); err != nil {
			return Sample{}, fmt.Errorf("GPX track point %d: %w", index, err)
		}
	}
	return sample, nil
}

func readGPXExtensions(sample *Sample, element gpxElement) error {
	name := element.XMLName.Local
	var dst *Value
	switch name {
	case "hr":
		dst = &sample.HR
	case "cad":
		dst = &sample.Cadence
	case "atemp":
		dst = &sample.Temperature
	case "power":
		dst = &sample.Power
	case "speed":
		dst = &sample.Speed
	}
	if dst != nil {
		number, err := strconv.ParseFloat(strings.TrimSpace(element.Text), 64)
		if err != nil {
			return fmt.Errorf("invalid %s extension value %q", name, element.Text)
		}
		setValue(dst, number)
	}
	for _, child := range element.Children {
		if err := readGPXExtensions(sample, child); err != nil {
			return err
		}
	}
	return nil
}
