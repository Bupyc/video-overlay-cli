package cli

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTransparentFrameWindowScalesWithEncoderPressure(t *testing.T) {
	window := newTransparentFrameWindow(16)
	if got := window.observe(0, false); got != 4 {
		t.Fatalf("initial frame window = %d, want 4", got)
	}
	for _, want := range []int{8, 12, 16, 16} {
		if got := window.observe(100*time.Millisecond, false); got != want {
			t.Fatalf("frame window after encoder wait = %d, want %d", got, want)
		}
	}
	for _, want := range []int{16, 16, 12, 12, 12, 8, 8, 8, 4, 4, 4} {
		if got := window.observe(0, true); got != want {
			t.Fatalf("frame window after queued output = %d, want %d", got, want)
		}
	}
}

func TestTransparentRenderWorkersAreBoundedByFrameWindow(t *testing.T) {
	for _, tc := range []struct {
		workers     int
		transparent bool
		want        int
	}{
		{workers: 16, transparent: true, want: 4},
		{workers: 3, transparent: true, want: 3},
		{workers: 16, transparent: false, want: 16},
	} {
		if got := streamWorkerLimit(tc.workers, tc.transparent); got != tc.want {
			t.Errorf("streamWorkerLimit(%d, %t) = %d, want %d", tc.workers, tc.transparent, got, tc.want)
		}
	}
}

func TestRenderWorkerCountHonorsExplicitRequest(t *testing.T) {
	if got := renderWorkerCount(24); got != 24 {
		t.Fatalf("renderWorkerCount(24) = %d, want 24", got)
	}
}

func TestRunPrintsRootAndRenderHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := Run(context.Background(), []string{"--help"}, &stdout, &stderr); err != nil {
		t.Fatalf("root help returned an error: %v", err)
	}
	if !strings.Contains(stdout.String(), "Usage: video-overlay-cli <command>") || !strings.Contains(stdout.String(), "render") {
		t.Fatalf("unexpected root help: %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if err := Run(context.Background(), []string{"render", "--help"}, &stdout, &stderr); err != nil {
		t.Fatalf("render help returned an error: %v", err)
	}
	for _, flag := range []string{"--activity", "--layout", "--output", "--size", "--fps", "--background", "--profile", "--ffmpeg-profile", "--ffmpeg-profiles", "--ffmpeg-arg", "--workers", "--font", "--ffmpeg", "--video", "--video-offset", "--privacy", "--tile-url", "--tile-cache", "--tile-attribution"} {
		if !strings.Contains(stdout.String(), flag) {
			t.Errorf("render help does not include %s: %q", flag, stdout.String())
		}
	}
	if !strings.Contains(stdout.String(), "alpha-vp9") {
		t.Errorf("render help does not list alpha-vp9: %q", stdout.String())
	}
}

func TestRunRenderAppliesCustomFFmpegProfileAndArguments(t *testing.T) {
	dir := t.TempDir()
	activityPath, layoutPath := writeCLIInputs(t, dir, `<layout/>`)
	profilePath := filepath.Join(dir, "ffmpeg-profiles.json")
	if err := os.WriteFile(profilePath, []byte(`{"balanced":{"input":["-thread_queue_size","64"],"output":["-crf","31"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	argsPath := filepath.Join(dir, "args.txt")
	ffmpegPath := filepath.Join(dir, "ffmpeg")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > '" + argsPath + "'\ncat >/dev/null\n"
	if err := os.WriteFile(ffmpegPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	err := Run(context.Background(), []string{
		"render", "--activity", activityPath, "--layout", layoutPath, "--output", filepath.Join(dir, "overlay.webm"),
		"--size", "2x2", "--fps", "4", "--workers", "1", "--background", "transparent", "--profile", "alpha-vp9",
		"--ffmpeg-profile", "balanced", "--ffmpeg-profiles", profilePath, "--ffmpeg-arg=-cpu-used", "--ffmpeg-arg=3",
		"--ffmpeg", ffmpegPath,
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Run returned %v (stderr %q)", err, stderr.String())
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(args)
	for _, arg := range []string{"libvpx-vp9", "-crf\n31\n", "-cpu-used\n3\n"} {
		if !strings.Contains(got, arg) {
			t.Errorf("FFmpeg args missing %q:\n%s", arg, got)
		}
	}
}

func TestRunRenderComposesTransparentOverlayWithExplicitOffset(t *testing.T) {
	dir := t.TempDir()
	activityPath, layoutPath := writeCLIInputs(t, dir, `<layout/>`)
	videoPath := filepath.Join(dir, "camera.mov")
	argsPath, rawPath := filepath.Join(dir, "args.txt"), filepath.Join(dir, "overlay.rgba")
	if err := os.WriteFile(videoPath, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	ffmpegPath := filepath.Join(dir, "ffmpeg")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > '" + argsPath + "'\ncat > '" + rawPath + "'\n"
	if err := os.WriteFile(ffmpegPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	err := Run(context.Background(), []string{
		"render", "--activity", activityPath, "--layout", layoutPath, "--output", filepath.Join(dir, "composed.mov"),
		"--size", "2x2", "--fps", "4", "--workers", "1", "--background", "transparent", "--profile", "black-h264",
		"--ffmpeg", ffmpegPath, "--video", videoPath, "--video-offset", "1.25",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Run returned %v (stderr %q)", err, stderr.String())
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(args), "1.25/TB") || !strings.Contains(string(args), videoPath) || !strings.Contains(string(args), "0:a?") {
		t.Fatalf("FFmpeg args do not describe video composition at +1.25s:\n%s", args)
	}
	raw, err := os.ReadFile(rawPath)
	if err != nil || len(raw) != 48 {
		t.Fatalf("raw RGBA overlay length = %d, error = %v; want 48", len(raw), err)
	}
}

func TestRunRejectsInvalidLayoutBeforeOpeningOutput(t *testing.T) {
	dir := t.TempDir()
	activityPath, layoutPath := writeCLIInputs(t, dir, `<layout><component type="not-a-widget"/></layout>`)
	outputPath, markerPath := filepath.Join(dir, "existing.mov"), filepath.Join(dir, "ffmpeg-started")
	if err := os.WriteFile(outputPath, []byte("preserve this output"), 0o600); err != nil {
		t.Fatal(err)
	}
	ffmpegPath := filepath.Join(dir, "ffmpeg")
	if err := os.WriteFile(ffmpegPath, []byte("#!/bin/sh\ntouch '"+markerPath+"'\ncat >/dev/null\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	err := Run(context.Background(), []string{"render", "--activity", activityPath, "--layout", layoutPath, "--output", outputPath, "--ffmpeg", ffmpegPath}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "not-a-widget") {
		t.Fatalf("Run error = %v, want unsupported component", err)
	}
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Fatalf("FFmpeg marker exists or stat failed: %v", err)
	}
	if content, err := os.ReadFile(outputPath); err != nil || string(content) != "preserve this output" {
		t.Fatalf("existing output changed to %q, error = %v", content, err)
	}
}

func TestRunSuggestsWebMForAlphaVP9Output(t *testing.T) {
	dir := t.TempDir()
	activityPath, layoutPath := writeCLIInputs(t, dir, `<layout/>`)
	markerPath := filepath.Join(dir, "ffmpeg-started")
	ffmpegPath := filepath.Join(dir, "ffmpeg")
	if err := os.WriteFile(ffmpegPath, []byte("#!/bin/sh\ntouch '"+markerPath+"'\ncat >/dev/null\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	err := Run(context.Background(), []string{
		"render", "--activity", activityPath, "--layout", layoutPath, "--output", filepath.Join(dir, "overlay.mov"),
		"--size", "2x2", "--fps", "4", "--background", "transparent", "--profile", "alpha-vp9", "--workers", "1", "--ffmpeg", ffmpegPath,
	}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), ".webm") {
		t.Fatalf("Run error = %v, want a WebM output suggestion", err)
	}
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Fatalf("FFmpeg started before rejecting the output extension: %v", err)
	}
}

func TestRunRejectsVideoOutputCollisionBeforeStartingFFmpeg(t *testing.T) {
	dir := t.TempDir()
	activityPath, layoutPath := writeCLIInputs(t, dir, `<layout/>`)
	videoPath, markerPath := filepath.Join(dir, "camera.mov"), filepath.Join(dir, "ffmpeg-started")
	if err := os.WriteFile(videoPath, []byte("preserve the source video"), 0o600); err != nil {
		t.Fatal(err)
	}
	ffmpegPath := filepath.Join(dir, "ffmpeg")
	if err := os.WriteFile(ffmpegPath, []byte("#!/bin/sh\ntouch '"+markerPath+"'\ncat >/dev/null\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	err := Run(context.Background(), []string{
		"render", "--activity", activityPath, "--layout", layoutPath, "--output", videoPath,
		"--video", videoPath, "--background", "transparent", "--ffmpeg", ffmpegPath,
	}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "output path must differ from input") {
		t.Fatalf("Run error = %v, want output/input collision", err)
	}
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Fatalf("FFmpeg marker exists or stat failed: %v", err)
	}
	if content, err := os.ReadFile(videoPath); err != nil || string(content) != "preserve the source video" {
		t.Fatalf("source video changed to %q, error = %v", content, err)
	}
}

func TestRunRejectsMissingVideoBeforeStartingFFmpeg(t *testing.T) {
	dir := t.TempDir()
	activityPath, layoutPath := writeCLIInputs(t, dir, `<layout/>`)
	outputPath, markerPath := filepath.Join(dir, "existing.mov"), filepath.Join(dir, "ffmpeg-started")
	missingVideo := filepath.Join(dir, "missing.mov")
	if err := os.WriteFile(outputPath, []byte("preserve this output"), 0o600); err != nil {
		t.Fatal(err)
	}
	ffmpegPath := filepath.Join(dir, "ffmpeg")
	if err := os.WriteFile(ffmpegPath, []byte("#!/bin/sh\ntouch '"+markerPath+"'\ncat >/dev/null\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	err := Run(context.Background(), []string{
		"render", "--activity", activityPath, "--layout", layoutPath, "--output", outputPath,
		"--video", missingVideo, "--background", "transparent", "--ffmpeg", ffmpegPath,
	}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "open source video") {
		t.Fatalf("Run error = %v, want missing video error", err)
	}
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Fatalf("FFmpeg marker exists or stat failed: %v", err)
	}
	if content, err := os.ReadFile(outputPath); err != nil || string(content) != "preserve this output" {
		t.Fatalf("existing output changed to %q, error = %v", content, err)
	}
}

func writeCLIInputs(t *testing.T, dir, layoutSource string) (string, string) {
	t.Helper()
	activityPath, layoutPath := filepath.Join(dir, "activity.gpx"), filepath.Join(dir, "layout.xml")
	if err := os.WriteFile(activityPath, []byte(`<gpx><trk><trkseg><trkpt lat="37" lon="-122"><time>2025-01-01T00:00:00Z</time></trkpt><trkpt lat="37.0001" lon="-122"><time>2025-01-01T00:00:00.5Z</time></trkpt></trkseg></trk></gpx>`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layoutPath, []byte(layoutSource), 0o600); err != nil {
		t.Fatal(err)
	}
	return activityPath, layoutPath
}

func TestParsePrivacy(t *testing.T) {
	zones, err := parsePrivacy("37.5, -122.4, 1.25")
	if err != nil {
		t.Fatal(err)
	}
	if len(zones) != 1 || zones[0].Center.Lat != 37.5 || zones[0].Center.Lon != -122.4 || zones[0].RadiusMeters != 1250 {
		t.Fatalf("parsePrivacy() = %+v", zones)
	}
	if zones, err := parsePrivacy(""); err != nil || len(zones) != 0 {
		t.Fatalf("empty privacy zone = %+v, %v", zones, err)
	}
	for _, invalid := range []string{"37,-122", "91,-122,1", "37,-181,1", "37,-122,0", "NaN,0,1"} {
		if _, err := parsePrivacy(invalid); err == nil {
			t.Errorf("parsePrivacy(%q) accepted invalid input", invalid)
		}
	}
}

func TestRunRenderStreamsGeneratedFramesToFFmpeg(t *testing.T) {
	dir := t.TempDir()
	activityPath := filepath.Join(dir, "activity.gpx")
	layoutPath := filepath.Join(dir, "layout.xml")
	rawPath := filepath.Join(dir, "frames.rgba")
	ffmpegPath := filepath.Join(dir, "ffmpeg")
	if err := os.WriteFile(activityPath, []byte(`<gpx><trk><trkseg><trkpt lat="37" lon="-122"><time>2025-01-01T00:00:00Z</time></trkpt><trkpt lat="37.0001" lon="-122"><time>2025-01-01T00:00:00.5Z</time></trkpt></trkseg></trk></gpx>`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layoutPath, []byte(`<layout/>`), 0o600); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\ncat > '" + strings.ReplaceAll(rawPath, "'", "'\\''") + "'\n"
	if err := os.WriteFile(ffmpegPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	err := Run(context.Background(), []string{
		"render", "--activity", activityPath, "--layout", layoutPath, "--output", filepath.Join(dir, "overlay.mov"),
		"--size", "2x2", "--fps", "4", "--background", "transparent", "--profile", "alpha-prores", "--workers", "2", "--ffmpeg", ffmpegPath,
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Run returned %v (stderr %q)", err, stderr.String())
	}
	frames, err := os.ReadFile(rawPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(frames), 3*2*2*4; got != want {
		t.Fatalf("streamed %d bytes, want %d (three RGBA frames)", got, want)
	}
	if !strings.Contains(stdout.String(), fmt.Sprintf("%d frames", 3)) {
		t.Fatalf("unexpected success output: %q", stdout.String())
	}
}

func TestRunRenderPreparesMetricWindowsBeforeDrawing(t *testing.T) {
	dir := t.TempDir()
	layoutPath := filepath.Join(dir, "layout.xml")
	if err := os.WriteFile(layoutPath, []byte(`<layout><component type="metric" x="5" y="5" metric="gradient" format=".0f" size="24"/></layout>`), 0o600); err != nil {
		t.Fatal(err)
	}
	outputs := make([][]byte, 0, 2)
	for _, test := range []struct {
		name string
		ele  string
	}{
		{name: "with-altitude", ele: "<ele>500</ele>"},
		{name: "without-altitude"},
	} {
		activityPath := filepath.Join(dir, test.name+".gpx")
		points := make([]string, 4)
		for i := range points {
			ele := ""
			if test.ele != "" {
				ele = fmt.Sprintf("<ele>%d</ele>", 500+i)
			}
			points[i] = fmt.Sprintf(`<trkpt lat="45.%04d" lon="7"><time>2025-01-01T00:00:%02dZ</time>%s</trkpt>`, i+1, i, ele)
		}
		gpx := "<gpx><trk><trkseg>" + strings.Join(points, "") + "</trkseg></trk></gpx>"
		if err := os.WriteFile(activityPath, []byte(gpx), 0o600); err != nil {
			t.Fatal(err)
		}
		rawPath := filepath.Join(dir, test.name+".rgba")
		ffmpegPath := filepath.Join(dir, test.name+"-ffmpeg")
		script := "#!/bin/sh\ncat > '" + strings.ReplaceAll(rawPath, "'", "'\\''") + "'\n"
		if err := os.WriteFile(ffmpegPath, []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		err := Run(context.Background(), []string{
			"render", "--activity", activityPath, "--layout", layoutPath, "--output", filepath.Join(dir, test.name+".mov"),
			"--size", "400x100", "--fps", "1", "--workers", "1", "--ffmpeg", ffmpegPath,
		}, &stdout, &stderr)
		if err != nil {
			t.Fatalf("Run(%s) returned %v (stderr %q)", test.name, err, stderr.String())
		}
		data, err := os.ReadFile(rawPath)
		if err != nil {
			t.Fatal(err)
		}
		outputs = append(outputs, data)
	}
	if bytes.Equal(outputs[0], outputs[1]) {
		t.Fatal("gradient metric was not rendered from prepared altitude windows")
	}
}

func TestRunRenderReturnsTileProviderErrors(t *testing.T) {
	dir := t.TempDir()
	activityPath := filepath.Join(dir, "activity.gpx")
	layoutPath := filepath.Join(dir, "layout.xml")
	rawPath := filepath.Join(dir, "frames.rgba")
	ffmpegPath := filepath.Join(dir, "ffmpeg")
	if err := os.WriteFile(activityPath, []byte(`<gpx><trk><trkseg><trkpt lat="37" lon="-122"><time>2025-01-01T00:00:00Z</time></trkpt><trkpt lat="37.0001" lon="-122"><time>2025-01-01T00:00:00.5Z</time></trkpt></trkseg></trk></gpx>`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layoutPath, []byte(`<layout><component type="moving_map" size="64"/></layout>`), 0o600); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\ncat > '" + strings.ReplaceAll(rawPath, "'", "'\\''") + "'\n"
	if err := os.WriteFile(ffmpegPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "tile service unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	err := Run(context.Background(), []string{
		"render", "--activity", activityPath, "--layout", layoutPath, "--output", filepath.Join(dir, "overlay.mov"),
		"--size", "64x64", "--fps", "1", "--workers", "1", "--ffmpeg", ffmpegPath,
		"--tile-url", server.URL + "/{z}/{x}/{y}.png", "--tile-cache", filepath.Join(dir, "tiles"),
	}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "tile server returned HTTP 503") {
		t.Fatalf("Run returned %v, want a tile provider error (stderr %q)", err, stderr.String())
	}
}
