# Usage

English | [Русский](usage.ru.md)

## Requirements

- Go 1.27.3 or newer for building or running from source.
- FFmpeg 8 or compatible, with `prores_ks` for ProRes alpha, `hevc_videotoolbox` on macOS for HEVC alpha, `libvpx-vp9` for VP9 alpha, and `libx264` for H.264 output.
- A usable font. The default is installed Roboto Medium; otherwise the renderer uses macOS Arial Bold, then the Arial system font. `--font PATH` selects a font file.

```sh
go build -o video-overlay-cli ./cmd/video-overlay-cli
./video-overlay-cli --help
./video-overlay-cli render --help
```

The CLI renders frames in Go and streams them to FFmpeg for encoding. With `--video`, FFmpeg also composites the overlay over source footage.

## Export a standalone overlay

Overlay-only export is the default. It renders the activity duration without opening a source video.

```sh
./video-overlay-cli render \
  --activity activity.fit \
  --layout layouts/virb-run.xml \
  --output run-overlay.mov \
  --size 1920x1080 --fps 10 --workers 4 \
  --background transparent --profile alpha-prores
```

`alpha-prores` feeds RGBA frames to ProRes 4444 (`prores_ks`, `yuva444p10le`) with alpha. FFmpeg may report the encoded stream as `yuva444p12le`; inspect the actual stream with `ffprobe`.

For a smaller, lossy transparent MOV on macOS, use `--profile alpha-hevc`. It encodes through VideoToolbox from BGRA with alpha quality 0.8; verify colored text edges on the intended background before using it for a final export:

```sh
./video-overlay-cli render --activity activity.fit --layout layouts/virb-run.xml \
  --output run-overlay-hevc.mov --size 1920x1080 --fps 10 \
  --background transparent --profile alpha-hevc
```

HEVC alpha prioritizes speed through VideoToolbox. `ffprobe` may show `pix_fmt=yuv420p` because it reports the base layer; use an Apple HEVC-alpha-compatible decoder to verify transparency.

For transparent WebM output, use the VP9 profile. It defaults to CRF 32, `yuva420p`, and disables alternate reference frames so the alpha plane is retained. Its speed-priority defaults are `deadline=realtime`, `cpu-used=8`, 16 threads, and 8 tile columns; this can increase file size and reduce compression efficiency compared with slower encoding:

```sh
./video-overlay-cli render --activity activity.fit --layout layouts/virb-run.xml \
  --output run-overlay-vp9.webm --size 1920x1080 --fps 10 \
  --background transparent --profile alpha-vp9
```

VP9 alpha support depends on the decoder and target application; FFmpeg's default VP9 decoder may expose only the color planes. Verify transparency in the application that will consume the overlay.

## FFmpeg profiles and custom options

`--profile` selects the built-in output format (`alpha-prores`, `alpha-hevc`, `alpha-vp9`, or `black-h264`). Separately, `--ffmpeg-profile NAME` loads a named JSON profile with `input` and `output` argument arrays and an optional `filter` override for `--video` composition. By default, profiles are read from `video-overlay-cli/ffmpeg-profiles.json` under the OS user config directory; pass `--ffmpeg-profiles PATH` to select another file. Custom filters should leave the output unlabeled for FFmpeg's automatic mapping; if the filter labels outputs, add the matching `-map` in the profile's `output` arguments. The profile format matches the original Python tool; see [`../examples/ffmpeg-profiles.example.json`](../examples/ffmpeg-profiles.example.json).

For one-off tuning, repeat `--ffmpeg-arg` once per argument token. These output arguments are appended after built-in and named-profile options, allowing you to override encoder defaults. For example, `--ffmpeg-arg=-crf --ffmpeg-arg=28` selects a higher-quality VP9 encode. FFmpeg applies options according to their position relative to the input and output files; custom arguments can also invalidate alpha or container settings if they override the selected profile.

To export an opaque overlay on black, pair `--background black` with `--profile black-h264`:

```sh
./video-overlay-cli render --activity ride.gpx --layout layouts/virb-bike.xml \
  --output bike-overlay.mp4 --size 1920x1080 --fps 10 \
  --background black --profile black-h264
```

For standalone output, `black-h264` requires `--background black`, while alpha profiles require `--background transparent`. The built-in `black-h264` output uses `yuv420p`, so frame dimensions must be even unless an FFmpeg pixel format is supplied explicitly. H.264 does not preserve alpha. With `--video`, a transparent overlay can be composited and encoded as opaque H.264. To inspect output stream properties:

```sh
ffprobe -v error -select_streams v:0 -show_entries stream=codec_name,pix_fmt \
  -of default=noprint_wrappers=1 run-overlay.mov
```

## Optional source-video composition

`--video PATH` asks FFmpeg to composite the generated overlay over that video. It does not read or extract telemetry from the video. The overlay must use a transparent background. `--video-offset SEC` shifts the overlay timeline relative to the beginning of the source video; positive values start it later, negative values start it earlier. The default offset is zero. The source audio is copied when present, and the video is scaled to `--size`.

```sh
./video-overlay-cli render --activity activity.fit --layout layouts/virb-run.xml \
  --output run-composed.mov --video camera.mov --video-offset 12.4 \
  --size 1920x1080 --fps 10 --background transparent --profile black-h264
```

This uses one FFmpeg process and streams frames to stdin in the selected profile's input pixel format. The source video must be readable, and the output path must differ from the activity, layout, and source video paths.

## Layouts, fonts, and icons

Templates use the existing XML format. Component names accept hyphens and underscores; see the [widget matrix](widget-matrix.md) for XML support and internal widgets that are not exposed by the CLI.

Custom icon paths are resolved relative to the XML file, then as explicit filesystem paths. Built-in icons are embedded in the binary. Unsupported XML components report their type and location before FFmpeg starts.

Tile-backed maps use OpenStreetMap tiles by default and show attribution. The first uncached map render needs a network connection. `--tile-url`, `--tile-cache`, and `--tile-attribution` configure another compatible tile source. `--privacy LAT,LON,KM` hides map details in the selected radius and splits route lines at hidden points.

## Errors and exit status

In an interactive terminal, the progress bar shows export FPS, elapsed time, current video time out of the total duration, and ETA. A completed export prints its path, frame count, total elapsed time, and average FPS.

Pressing Ctrl+C stops rendering and lets FFmpeg finalize the partial file so its container remains readable. The CLI prints the partial output path and exits with status 130. Invalid activity, layout, widget, size, profile, font, or video inputs are reported before FFmpeg is started. FFmpeg failures include its diagnostic text. Other errors are printed to stderr and exit with status 2. An existing output is replaced only after static input and XML validation succeeds. Keep the output path separate from all input files.
