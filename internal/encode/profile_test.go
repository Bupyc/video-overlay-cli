package encode

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadFFmpegProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ffmpeg-profiles.json")
	content := `{"quality":{"input":["-hwaccel","videotoolbox"],"output":["-crf","29"],"filter":"[0:v][1:v]overlay"}}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFFmpegProfile(path, "quality")
	if err != nil {
		t.Fatal(err)
	}
	want := FFmpegProfile{
		Input:  []string{"-hwaccel", "videotoolbox"},
		Output: []string{"-crf", "29"},
		Filter: "[0:v][1:v]overlay",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadFFmpegProfile() = %#v, want %#v", got, want)
	}
}

func TestLoadFFmpegProfileRejectsUnknownOrIncompleteProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ffmpeg-profiles.json")
	if err := os.WriteFile(path, []byte(`{"incomplete":{"input":[]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"missing", "incomplete"} {
		if _, err := LoadFFmpegProfile(path, name); err == nil {
			t.Errorf("LoadFFmpegProfile(%q) accepted invalid profile", name)
		}
	}
}
