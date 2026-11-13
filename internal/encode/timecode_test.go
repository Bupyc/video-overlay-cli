package encode

import (
	"testing"
	"time"
)

func TestSMPTETimecodeUsesUTCAndOutputRate(t *testing.T) {
	for _, tc := range []struct {
		name string
		at   time.Time
		fps  string
		want string
	}{
		{"drop frame at minute boundary", time.Date(2025, 1, 1, 11, 1, 0, 0, time.FixedZone("UTC+3", 3*60*60)), "30000/1001", "08:01:00;02"},
		{"drop frame decimal rate", time.Date(2025, 1, 1, 8, 10, 0, 0, time.UTC), "29.97", "08:10:00;00"},
		{"high-rate drop frame", time.Date(2025, 1, 1, 8, 1, 0, 0, time.UTC), "60000/1001", "08:01:00;04"},
		{"fraction rounds to nearest output frame", time.Date(2025, 1, 1, 8, 1, 1, 50_000_000, time.UTC), "30000/1001", "08:01:01;01"},
		{"non-drop frame", time.Date(2025, 1, 1, 8, 0, 0, 250_000_000, time.UTC), "10", "08:00:00:03"},
		{"rounding carries into next second", time.Date(2025, 1, 1, 8, 0, 59, 999_000_000, time.UTC), "10", "08:01:00:00"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := smpteTimecode(tc.at, tc.fps)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("smpteTimecode() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSMPTETimecodeRejectsSubframeRates(t *testing.T) {
	if _, err := smpteTimecode(time.Now(), "1/4"); err == nil {
		t.Fatal("smpteTimecode accepted a rate with no integer timecode frame rate")
	}
}
