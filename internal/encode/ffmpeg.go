package encode

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

type Encoder struct {
	cmd              *exec.Cmd
	stdin            io.WriteCloser
	writer           *bufio.Writer
	stderr           bytes.Buffer
	width            int
	height           int
	inputPixelFormat string
	videoInput       bool
	mu               sync.Mutex
	closed           bool
	closeErr         error
}

func Start(ctx context.Context, opts Options) (*Encoder, error) {
	var err error
	opts, err = opts.validate()
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	inputPixelFormat := "rgba"
	if opts.Profile == AlphaHEVC {
		inputPixelFormat = "bgra"
	} else if opts.Profile == BlackH264 && opts.Video == "" && opts.Width%2 == 0 && opts.Height%2 == 0 {
		inputPixelFormat = "yuv420p"
	}
	cmd := exec.CommandContext(ctx, opts.FFmpeg, args(opts, inputPixelFormat)...)
	isolateInterrupt(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open ffmpeg input: %w", err)
	}
	encoder := &Encoder{cmd: cmd, stdin: stdin, writer: bufio.NewWriterSize(stdin, 64*1024), width: opts.Width, height: opts.Height, inputPixelFormat: inputPixelFormat, videoInput: opts.Video != ""}
	cmd.Stdout = io.Discard
	cmd.Stderr = &encoder.stderr
	if err := cmd.Start(); err != nil {
		stdin.Close()
		return nil, fmt.Errorf("start ffmpeg %q: %w", opts.FFmpeg, err)
	}
	return encoder, nil
}

func args(opts Options, inputPixelFormat string) []string {
	args := []string{
		"-hide_banner", "-loglevel", "error", "-y",
	}
	if opts.Video != "" {
		args = append(args, opts.FFmpegProfile.Input...)
		args = append(args, "-i", opts.Video)
	} else {
		args = append(args, opts.FFmpegProfile.Input...)
	}
	args = append(args,
		"-f", "rawvideo", "-pix_fmt", inputPixelFormat,
		"-video_size", fmt.Sprintf("%dx%d", opts.Width, opts.Height),
		"-framerate", strconv.FormatFloat(opts.FPS, 'f', -1, 64), "-i", "-",
	)
	if opts.Video != "" {
		offset := "+" + strconv.FormatFloat(opts.VideoOffset, 'f', -1, 64)
		if opts.VideoOffset < 0 {
			offset = strconv.FormatFloat(opts.VideoOffset, 'f', -1, 64)
		}
		filter := fmt.Sprintf("[0:v]scale=%d:%d,setsar=1,setpts=PTS-STARTPTS[base];[1:v]setpts=PTS-STARTPTS%s/TB[overlay];[base][overlay]overlay=eof_action=pass:shortest=0:format=auto[v]", opts.Width, opts.Height, offset)
		if opts.FFmpegProfile.Filter != "" {
			filter = opts.FFmpegProfile.Filter
		}
		args = append(args, "-filter_complex", filter)
		if opts.FFmpegProfile.Filter == "" {
			args = append(args, "-map", "[v]")
		}
		args = append(args, "-map", "0:a?", "-c:a", "copy")
	} else {
		args = append(args, "-an")
	}
	if opts.Profile == AlphaProRes {
		args = append(args, "-c:v", "prores_ks", "-profile:v", "4", "-pix_fmt", "yuva444p10le", "-alpha_bits", "16")
	} else if opts.Profile == AlphaHEVC {
		args = append(args, "-c:v", "hevc_videotoolbox", "-pix_fmt", "bgra", "-alpha_quality", "0.8", "-prio_speed", "1", "-tag:v", "hvc1")
	} else if opts.Profile == AlphaVP9 {
		args = append(args, "-c:v", "libvpx-vp9", "-pix_fmt", "yuva420p", "-auto-alt-ref", "0", "-deadline", "realtime", "-cpu-used", "8", "-row-mt", "1", "-threads", "16", "-tile-columns", "3", "-crf", "32", "-b:v", "0")
	} else {
		args = append(args, "-c:v", "libx264", "-preset", "veryfast", "-pix_fmt", "yuv420p")
	}
	args = append(args, opts.FFmpegProfile.Output...)
	args = append(args, opts.FFmpegArgs...)
	return append(args, opts.Output)
}

func (e *Encoder) UsesYUV420P() bool {
	return e.inputPixelFormat == "yuv420p"
}

func (e *Encoder) UsesBGRA() bool {
	return e.inputPixelFormat == "bgra"
}

func (e *Encoder) WriteFrame(frame *image.RGBA) error {
	if frame == nil {
		return fmt.Errorf("frame is nil")
	}
	if e.UsesYUV420P() {
		return fmt.Errorf("encoder expects YUV420P frames")
	}
	if frame.Bounds().Dx() != e.width || frame.Bounds().Dy() != e.height {
		return fmt.Errorf("frame size %dx%d does not match encoder size %dx%d", frame.Bounds().Dx(), frame.Bounds().Dy(), e.width, e.height)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return fmt.Errorf("encoder is closed")
	}
	rowBytes := e.width * 4
	if frame.Stride == rowBytes {
		start := frame.PixOffset(frame.Rect.Min.X, frame.Rect.Min.Y)
		if err := writeAll(e.writer, frame.Pix[start:start+rowBytes*e.height]); err != nil {
			return fmt.Errorf("write raw frame to ffmpeg: %w", err)
		}
		return nil
	}
	for y := frame.Bounds().Min.Y; y < frame.Bounds().Max.Y; y++ {
		offset := frame.PixOffset(frame.Bounds().Min.X, y)
		if err := writeAll(e.writer, frame.Pix[offset:offset+rowBytes]); err != nil {
			return fmt.Errorf("write raw frame to ffmpeg: %w", err)
		}
	}
	return nil
}

func (e *Encoder) WriteYUV420P(frame []byte) error {
	if !e.UsesYUV420P() {
		return fmt.Errorf("encoder input is not YUV420P")
	}
	want := e.width * e.height * 3 / 2
	if len(frame) != want {
		return fmt.Errorf("YUV420P frame has %d bytes, want %d", len(frame), want)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return fmt.Errorf("encoder is closed")
	}
	if err := writeAll(e.writer, frame); err != nil {
		return fmt.Errorf("write raw YUV420P frame to ffmpeg: %w", err)
	}
	return nil
}

func (e *Encoder) Close() error {
	return e.close(false)
}

func (e *Encoder) ClosePartial() error {
	return e.close(true)
}

func (e *Encoder) close(partial bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return e.closeErr
	}
	e.closed = true
	flushErr := e.writer.Flush()
	closeErr := e.stdin.Close()
	var interruptErr error
	interrupted := false
	if partial && e.videoInput {
		interruptErr = interruptFFmpeg(e.cmd)
		interrupted = interruptErr == nil
		if errors.Is(interruptErr, os.ErrProcessDone) {
			interruptErr = nil
		}
	}
	waitErr := e.cmd.Wait()
	if interrupted && expectedInterruptExit(waitErr) {
		waitErr = nil
	}
	if waitErr != nil {
		message := strings.TrimSpace(e.stderr.String())
		if message != "" {
			e.closeErr = fmt.Errorf("ffmpeg failed: %w: %s", waitErr, message)
		} else {
			e.closeErr = fmt.Errorf("ffmpeg failed: %w", waitErr)
		}
	} else if flushErr != nil {
		e.closeErr = fmt.Errorf("flush raw frame input: %w", flushErr)
	} else if closeErr != nil {
		e.closeErr = fmt.Errorf("close ffmpeg input: %w", closeErr)
	} else if interruptErr != nil {
		e.closeErr = fmt.Errorf("interrupt ffmpeg after closing partial input: %w", interruptErr)
	}
	return e.closeErr
}

func expectedInterruptExit(err error) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && (exitErr.ExitCode() == -1 || exitErr.ExitCode() == 255)
}

func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := w.Write(data)
		data = data[written:]
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}
