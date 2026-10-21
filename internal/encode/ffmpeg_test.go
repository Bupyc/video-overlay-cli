package encode

import (
	"bufio"
	"bytes"
	"context"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteFrameWritesContiguousRGBAInOneChunk(t *testing.T) {
	underlying := &recordingWriter{}
	encoder := &Encoder{writer: bufio.NewWriterSize(underlying, 16), width: 5, height: 3}
	frame := image.NewRGBA(image.Rect(0, 0, 5, 3))
	for i := range frame.Pix {
		frame.Pix[i] = byte(i)
	}
	if err := encoder.WriteFrame(frame); err != nil {
		t.Fatal(err)
	}
	if err := encoder.writer.Flush(); err != nil {
		t.Fatal(err)
	}
	if underlying.writes != 1 {
		t.Fatalf("WriteFrame made %d writes, want 1", underlying.writes)
	}
	if !bytes.Equal(underlying.data, frame.Pix) {
		t.Fatalf("written frame differs from source")
	}
}

type recordingWriter struct {
	writes int
	data   []byte
}

func (w *recordingWriter) Write(data []byte) (int, error) {
	w.writes++
	w.data = append(w.data, data...)
	return len(data), nil
}

func TestStartStreamsRGBAWithAlphaProResProfile(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	rawPath := filepath.Join(dir, "raw.rgba")
	ffmpeg := fakeFFmpeg(t, dir, "printf '%s\\n' \"$@\" > "+shellQuote(argsPath)+"\ncat > "+shellQuote(rawPath)+"\n")
	encoder, err := Start(t.Context(), Options{
		FFmpeg: ffmpeg, Output: filepath.Join(dir, "overlay.mov"), Width: 2, Height: 1,
		FPS: 10, Profile: AlphaProRes, Transparent: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	frame := image.NewRGBA(image.Rect(0, 0, 2, 1))
	frame.SetRGBA(0, 0, color.RGBA{R: 11, A: 255})
	frame.SetRGBA(1, 0, color.RGBA{G: 22, A: 128})
	if err := encoder.WriteFrame(frame); err != nil {
		t.Fatal(err)
	}
	if err := encoder.Close(); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, arg := range []string{"rgba", "yuva444p10le", "prores_ks", "-alpha_bits", "16"} {
		if !strings.Contains(string(args), arg) {
			t.Errorf("FFmpeg args missing %q:\n%s", arg, args)
		}
	}
	want := []byte{11, 0, 0, 255, 0, 22, 0, 128}
	got, err := os.ReadFile(rawPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("raw frame = %v, want %v", got, want)
	}
}

func TestArgsEncodeAlphaHEVCWithVideoToolbox(t *testing.T) {
	options, err := (Options{
		Output: "overlay.mov", Width: 2, Height: 2, FPS: 10,
		Profile: Profile("alpha-hevc"), Transparent: true,
	}).validate()
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(args(options, "bgra"), " ")
	for _, arg := range []string{"hevc_videotoolbox", "bgra", "-alpha_quality 0.8", "-prio_speed 1", "-tag:v hvc1"} {
		if !strings.Contains(got, arg) {
			t.Errorf("HEVC alpha args missing %q:\n%s", arg, got)
		}
	}
}

func TestStartUsesBGRAInputForAlphaHEVC(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	ffmpeg := fakeFFmpeg(t, dir, "printf '%s\\n' \"$@\" > "+shellQuote(argsPath)+"\ncat >/dev/null\n")
	encoder, err := Start(t.Context(), Options{
		FFmpeg: ffmpeg, Output: filepath.Join(dir, "overlay.mov"), Width: 2, Height: 2,
		FPS: 10, Profile: AlphaHEVC, Transparent: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !encoder.UsesBGRA() {
		t.Fatal("HEVC alpha encoder does not report BGRA input")
	}
	if err := encoder.Close(); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(args); !strings.Contains(got, "-f\nrawvideo\n-pix_fmt\nbgra\n") {
		t.Fatalf("HEVC raw input format is not BGRA:\n%s", got)
	}
}

func TestAlphaHEVCRequiresTransparentBackground(t *testing.T) {
	_, err := (Options{
		Output: "overlay.mov", Width: 2, Height: 2, FPS: 10,
		Profile: Profile("alpha-hevc"),
	}).validate()
	if err == nil || !strings.Contains(err.Error(), "transparent") {
		t.Fatalf("validate error = %v, want transparent-background error", err)
	}
}

func TestArgsEncodeAlphaVP9(t *testing.T) {
	options, err := (Options{
		Output: "overlay.webm", Width: 2, Height: 2, FPS: 10,
		Profile: AlphaVP9, Transparent: true,
	}).validate()
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(args(options, "rgba"), " ")
	for _, arg := range []string{"libvpx-vp9", "yuva420p", "-auto-alt-ref 0", "-deadline realtime", "-cpu-used 8", "-threads 16", "-tile-columns 3", "-crf 32", "-b:v 0"} {
		if !strings.Contains(got, arg) {
			t.Errorf("VP9 alpha args missing %q:\n%s", arg, got)
		}
	}
}

func TestArgsApplyCustomFFmpegProfileAndArguments(t *testing.T) {
	options := Options{
		Output: "overlay.webm", Video: "source.mov", Width: 2, Height: 2, FPS: 10,
		Profile: AlphaVP9, Transparent: true,
		FFmpegProfile: FFmpegProfile{
			Input:  []string{"-hwaccel", "videotoolbox"},
			Output: []string{"-crf", "29"},
			Filter: "[0:v][1:v]overlay=shortest=1",
		},
		FFmpegArgs: []string{"-row-mt", "0"},
	}
	got := args(options, "rgba")
	joined := strings.Join(got, " ")
	for _, want := range []string{"-hwaccel videotoolbox -i source.mov", "-filter_complex [0:v][1:v]overlay=shortest=1", "libvpx-vp9", "-crf 32", "-crf 29 -row-mt 0 overlay.webm"} {
		if !strings.Contains(joined, want) {
			t.Errorf("custom FFmpeg profile args missing %q:\n%s", want, joined)
		}
	}
	for _, arg := range got {
		if arg == "[v]" {
			t.Fatal("custom filter should replace the generated [v] mapping")
		}
	}
	if got[len(got)-1] != options.Output {
		t.Fatalf("last FFmpeg arg = %q, want output path %q", got[len(got)-1], options.Output)
	}
}

func TestStartCompositesOverlayOnVideoAtOffset(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	rawPath := filepath.Join(dir, "raw.rgba")
	videoPath := filepath.Join(dir, "source.mov")
	if err := os.WriteFile(videoPath, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	ffmpeg := fakeFFmpeg(t, dir, "printf '%s\\n' \"$@\" > "+shellQuote(argsPath)+"\ncat > "+shellQuote(rawPath)+"\n")
	encoder, err := Start(t.Context(), Options{
		FFmpeg: ffmpeg, Output: filepath.Join(dir, "composed.mov"), Video: videoPath, VideoOffset: 1.25,
		Width: 2, Height: 2, FPS: 10, Profile: BlackH264, Transparent: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	frame := image.NewRGBA(image.Rect(0, 0, 2, 2))
	if err := encoder.WriteFrame(frame); err != nil {
		t.Fatal(err)
	}
	if err := encoder.Close(); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, arg := range []string{videoPath, "-i", "overlay=eof_action=pass:shortest=0:format=auto", "1.25/TB", "0:a?", "-c:a", "copy", "libx264", "yuv420p"} {
		if !strings.Contains(string(args), arg) {
			t.Errorf("composition args missing %q:\n%s", arg, args)
		}
	}
	got, err := os.ReadFile(rawPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 16 {
		t.Fatalf("raw overlay stream has %d bytes, want 16", len(got))
	}
}

func TestCloseIncludesFFmpegFailureOutput(t *testing.T) {
	dir := t.TempDir()
	ffmpeg := fakeFFmpeg(t, dir, "cat >/dev/null\nprintf 'encoder failed\\n' >&2\nexit 7\n")
	encoder, err := Start(t.Context(), Options{
		FFmpeg: ffmpeg, Output: filepath.Join(dir, "overlay.mov"), Width: 1, Height: 1,
		FPS: 10, Profile: AlphaProRes, Transparent: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := encoder.WriteFrame(image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	if err := encoder.Close(); err == nil || !strings.Contains(err.Error(), "encoder failed") {
		t.Fatalf("Close error = %v", err)
	}
}

func TestStartRejectsProfileBackgroundMismatch(t *testing.T) {
	_, err := Start(t.Context(), Options{FFmpeg: "missing-ffmpeg", Output: "out.mov", Width: 1, Height: 1, FPS: 1, Profile: BlackH264, Transparent: true})
	if err == nil || !strings.Contains(err.Error(), "transparent") {
		t.Fatalf("Start error = %v", err)
	}
}

func TestBlackH264RejectsOddDimensions(t *testing.T) {
	for _, size := range []struct {
		name          string
		width, height int
	}{{"odd-width", 1, 2}, {"odd-height", 2, 1}} {
		t.Run(size.name, func(t *testing.T) {
			_, err := (Options{Output: "out.mp4", Width: size.width, Height: size.height, FPS: 1, Profile: BlackH264}).validate()
			if err == nil || !strings.Contains(err.Error(), "even") {
				t.Fatalf("validate error = %v, want even-dimension error", err)
			}
		})
	}
	if _, err := (Options{
		Output: "out.mp4", Width: 1, Height: 2, FPS: 1, Profile: BlackH264,
		FFmpegArgs: []string{"-pix_fmt", "yuv444p"},
	}).validate(); err != nil {
		t.Fatalf("custom pixel format should allow odd dimensions, got %v", err)
	}
}

func TestContextCancellationStopsFFmpeg(t *testing.T) {
	dir := t.TempDir()
	ffmpeg := fakeFFmpeg(t, dir, "exec sleep 30\n")
	ctx, cancel := context.WithCancel(t.Context())
	encoder, err := Start(ctx, Options{
		FFmpeg: ffmpeg, Output: filepath.Join(dir, "overlay.mov"), Width: 1, Height: 1,
		FPS: 10, Profile: AlphaProRes, Transparent: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	done := make(chan error, 1)
	go func() { done <- encoder.Close() }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Close returned nil after cancellation")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not stop canceled FFmpeg promptly")
	}
}

func fakeFFmpeg(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, "ffmpeg")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
