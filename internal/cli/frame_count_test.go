package cli

import (
	"testing"
	"time"
)

func TestFrameCountIncludesEndFrame(t *testing.T) {
	got, err := frameCount(1799*100*time.Millisecond, 10)
	if err != nil {
		t.Fatal(err)
	}
	if got != 1800 {
		t.Fatalf("frameCount() = %d, want 1800", got)
	}
}
