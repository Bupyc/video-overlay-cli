# video-overlay-cli

English | [Русский](README.ru.md)

A Go command-line tool for rendering telemetry overlays from FIT or GPX activity files and XML layouts. Export a transparent overlay, an opaque overlay on black, or composite the overlay over an existing video.

The renderer is written in Go; performance benchmarks were run on macOS with Apple Silicon. FFmpeg handles encoding and optional video compositing; telemetry is read from FIT/GPX files, not extracted from video.

## Contents

- [Compared with the original Python project](#compared-with-the-original-python-project)
- [Requirements and build](#requirements-and-build)
- [Quick start](#quick-start)
- [Export mode speed and size](#export-mode-speed-and-size)
- [Progress and cancellation](#progress-and-cancellation)
- [Command reference](#command-reference)
- [XML layouts](#xml-layouts)
- [Metrics and trail smoothing](#metrics-and-trail-smoothing)
- [Maps, tiles, and privacy](#maps-tiles-and-privacy)
- [Documentation and examples](#documentation-and-examples)
- [Development](#development)

## Compared with the original Python project

The comparison below is against the unmodified upstream [`gopro-dashboard-overlay` at `8e26ee5`](https://github.com/time4tea/gopro-dashboard-overlay/tree/8e26ee5b452c6aefa38aa6d08557f15139b61e6c) (2026-05-16). Performance notes use a customized local Python checkout only as a benchmark reference; it is not the baseline for this list.

- **Graceful cancellation:** pressing Ctrl+C lets FFmpeg finalize the partial export, so the output container remains readable.
- **Telemetry input:** Go reads FIT and GPX files. Unlike upstream, it does not extract GPMF telemetry from GoPro video; `--video` is only used for composition.
- **Trail metrics:** Go adds adaptive windows for pace and time-based gradient/vertical speed, FIT-distance-based gradient, and running cadence in steps per minute. The original baseline has no vertical-speed metric or these trail-specific calculations. See [metric details](docs/metrics.md).
- **Parallel work:** Go renders frames concurrently and shares the prepared renderer and caches. Transparent exports adapt active workers to FFmpeg backpressure, up to the `--workers` limit. Metric preparation is still a serial pass, as in the upstream baseline; parallel metric calculation has not been added.
- **Templates:** the XML layout format is retained. Current widget coverage and gaps are listed in the [widget matrix](docs/widget-matrix.md); rendering is not pixel-identical to Python, especially for text and rasterization.
- **Editor:** this CLI has no visual template editor; XML is edited directly. The UI is a separate project and is not implemented yet.
- **Performance evidence:** the current [benchmark](docs/performance.md) uses a customized Python checkout and synthetic GPX, not the unmodified upstream baseline or a full widget-parity layout.

## Requirements and build

- Go 1.27.3 or newer.
- FFmpeg 8 or a compatible build. `alpha-prores` needs `prores_ks`; `alpha-hevc` needs macOS VideoToolbox's `hevc_videotoolbox`; `alpha-vp9` needs `libvpx-vp9`; `black-h264` needs `libx264`.
- A usable font. By default the renderer tries installed Roboto Medium, then macOS Arial fonts. Use `--font` to select another font file.

Build a native executable:

```sh
go build -o video-overlay-cli ./cmd/video-overlay-cli
```

Check the available commands and flags:

```sh
./video-overlay-cli --help
./video-overlay-cli render --help
```

## Quick start

Export a standalone transparent overlay from a running activity:

```sh
./video-overlay-cli render \
  --activity activity.fit \
  --layout layouts/virb-run.xml \
  --output run-overlay.mov \
  --size 1920x1080 --fps 10 \
  --background transparent --profile alpha-prores
```

Export an opaque overlay on black:

```sh
./video-overlay-cli render \
  --activity ride.gpx \
  --layout layouts/virb-bike.xml \
  --output bike-overlay.mp4 \
  --size 1920x1080 --fps 10 \
  --background black --profile black-h264
```

For a compressed transparent MOV on macOS, select `alpha-hevc` instead of the default `alpha-prores`:

```sh
./video-overlay-cli render \
  --activity activity.fit \
  --layout layouts/virb-run.xml \
  --output run-overlay-hevc.mov \
  --size 1920x1080 --fps 10 \
  --background transparent --profile alpha-hevc
```

For transparent WebM output, use `alpha-vp9`:

```sh
./video-overlay-cli render \
  --activity activity.fit \
  --layout layouts/virb-run.xml \
  --output run-overlay-vp9.webm \
  --size 1920x1080 --fps 10 \
  --background transparent --profile alpha-vp9
```

VP9 alpha uses speed-priority settings by default (`realtime`, `cpu-used=8`, 16 threads, and 8 tile columns). This can increase output size and reduce compression efficiency. HEVC alpha also prioritizes speed through VideoToolbox. Use `--ffmpeg-arg` to override encoder options when you prefer a different speed/size/quality tradeoff.

## Export mode speed and size

Approximate results on an M4 Pro for the same 179.9-second trail sample, `layouts/virb-run.xml`, 1920×1080, 10 fps, and 8 workers. Export speed is rendered frames per wall-clock second; the resulting video still plays at 10 fps. One-hour file sizes are linearly estimated from each sample's actual output size.

| Profile | Output | Export speed | Approx. size per video hour | Use it when |
|---|---|---:|---:|---|
| `alpha-prores` | Transparent ProRes 4444 MOV | 110 fps | 8.0 GB | You need a high-quality editing/master file and can afford the storage. |
| `alpha-hevc` | Transparent HEVC MOV | 179 fps | 265 MB | You need alpha in MOV with a smaller file and faster export than ProRes. Check HEVC Alpha support in the target app. |
| `alpha-vp9` | Transparent VP9 WebM | 283 fps | 71 MB | You want the fastest compact transparent output and the target app supports VP9 alpha in WebM. |
| `black-h264` | Opaque H.264 MP4 on black | 706 fps | 20 MB | A black background is acceptable and alpha is not needed. |

For transparent output, choose ProRes for editing quality, HEVC for MOV compatibility, or VP9 for speed and smaller files when the destination supports it. `black-h264` is the fastest/smallest measured mode, but it does not preserve transparency. The estimates are content-dependent; a composite that includes `--video` also includes the source footage and has a different size.

These measurements use the checked-in synthetic trail sample and this Mac/FFmpeg build; real activities, layouts, worker counts, and encoder versions can change both figures. See [performance notes](docs/performance.md) for the exact method and sample sizes.

## Progress and cancellation

In an interactive terminal, the progress bar shows export FPS, elapsed time, the current video time and total duration, and the estimated time remaining. When rendering finishes, the CLI prints total elapsed time and average FPS. Pressing Ctrl+C stops at a frame boundary and asks FFmpeg to finalize the partial output; the resulting video remains readable.

Composite an overlay over an existing video. This uses a transparent render internally and produces a regular H.264 video; telemetry is still read from the FIT/GPX file, not from the video.

```sh
./video-overlay-cli render \
  --activity activity.fit \
  --layout layouts/virb-run.xml \
  --output run-composite.mp4 \
  --video camera.mov --video-offset 12.4 \
  --size 1920x1080 --fps 10 \
  --background transparent --profile black-h264
```

When composing, source audio is copied when present. A positive `--video-offset` starts the overlay later; a negative value starts it earlier.

## Command reference

The required inputs are an activity file, an XML layout, and an output path.

| Option | Default | Description |
|---|---|---|
| `--activity PATH` | — | FIT or GPX activity data. |
| `--layout PATH` | — | XML overlay layout. |
| `--output PATH` | — | Output video path. Keep it separate from all input paths. |
| `--size WxH` | `1920x1080` | Output frame dimensions. |
| `--fps N` | `10` | Output frame rate. |
| `--background MODE` | `transparent` | `transparent` or `black`. |
| `--profile NAME` | `alpha-prores` | `alpha-prores`, `alpha-hevc`, `alpha-vp9`, or `black-h264` (built-in `yuv420p` output requires even frame dimensions). HEVC alpha requires macOS VideoToolbox; VP9 alpha requires `libvpx-vp9`. |
| `--ffmpeg-profile NAME` | — | Load named user settings from `ffmpeg-profiles.json`. |
| `--ffmpeg-profiles PATH` | `video-overlay-cli/ffmpeg-profiles.json` in user config | Path to the JSON profile file; requires `--ffmpeg-profile`. |
| `--ffmpeg-arg ARG` | — | Extra output argument; repeat once per FFmpeg argument token. Applied after the selected encoder and named profile settings. |
| `--workers N` | automatic | Maximum number of frame-rendering workers; transparent mode adapts active workers up to this limit. |
| `--font PATH` | system fallback | Font file used by text widgets. |
| `--ffmpeg PATH` | `ffmpeg` | FFmpeg executable. |
| `--video PATH` | — | Optional source video to composite under the overlay. |
| `--video-offset SEC` | `0` | Timeline offset; requires `--video`. |
| `--privacy LAT,LON,KM` | — | Hide map details within a radius around a location. |
| `--tile-url TEMPLATE` | OpenStreetMap | Tile URL containing `{z}`, `{x}`, and `{y}` placeholders. |
| `--tile-cache PATH` | OS user cache | Persistent cache directory for map tiles. |
| `--tile-attribution TEXT` | OpenStreetMap attribution | Attribution shown on tile-backed map widgets. |

Choose a matching background and encoding profile:

| Use | Background | Profile | Result |
|---|---|---|---|
| Standalone overlay with alpha | `transparent` | `alpha-prores` | ProRes 4444 video with alpha. |
| Compressed standalone overlay with alpha on macOS | `transparent` | `alpha-hevc` | HEVC video with alpha via VideoToolbox; lossy. |
| Compressed standalone overlay with alpha | `transparent` | `alpha-vp9` | VP9 video with alpha in WebM; lossy and requires a compatible decoder. |
| Standalone overlay on black | `black` | `black-h264` | Opaque H.264 overlay. |
| Composite over `--video` | `transparent` | `black-h264` | Opaque H.264 composition; no alpha in the final video. |

The CLI validates the combinations. `black-h264` cannot preserve transparency in a standalone overlay. HEVC defaults to alpha quality 0.8; VP9 defaults to CRF 32 with alpha. Both are lossy; use `alpha-prores` for a high-quality master. FFmpeg's default VP9 decoder may omit alpha, so confirm WebM alpha support in the target application. For output details, inspect the file with `ffprobe`:

To save custom FFmpeg settings, create a JSON profile file and select a profile by name. The default path is the OS user config directory under `video-overlay-cli/ffmpeg-profiles.json`; `--ffmpeg-profiles` selects another file. Each entry requires `input` and `output` arrays and may include a custom `filter` for video composition. See [`examples/ffmpeg-profiles.example.json`](examples/ffmpeg-profiles.example.json). Use repeated `--ffmpeg-arg` values for one-off encoder options, such as `--ffmpeg-arg=-crf --ffmpeg-arg=28`; these are appended last, so they override the same FFmpeg setting from the selected profile.

```sh
ffprobe -v error -select_streams v:0 \
  -show_entries stream=codec_name,width,height,avg_frame_rate,pix_fmt,nb_frames \
  -of default=noprint_wrappers=1 run-overlay.mov
```

## XML layouts

Layouts use a `<layout>` root. `<composite>` groups child widgets and positions them relative to the output canvas; `<frame>` creates a framed group. A widget is written as `<component type="…">`. Component names accept hyphens or underscores.

```xml
<layout>
  <composite x="40" y="40">
    <component type="text" y="0" size="15" rgb="90,215,255,235">SPEED</component>
    <component type="metric" y="24" metric="speed" units="km/h"
               format=".1f" size="34" rgb="255,255,255,255"/>
  </composite>
</layout>
```

Colors use `red,green,blue,alpha` byte values from 0 to 255. Widget-specific attributes control values, units, labels, size, alignment, outlines, charts, gauges, and map appearance. See the supplied layouts and gallery files for complete examples:

- `layouts/virb-run.xml` and `layouts/virb-bike.xml` — running and cycling layouts.
- `examples/widget-gallery.xml` — core text, metric, chart, map, and gauge widgets.
- `examples/map-gallery.xml` — tile-backed map widgets.
- [Reference layouts](examples/reference-layouts/README.md) ([Русский](examples/reference-layouts/README.ru.md)) — full layouts copied from the original project, including motorcycle, power, route, and vector-gauge examples.

Custom icon paths are resolved relative to the layout file, then as explicit filesystem paths. Built-in icons are embedded in the executable. An unsupported widget type is reported before FFmpeg starts. The [widget matrix](docs/widget-matrix.md) lists supported XML components and internal widgets that are not exposed by the CLI.

## Metrics and trail smoothing

Common `metric` names include `speed`, `pace`, `dist`, `odo`, `alt`, `gradient`, `vertical_speed`, `hr`, `cadence`, `running_cadence`, `power`, `temp`, `time`, `azi`, `cog`, and `gps-lock`. Other fields include raw and smoothed speed, GPS accuracy/position, respiration, and gear data.

FIT distance is used for gradient only when its full series is valid and nondecreasing. Pace switches from a 3-second to a 30-second speed estimate at paces slower than 20 min/km; time-based gradient and derived vertical speed can use windows up to 120 seconds. Pace is unavailable at speeds of 0.05 m/s or less.

See [metric semantics](docs/metrics.md) for smoothing, units, and calculation details.

## Maps, tiles, and privacy

Route-only widgets can render from activity data without map tiles. Tile-backed widgets use OpenStreetMap by default, show attribution, and cache downloaded tiles in the OS user cache. The first render for an uncached area needs a network connection.

Use `--tile-url`, `--tile-cache`, and `--tile-attribution` to configure another compatible tile source. `--privacy LAT,LON,KM` suppresses map detail inside the selected radius; route lines are split at hidden points.

## Documentation and examples

- [Usage details](docs/usage.md) ([Русский](docs/usage.ru.md)) — output modes, composition, fonts, icons, and errors.
- [Metric semantics](docs/metrics.md) ([Русский](docs/metrics.ru.md)) — derived values and adaptive windows.
- [Widget matrix](docs/widget-matrix.md) ([Русский](docs/widget-matrix.ru.md)) — XML widget coverage and limitations.
- [Performance notes](docs/performance.md) ([Русский](docs/performance.ru.md)) — benchmark method, measured results, and limitations.
- [Notices and attributions](NOTICE.md) ([Русский](NOTICE.ru.md)).

## Development

```sh
go build ./cmd/video-overlay-cli
go test ./...
```

The executable accepts one command, `render`. Templates are XML files; the CLI does not include a visual template editor.

To publish downloadable binaries, push a version tag such as `v1.0.0`. After tests and builds pass, GitHub Actions creates or updates a Release with Linux x64, Windows x64, and macOS Apple Silicon binaries and generated release notes:

```sh
git tag v1.0.0
git push origin v1.0.0
```
