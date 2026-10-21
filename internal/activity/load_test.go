package activity

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/muktihari/fit/encoder"
	"github.com/muktihari/fit/kit/datetime"
	"github.com/muktihari/fit/profile/factory"
	"github.com/muktihari/fit/profile/typedef"
	"github.com/muktihari/fit/profile/untyped/fieldnum"
	"github.com/muktihari/fit/profile/untyped/mesgnum"
	"github.com/muktihari/fit/proto"
)

func TestLoadGPXOrdersSamplesAndKeepsOptionalFieldsInvalid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "activity.gpx")
	gpx := `<?xml version="1.0"?><gpx xmlns="http://www.topografix.com/GPX/1/1" xmlns:gpxtpx="http://www.garmin.com/xmlschemas/TrackPointExtension/v1"><trk><trkseg>
<trkpt lat="51.2" lon="-2.5"><ele>100.5</ele><time>2024-07-01T10:00:02Z</time><extensions><gpxtpx:TrackPointExtension><gpxtpx:hr>144</gpxtpx:hr><gpxtpx:cad>170</gpxtpx:cad><gpxtpx:atemp>9</gpxtpx:atemp></gpxtpx:TrackPointExtension><power>180</power><speed>2.8</speed></extensions></trkpt>
<trkpt lat="51.1" lon="-2.6"><time>2024-07-01T10:00:01Z</time><extensions><hr>130</hr></extensions></trkpt>
</trkseg></trk></gpx>`
	if err := os.WriteFile(path, []byte(gpx), 0o600); err != nil {
		t.Fatal(err)
	}

	a, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Samples) != 2 || !a.Start.Equal(a.Samples[0].Timestamp) || !a.End.Equal(a.Samples[1].Timestamp) {
		t.Fatalf("unexpected sample range: %#v", a)
	}
	if got := a.Samples[0].Latitude; !got.Valid || got.Number != 51.1 {
		t.Errorf("first latitude = %+v, want 51.1", got)
	}
	if got := a.Samples[0].HR; !got.Valid || got.Number != 130 {
		t.Errorf("first heart rate = %+v, want 130", got)
	}
	if a.Samples[0].Altitude.Valid || a.Samples[0].Power.Valid {
		t.Errorf("missing values must remain invalid: first sample = %+v", a.Samples[0])
	}
	last := a.Samples[1]
	if last.Altitude.Number != 100.5 || last.HR.Number != 144 || last.Cadence.Number != 170 || last.Temperature.Number != 9 || last.Power.Number != 180 || last.Speed.Number != 2.8 {
		t.Errorf("parsed extensions/fields = %+v", last)
	}
}

func TestLoadFITScalesFieldsAndPersistsGearEvents(t *testing.T) {
	start := time.Date(2024, 7, 1, 10, 0, 0, 0, time.UTC)
	activity := proto.FIT{Messages: []proto.Message{
		{Num: mesgnum.FileId, Fields: []proto.Field{
			fitField(mesgnum.FileId, fieldnum.FileIdType, typedef.FileActivity.Byte()),
			fitField(mesgnum.FileId, fieldnum.FileIdManufacturer, typedef.ManufacturerGarmin.Uint16()),
			fitField(mesgnum.FileId, fieldnum.FileIdProduct, uint16(1)),
			fitField(mesgnum.FileId, fieldnum.FileIdTimeCreated, datetime.ToUint32(start)),
		}},
		fitRecord(start, 51.2, -2.5),
		fitRecord(start.Add(time.Second), 51.2001, -2.5001),
		{Num: mesgnum.Event, Fields: []proto.Field{
			fitField(mesgnum.Event, fieldnum.EventTimestamp, datetime.ToUint32(start.Add(time.Second))),
			fitField(mesgnum.Event, fieldnum.EventEvent, typedef.EventFrontGearChange.Byte()),
			fitField(mesgnum.Event, fieldnum.EventFrontGearNum, uint8(2)),
			fitField(mesgnum.Event, fieldnum.EventRearGearNum, uint8(5)),
		}},
		fitRecord(start.Add(2*time.Second), 51.2002, -2.5002),
	}}
	path := filepath.Join(t.TempDir(), "activity.fit")
	var data bytes.Buffer
	if err := encoder.New(&data).Encode(&activity); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Samples) != 3 {
		t.Fatalf("got %d samples, want 3", len(got.Samples))
	}
	first := got.Samples[0]
	if math.Abs(first.Distance.Number-123.45) > 1e-6 || math.Abs(first.Altitude.Number-96.6) > 1e-6 || math.Abs(first.Speed.Number-1.234) > 1e-6 || first.VerticalSpeed.Number != -1.25 || first.HR.Number != 72 {
		t.Errorf("FIT values are not scaled: %+v", first)
	}
	if math.Abs(first.Latitude.Number-51.2) > 1e-6 || math.Abs(first.Longitude.Number-(-2.5)) > 1e-6 {
		t.Errorf("FIT coordinates are not converted from semicircles: (%v, %v)", first.Latitude, first.Longitude)
	}
	if first.GPSFix.Number != 3 || first.FrontGear.Valid {
		t.Errorf("unexpected initial GPS/gear values: %+v", first)
	}
	for _, sample := range got.Samples[1:] {
		if sample.FrontGear.Number != 2 || sample.RearGear.Number != 5 {
			t.Errorf("gear event did not persist at %s: front=%+v rear=%+v", sample.Timestamp, sample.FrontGear, sample.RearGear)
		}
	}
}

func fitField(mesg typedef.MesgNum, num byte, value any) proto.Field {
	return factory.CreateField(mesg, num).WithValue(value)
}

func fitRecord(at time.Time, lat, lon float64) proto.Message {
	semicircles := func(degrees float64) int32 { return int32(degrees * (1 << 31) / 180) }
	return proto.Message{Num: mesgnum.Record, Fields: []proto.Field{
		fitField(mesgnum.Record, fieldnum.RecordTimestamp, datetime.ToUint32(at)),
		fitField(mesgnum.Record, fieldnum.RecordPositionLat, semicircles(lat)),
		fitField(mesgnum.Record, fieldnum.RecordPositionLong, semicircles(lon)),
		fitField(mesgnum.Record, fieldnum.RecordDistance, uint32(12345)),
		fitField(mesgnum.Record, fieldnum.RecordAltitude, uint16(2983)),
		fitField(mesgnum.Record, fieldnum.RecordSpeed, uint16(1234)),
		fitField(mesgnum.Record, fieldnum.RecordVerticalSpeed, int16(-1250)),
		fitField(mesgnum.Record, fieldnum.RecordHeartRate, uint8(72)),
		fitField(mesgnum.Record, fieldnum.RecordCadence, uint8(88)),
		fitField(mesgnum.Record, fieldnum.RecordPower, uint16(123)),
		fitField(mesgnum.Record, fieldnum.RecordTemperature, int8(16)),
		fitField(mesgnum.Record, fieldnum.RecordGrade, int16(350)),
	}}
}
