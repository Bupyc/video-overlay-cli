package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"image"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"video-overlay-cli/internal/activity"
	"video-overlay-cli/internal/encode"
	"video-overlay-cli/internal/layout"
	"video-overlay-cli/internal/maps"
	"video-overlay-cli/internal/metrics"
	"video-overlay-cli/internal/render"
	"video-overlay-cli/internal/render/widgets"
)

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		_, err := fmt.Fprint(stdout, "Usage: video-overlay-cli <command> [options]\n\nCommands:\n  render  Render an activity overlay\n")
		return err
	}
	if args[0] != "render" {
		return fmt.Errorf("unknown command %q", args[0])
	}
	return runRender(ctx, args[1:], stdout, stderr)
}

func runRender(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("render", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprint(stdout, "Usage: video-overlay-cli render [options]\n\nOptions:\n  --activity PATH       FIT or GPX activity file\n  --layout PATH         XML overlay layout\n  --output PATH         output video path\n  --size WxH            frame size (default 1920x1080)\n  --fps N               output frame rate (default 10)\n  --background MODE     transparent or black (default transparent)\n  --profile NAME        alpha-prores, alpha-hevc, alpha-vp9, or black-h264\n  --ffmpeg-profile NAME named profile from ffmpeg-profiles.json\n  --ffmpeg-profiles PATH profile file (default user config/video-overlay-cli)\n  --ffmpeg-arg ARG      extra output argument; repeat to pass multiple values\n  --workers N           max render workers (default auto)\n  --font PATH           font file\n  --ffmpeg PATH         ffmpeg executable (default ffmpeg)\n  --video PATH          optional source video for composition\n  --video-offset SEC    overlay start offset in source-video seconds (default 0)\n  --privacy LAT,LON,KM  hide map details within this radius\n  --tile-url TEMPLATE  tile URL with {z}, {x}, {y} placeholders\n  --tile-cache PATH     persistent map tile cache (default OS cache dir)\n  --tile-attribution T  attribution text shown on map widgets\n")
	}
	activityPath := flags.String("activity", "", "FIT or GPX activity file")
	layoutPath := flags.String("layout", "", "XML overlay layout")
	outputPath := flags.String("output", "", "output video path")
	size := flags.String("size", "1920x1080", "frame size")
	fps := flags.Float64("fps", 10, "output frame rate")
	background := flags.String("background", "transparent", "transparent or black")
	profile := flags.String("profile", "alpha-prores", "alpha-prores, alpha-hevc, alpha-vp9, or black-h264")
	ffmpegProfileName := flags.String("ffmpeg-profile", "", "named profile from ffmpeg-profiles.json")
	ffmpegProfilesPath := flags.String("ffmpeg-profiles", "", "FFmpeg profile JSON path (default user config/video-overlay-cli)")
	var ffmpegArgs stringListFlag
	flags.Var(&ffmpegArgs, "ffmpeg-arg", "extra output argument for FFmpeg; repeat for each token")
	workers := flags.Int("workers", 0, "render workers (default auto)")
	fontPath := flags.String("font", "", "font file")
	ffmpegPath := flags.String("ffmpeg", "ffmpeg", "ffmpeg executable")
	videoPath := flags.String("video", "", "optional source video for composition")
	videoOffset := flags.Float64("video-offset", 0, "overlay start offset in source-video seconds")
	defaults := maps.DefaultConfig()
	privacyText := flags.String("privacy", "", "hide map details inside LAT,LON,KM radius")
	tileURL := flags.String("tile-url", defaults.TileURL, "tile URL with {z}, {x}, {y} placeholders")
	tileCachePath := flags.String("tile-cache", "", "persistent map tile cache (default OS cache dir)")
	tileAttribution := flags.String("tile-attribution", defaults.Attribution, "attribution text shown on map widgets")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var ffmpegProfile encode.FFmpegProfile
	if *ffmpegProfileName != "" {
		profilePath := *ffmpegProfilesPath
		if profilePath == "" {
			configDir, err := os.UserConfigDir()
			if err != nil {
				return fmt.Errorf("locate user config directory for FFmpeg profiles: %w", err)
			}
			profilePath = filepath.Join(configDir, "video-overlay-cli", "ffmpeg-profiles.json")
		}
		loaded, err := encode.LoadFFmpegProfile(profilePath, *ffmpegProfileName)
		if err != nil {
			return err
		}
		ffmpegProfile = loaded
	} else if *ffmpegProfilesPath != "" {
		return errors.New("--ffmpeg-profiles requires --ffmpeg-profile")
	}
	if *activityPath == "" || *layoutPath == "" || *outputPath == "" {
		return errors.New("--activity, --layout, and --output are required")
	}
	if encode.Profile(*profile) == encode.AlphaVP9 && !strings.EqualFold(filepath.Ext(*outputPath), ".webm") {
		webmPath := strings.TrimSuffix(*outputPath, filepath.Ext(*outputPath)) + ".webm"
		return fmt.Errorf("alpha-vp9 requires a WebM output; use --output %q", webmPath)
	}
	if math.IsNaN(*videoOffset) || math.IsInf(*videoOffset, 0) {
		return fmt.Errorf("video offset must be finite")
	}
	if *videoPath == "" && *videoOffset != 0 {
		return fmt.Errorf("--video-offset requires --video")
	}
	if err := validateOutputPath(*outputPath, *activityPath, *layoutPath, *videoPath); err != nil {
		return err
	}
	if *videoPath != "" {
		file, err := os.Open(*videoPath)
		if err != nil {
			return fmt.Errorf("open source video %q: %w", *videoPath, err)
		}
		info, statErr := file.Stat()
		closeErr := file.Close()
		if statErr != nil {
			return fmt.Errorf("stat source video %q: %w", *videoPath, statErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close source video %q: %w", *videoPath, closeErr)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("source video %q is not a regular file", *videoPath)
		}
	}
	width, height, err := parseSize(*size)
	if err != nil {
		return err
	}
	var mode render.Background
	switch *background {
	case string(render.Transparent):
		mode = render.Transparent
	case string(render.Black):
		mode = render.Black
	default:
		return fmt.Errorf("unknown background %q: use transparent or black", *background)
	}
	if *videoPath != "" && mode != render.Transparent {
		return fmt.Errorf("video composition requires --background transparent")
	}
	if *workers < 0 {
		return fmt.Errorf("worker count must not be negative")
	}
	privacy, err := parsePrivacy(*privacyText)
	if err != nil {
		return err
	}
	if *tileCachePath == "" {
		cacheRoot, err := os.UserCacheDir()
		if err != nil {
			return fmt.Errorf("locate OS cache directory; set --tile-cache explicitly: %w", err)
		}
		*tileCachePath = filepath.Join(cacheRoot, "video-overlay-cli", "tiles")
	}
	mapConfig := defaults
	mapConfig.TileURL = *tileURL
	mapConfig.Attribution = *tileAttribution
	mapConfig.Cache = maps.NewCache(*tileCachePath)
	stageTiming := os.Getenv("OVERLAY_STAGE_TIMING") == "1"
	stageStarted := time.Now()
	markStage := func(name string) {
		if stageTiming {
			fmt.Fprintf(stderr, "stage %-16s %s\n", name, time.Since(stageStarted))
			stageStarted = time.Now()
		}
	}
	act, err := activity.Load(*activityPath)
	markStage("activity.load")
	if err != nil {
		return err
	}
	if err := activity.Resample(act, 10); err != nil {
		return fmt.Errorf("resample activity: %w", err)
	}
	markStage("activity.resample")
	if err := metrics.Process(act, metrics.Profile{}); err != nil {
		return fmt.Errorf("prepare activity metrics: %w", err)
	}
	markStage("metrics.process")
	layoutFile, err := os.Open(*layoutPath)
	if err != nil {
		return fmt.Errorf("open layout %q: %w", *layoutPath, err)
	}
	doc, parseErr := layout.Parse(layoutFile)
	closeErr := layoutFile.Close()
	if parseErr != nil {
		return fmt.Errorf("parse layout %q: %w", *layoutPath, parseErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close layout %q: %w", *layoutPath, closeErr)
	}
	markStage("layout.parse")
	frames, err := frameCount(act.End.Sub(act.Start), *fps)
	if err != nil {
		return err
	}
	workerCount := renderWorkerCount(*workers)
	registry := render.NewRegistry()
	widgets.RegisterDefaults(&registry)
	encodeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	renderers, err := render.NewWorkers(render.Options{
		Width: width, Height: height, Background: mode, FontPath: *fontPath, AssetDir: filepath.Dir(*layoutPath),
		MapContext: encodeCtx, MapConfig: mapConfig, Privacy: privacy,
	}, doc, act, registry, workerCount)
	if err != nil {
		return err
	}
	markStage("render.prepare")
	if err := encodeCtx.Err(); err != nil {
		return err
	}
	ffmpegCtx, stopFFmpeg := context.WithCancel(context.WithoutCancel(encodeCtx))
	defer stopFFmpeg()
	encoder, err := encode.Start(ffmpegCtx, encode.Options{
		FFmpeg: *ffmpegPath, Output: *outputPath, Width: width, Height: height,
		Video: *videoPath, VideoOffset: *videoOffset,
		FPS: *fps, Profile: encode.Profile(*profile), FFmpegProfile: ffmpegProfile, FFmpegArgs: ffmpegArgs,
		Transparent: mode == render.Transparent,
	})
	if err != nil {
		return err
	}
	markStage("ffmpeg.start")
	renderStarted := time.Now()
	renderErr := streamFrames(encodeCtx, encoder, renderers, *fps, frames, width, height, mode == render.Transparent, stderr)
	markStage("streamFrames")
	if renderErr != nil {
		cancel()
	}
	var encodeErr error
	if errors.Is(renderErr, context.Canceled) {
		encodeErr = encoder.ClosePartial()
	} else {
		encodeErr = encoder.Close()
	}
	markStage("ffmpeg.close")
	if errors.Is(renderErr, context.Canceled) && encodeErr == nil {
		if _, err := fmt.Fprintf(stdout, "Interrupted; finalized partial output %s (elapsed %s)\n", *outputPath, time.Since(renderStarted).Round(time.Second)); err != nil {
			return errors.Join(renderErr, err)
		}
		return context.Canceled
	}
	if renderErr != nil || encodeErr != nil {
		return errors.Join(renderErr, encodeErr)
	}
	elapsed := time.Since(renderStarted)
	_, err = fmt.Fprintf(stdout, "Wrote %s (%d frames, elapsed %s, avg %.1f fps)\n", *outputPath, frames, elapsed.Round(time.Second), float64(frames)/elapsed.Seconds())
	return err
}

type stringListFlag []string

func (f *stringListFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *stringListFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

func validateOutputPath(output string, inputs ...string) error {
	outputAbs, err := filepath.Abs(output)
	if err != nil {
		return fmt.Errorf("resolve output path %q: %w", output, err)
	}
	outputInfo, outputStatErr := os.Stat(outputAbs)
	for _, input := range inputs {
		if input == "" {
			continue
		}
		inputAbs, err := filepath.Abs(input)
		if err != nil {
			return fmt.Errorf("resolve input path %q: %w", input, err)
		}
		if filepath.Clean(inputAbs) == filepath.Clean(outputAbs) {
			return fmt.Errorf("output path must differ from input %q", input)
		}
		inputInfo, inputStatErr := os.Stat(inputAbs)
		if outputStatErr == nil && inputStatErr == nil && os.SameFile(outputInfo, inputInfo) {
			return fmt.Errorf("output path must differ from input %q", input)
		}
	}
	return nil
}

type frameResult struct {
	index int
	frame *image.RGBA
	err   error
}

type frameJob struct {
	first int
	count int
}

type frameResultBatch struct {
	frames [4]frameResult
	count  int
}

const frameBatchSize = 4

// shortcut: four batches saturated the measured ProRes pipe; increase if other hardware or encoders starve.
const maxTransparentFramesInFlight = frameBatchSize * 4
const frameWindowSampleInterval = 500 * time.Millisecond
const frameWindowWriterIdleThreshold = 40 * time.Millisecond
const frameWindowBackpressureSamples = 3

func streamWorkerLimit(requested int, transparent bool) int {
	if transparent {
		return min(requested, maxTransparentFramesInFlight/frameBatchSize)
	}
	return requested
}

type transparentFrameWindow struct {
	current             int
	maximum             int
	backpressureSamples int
}

func newTransparentFrameWindow(maximum int) *transparentFrameWindow {
	maximum = min(maximum, maxTransparentFramesInFlight)
	maximum = max(frameBatchSize, maximum/frameBatchSize*frameBatchSize)
	return &transparentFrameWindow{current: frameBatchSize, maximum: maximum}
}

func (w *transparentFrameWindow) observe(writerIdle time.Duration, backlogged bool) int {
	switch {
	case writerIdle >= frameWindowWriterIdleThreshold:
		w.current = min(w.current+frameBatchSize, w.maximum)
		w.backpressureSamples = 0
	case backlogged:
		w.backpressureSamples++
		if w.backpressureSamples >= frameWindowBackpressureSamples {
			w.current = max(frameBatchSize, w.current-frameBatchSize)
			w.backpressureSamples = 0
		}
	default:
		w.backpressureSamples = 0
	}
	return w.current
}

func streamFrames(ctx context.Context, encoder *encode.Encoder, renderers []*render.Renderer, fps float64, frames, width, height int, transparent bool, stderr io.Writer) error {
	workers := streamWorkerLimit(len(renderers), transparent)
	if workers == 0 {
		return fmt.Errorf("no render workers")
	}
	renderers = renderers[:workers]
	progressFile, isFile := stderr.(*os.File)
	showProgress := false
	if isFile {
		if info, err := progressFile.Stat(); err == nil {
			showProgress = info.Mode()&os.ModeCharDevice != 0
		}
	}
	var writtenFrames atomic.Int64
	progressDone := make(chan struct{})
	progressExited := make(chan struct{})
	if showProgress {
		progressStarted := time.Now()
		go func() {
			defer close(progressExited)
			ticker := time.NewTicker(250 * time.Millisecond)
			defer ticker.Stop()
			display := func() {
				completed := int(writtenFrames.Load())
				elapsed := time.Since(progressStarted)
				elapsedSeconds := elapsed.Seconds()
				if elapsedSeconds <= 0 {
					return
				}
				renderFPS := float64(completed) / elapsedSeconds
				remaining := 0.0
				if renderFPS > 0 {
					remaining = float64(frames-completed) / renderFPS
				}
				videoTime := time.Duration(float64(completed) / fps * float64(time.Second))
				totalVideoTime := time.Duration(float64(frames) / fps * float64(time.Second))
				const barWidth = 20
				filled := int(float64(completed) * barWidth / float64(frames))
				fmt.Fprintf(stderr, "\rRender: [%5.1f%%] [%5.1f fps] [%s%s] Elapsed %s | Video %s/%s | ETA %s\x1b[K", 100*float64(completed)/float64(frames), renderFPS, strings.Repeat("=", filled), strings.Repeat("-", barWidth-filled), elapsed.Round(time.Second), formatVideoTime(videoTime), formatVideoTime(totalVideoTime), time.Duration(remaining*float64(time.Second)).Round(time.Second))
			}
			for {
				select {
				case <-ticker.C:
					display()
				case <-progressDone:
					display()
					fmt.Fprintln(stderr)
					return
				}
			}
		}()
	}
	useYUV420P := encoder.UsesYUV420P()
	useBGRA := encoder.UsesBGRA()
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	maxWindow := workers * frameBatchSize
	if transparent {
		maxWindow = min(maxWindow, maxTransparentFramesInFlight)
	}
	window := maxWindow
	var frameWindow *transparentFrameWindow
	if transparent {
		frameWindow = newTransparentFrameWindow(maxWindow)
		window = frameWindow.current
	}
	var windowLimit atomic.Int64
	windowLimit.Store(int64(window))
	framePool := sync.Pool{New: func() any { return image.NewRGBA(image.Rect(0, 0, width, height)) }}
	jobs := make(chan frameJob, workers)
	results := make(chan frameResultBatch, workers)
	slots := make(chan struct{}, maxWindow)
	writeQueue := make(chan *image.RGBA, maxWindow)
	slotWake := make(chan struct{}, 1)
	writerDone := make(chan error, 1)
	var writerIdleNanos atomic.Int64
	go func() {
		var writeErr error
		trackWriterIdle := frameWindow != nil && frameWindow.maximum > frameBatchSize
		hasWrittenFrame := false
		for {
			waitStarted := time.Now()
			frame, ok := <-writeQueue
			if trackWriterIdle && hasWrittenFrame {
				writerIdleNanos.Add(time.Since(waitStarted).Nanoseconds())
			}
			if !ok {
				break
			}
			if writeErr == nil {
				if useYUV420P {
					writeErr = encoder.WriteYUV420P(frame.Pix[:width*height*3/2])
				} else {
					writeErr = encoder.WriteFrame(frame)
				}
				if writeErr != nil {
					cancel()
				} else {
					writtenFrames.Add(1)
					hasWrittenFrame = true
					<-slots
					select {
					case slotWake <- struct{}{}:
					default:
					}
				}
			}
			framePool.Put(frame)
		}
		if showProgress {
			close(progressDone)
			<-progressExited
		}
		writerDone <- writeErr
	}()
	var tuneStop, tuneExited chan struct{}
	if frameWindow != nil && frameWindow.maximum > frameBatchSize {
		tuneStop = make(chan struct{})
		tuneExited = make(chan struct{})
		go func() {
			defer close(tuneExited)
			ticker := time.NewTicker(frameWindowSampleInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					if writtenFrames.Load() == 0 {
						writerIdleNanos.Store(0)
						continue
					}
					current := int(windowLimit.Load())
					writerIdle := time.Duration(writerIdleNanos.Swap(0))
					backlogged := len(writeQueue) > 0 && len(slots) >= current
					if next := frameWindow.observe(writerIdle, backlogged); next != current {
						windowLimit.Store(int64(next))
						select {
						case slotWake <- struct{}{}:
						default:
						}
					}
				case <-tuneStop:
					return
				case <-workCtx.Done():
					return
				}
			}
		}()
	}
	var wait sync.WaitGroup
	for _, renderer := range renderers {
		wait.Add(1)
		go func(renderer *render.Renderer) {
			defer wait.Done()
			var pixelScratch []byte
			if useYUV420P {
				pixelScratch = make([]byte, width*height/2+width)
			}
			for job := range jobs {
				batch := frameResultBatch{}
				for i := 0; i < job.count; i++ {
					index := job.first + i
					at := time.Duration(float64(index) * float64(time.Second) / fps)
					frame := framePool.Get().(*image.RGBA)
					err := renderer.FrameAtContextInto(workCtx, at, frame)
					if err == nil {
						if useYUV420P {
							err = rgbaToYUV420PInPlace(frame, pixelScratch)
						} else if useBGRA {
							err = rgbaToBGRAInPlace(frame)
						}
					}
					batch.frames[i] = frameResult{index: index, frame: frame, err: err}
					batch.count++
				}
				select {
				case results <- batch:
				case <-workCtx.Done():
					for _, result := range batch.frames[:batch.count] {
						framePool.Put(result.frame)
					}
					return
				}
			}
		}(renderer)
	}
	go func() {
		defer close(jobs)
		for first := 0; first < frames; first += frameBatchSize {
			count := min(frameBatchSize, frames-first)
			for i := 0; i < count; i++ {
				for {
					if len(slots) < int(windowLimit.Load()) {
						slots <- struct{}{}
						break
					}
					select {
					case <-slotWake:
					case <-workCtx.Done():
						return
					}
				}
			}
			select {
			case jobs <- frameJob{first: first, count: count}:
			case <-workCtx.Done():
				return
			}
		}
	}()
	go func() {
		wait.Wait()
		close(results)
	}()

	pending := make(map[int]*image.RGBA, maxWindow)
	next := 0
	var firstErr error
	for batch := range results {
		for _, result := range batch.frames[:batch.count] {
			if result.err != nil {
				framePool.Put(result.frame)
				if firstErr == nil {
					firstErr = result.err
					cancel()
				}
				continue
			}
			if firstErr != nil {
				framePool.Put(result.frame)
				continue
			}
			pending[result.index] = result.frame
			for {
				frame, ok := pending[next]
				if !ok {
					break
				}
				delete(pending, next)
				writeQueue <- frame
				next++
			}
		}
	}
	for _, frame := range pending {
		framePool.Put(frame)
	}
	close(writeQueue)
	if tuneStop != nil {
		close(tuneStop)
		<-tuneExited
	}
	if err := <-writerDone; firstErr == nil && err != nil {
		firstErr = err
	}
	if firstErr != nil {
		return firstErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if next != frames {
		return fmt.Errorf("rendered %d of %d frames", next, frames)
	}
	return nil
}

func formatVideoTime(d time.Duration) string {
	seconds := int64(d.Round(time.Second) / time.Second)
	hours := seconds / 3600
	minutes := (seconds / 60) % 60
	seconds %= 60
	return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
}

func frameCount(duration time.Duration, fps float64) (int, error) {
	if fps <= 0 || math.IsNaN(fps) || math.IsInf(fps, 0) {
		return 0, fmt.Errorf("frame rate must be finite and positive")
	}
	count := math.Floor(float64(duration)*fps/float64(time.Second)) + 1
	if count < 1 {
		return 1, nil
	}
	if count > float64(int(^uint(0)>>1)) {
		return 0, fmt.Errorf("activity is too long at %.3f FPS", fps)
	}
	return int(count), nil
}

func renderWorkerCount(requested int) int {
	if requested > 0 {
		return requested
	}
	return min(12, runtime.GOMAXPROCS(0))
}

func parseSize(value string) (int, int, error) {
	widthText, heightText, ok := strings.Cut(strings.ToLower(value), "x")
	if !ok {
		return 0, 0, fmt.Errorf("invalid frame size %q: use WIDTHxHEIGHT", value)
	}
	width, widthErr := strconv.Atoi(widthText)
	height, heightErr := strconv.Atoi(heightText)
	if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 {
		return 0, 0, fmt.Errorf("invalid frame size %q: use positive WIDTHxHEIGHT", value)
	}
	return width, height, nil
}

func parsePrivacy(value string) ([]maps.PrivacyZone, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parts := strings.Split(value, ",")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid privacy zone %q: use LAT,LON,KM", value)
	}
	latitude, latErr := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	longitude, lonErr := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	kilometers, distanceErr := strconv.ParseFloat(strings.TrimSpace(parts[2]), 64)
	if latErr != nil || lonErr != nil || distanceErr != nil || math.IsNaN(latitude) || math.IsInf(latitude, 0) || math.IsNaN(longitude) || math.IsInf(longitude, 0) || math.IsNaN(kilometers) || math.IsInf(kilometers, 0) || math.IsInf(kilometers*1000, 0) || latitude < -90 || latitude > 90 || longitude < -180 || longitude > 180 || kilometers <= 0 {
		return nil, fmt.Errorf("invalid privacy zone %q: use valid LAT,LON and positive KM", value)
	}
	return []maps.PrivacyZone{{Center: maps.Point{Lat: latitude, Lon: longitude}, RadiusMeters: kilometers * 1000}}, nil
}
