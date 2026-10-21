# Performance comparison

English | [Русский](performance.ru.md)

This page reports Go export-profile sizes and end-to-end comparisons with the original upstream Python `gopro-overlay`. Benchmark inputs and the common-widget XML are checked in under `examples/`.

## Go export profiles

On 2026-10-21, each built-in CLI profile rendered the same 179.9-second synthetic slow-trail GPX with `layouts/virb-run.xml`, 1920×1080, 10 FPS, and eight workers on the M4 Pro. The CLI wrote 1,799 frames per run. FPS is export throughput; output playback remains 10 FPS. One-hour sizes below are scaled linearly from each actual sample, using decimal MB/GB.

This profile sweep predates the frame-count fix below and omitted the final sample. The current CLI writes 1,800 frames for this activity.

| Profile and output | Export FPS | Sample size | Estimated size per video hour |
|---|---:|---:|---:|
| `alpha-prores` — transparent ProRes 4444 MOV | 109.8 | 399.6 MB | 8.00 GB |
| `alpha-hevc` — transparent HEVC MOV, alpha quality 0.8 | 178.6 | 13.2 MB | 265 MB |
| `alpha-vp9` — transparent VP9 WebM, default CRF 32/speed settings | 282.7 | 3.6 MB | 71 MB |
| `black-h264` — opaque H.264 MP4 on black | 705.9 | 1.0 MB | 20 MB |

The black H.264 figure is for a standalone overlay on black; a composite also contains the source video. The VP9 CRF 28 custom-profile sample was 2.7 MiB over the same interval, or about 56 MB per video hour, but its export FPS was not measured in this comparison.

## Black H.264: original Python vs Go

On the same 179.91-second slow-trail GPX, both exporters produced 1,800 frames at 1920×1080 and 10 FPS using `libx264`, `veryfast`, and `yuv420p`. Python is the two-run original baseline. Each Go worker setting was run twice on the M4 Pro; `ffprobe` confirmed 1,800 frames and 180.0 seconds in the outputs.

The Python source was upstream commit `8e26ee5b452c6aefa38aa6d08557f15139b61e6c` (Python 3.14.5). It rendered frames sequentially in one process; FFmpeg ran separately and could use its own encoder threads.

| Renderer | Wall / export FPS | App CPU (average cores; CPU-s) | FFmpeg CPU (average cores; CPU-s) | App peak RSS | FFmpeg peak RSS |
|---|---:|---:|---:|---:|---:|
| Original upstream Python 0.134.0, sequential | 6.37 s / 283 FPS | 0.74 / 4.72 | 2.27 / 14.44 | 119 MiB | 578 MiB |
| Go CLI, 2 workers | 2.81 s / 690 FPS | 1.45 / 4.09 | 3.97 / 11.16 | 212 MiB | 504 MiB |
| Go CLI, 8 workers | 2.56 s / 735 FPS | 1.58 / 4.04 | 4.25 / 10.88 | 592 MiB | 502 MiB |

CPU cores are average core-equivalents over the whole command; CPU-s is each process's total CPU time. Peak RSS is the mean of two sampled process peaks. With two Go workers, app peak RSS fell 64% versus eight workers, while export throughput was about 6% lower. In black mode, set `--workers` manually; adaptive frame-window control currently applies to transparent exports. Go sends YUV420P from its workers, while Python sends RGBA and leaves conversion to FFmpeg. The Python original does not support the trail `pace` metric, so a temporary common copy of `layouts/virb-run.xml` used `speed` in that widget for both runs. Whole-command timing includes startup and FFmpeg shutdown, so its FPS is not directly comparable to the CLI-reported FPS in the profile table above.

## Go vs Python: transparent ProRes 4444

Both exporters rendered the same synthetic 3-minute activities as 1,800 frames at 1920×1080 and 10 FPS with transparent ProRes 4444 on the M4 Pro. Current Go runs used four render workers with an adaptive frame window of 4–16 frames; original Python rendered sequentially in one process. FFmpeg was free to use its own threads in both pipelines. Times and process resource figures are means of two runs.

| Activity | Renderer | Wall / export FPS | App CPU (average cores; CPU-s) | FFmpeg CPU (average cores; CPU-s) | App peak RSS | FFmpeg peak RSS |
|---|---|---:|---:|---:|---:|---:|
| Normal | Go, 4 workers (adaptive window) | 15.02 s / 121.7 FPS | 0.54 / 8.16 | 9.28 / 139.38 | 194 MiB | 968 MiB |
| Normal | Python, sequential | 15.81 s / 113.9 FPS | 0.42 / 6.71 | 9.38 / 148.38 | 120 MiB | 971 MiB |
| Slow trail | Go, 4 workers (adaptive window) | 14.86 s / 122.7 FPS | 0.54 / 8.06 | 9.36 / 139.11 | 194 MiB | 868 MiB |
| Slow trail | Python, sequential | 15.87 s / 113.4 FPS | 0.42 / 6.69 | 9.32 / 147.98 | 119 MiB | 884 MiB |

Average CPU cores are each process's CPU time divided by whole-command wall time. RSS is the mean of peak resident memory sampled separately for the app and FFmpeg during two runs; do not add the RSS columns because processes can share mapped pages. FPS includes startup and FFmpeg shutdown, so it is not directly comparable to the CLI-reported FPS in the profile table above.

For transparent ProRes, FFmpeg used about 9.3 cores while the Go/Python renderer process used about 0.5 cores. This points to the FFmpeg encoder and backpressure through its input pipe as the throughput limit: the renderers spend most of the wall time waiting for FFmpeg to accept and encode frames. This is process-level evidence, not a direct measurement of time blocked inside `write`. With the current adaptive frame window, Go peak RSS was about 194 MiB at 122 FPS. The Go app still used more RSS and CPU time than Python; FFmpeg remained the dominant CPU consumer. The black-mode Go worker sweep above shows that lowering workers also reduces app RSS there, with a small throughput trade-off.

The 90-second metric frame looked close between HEVC and VP9 after compositing on gray; this is not a full-motion quality review. The VP9 alpha plane ranged from 0 to 255 when decoded with `libvpx-vp9`; the default VP9 decoder on this machine did not expose alpha. HEVC alpha decoded with alpha values from 0 to 255 through AVFoundation. VP9/WebM therefore works in compatible decoders, but the target editor must support WebM alpha.

<details>
<summary>Benchmark setup, detailed measurements, reproduction, and limits</summary>

### Benchmark setup

- Machine: Apple M4 Pro, 24 GB RAM, macOS 27.0; FFmpeg 8.0.
- Go: 1.26.5. Python reference: upstream `gopro-overlay` 0.134.0 at commit `8e26ee5b452c6aefa38aa6d08557f15139b61e6c`, Python 3.14.5.
- The Go CLI runs `metrics.Process` before preparing widgets, including the default short/long/extra-long windows.
- The transparent comparison uses `examples/performance.xml`, the same Arial font, 1920×1080, 10 FPS, and ProRes 4444 (`prores_ks`, profile 4, `yuva444p10le`, 16 alpha bits); Go uses four render workers and an adaptive 4–16-frame window, while Python's renderer is sequential. Python's profile adds `-r 10` after its default `-r 30`.
- The black comparison uses a common copy of `layouts/virb-run.xml`, `libx264`/`veryfast`, and `yuv420p`; Python's unsupported `pace` widget is changed to `speed` in that copy for both runs.
- `examples/benchmark-normal.gpx` and `examples/benchmark-slow-trail.gpx` are synthetic, 181-point activities lasting 179.91 seconds. The normal profile averages 3.4 m/s with 9 m elevation gain; slow-trail averages 1.4 m/s with 30 m gain. Both include speed, heart rate, cadence, and power. They avoid personal activity data and network-backed maps.
- Each activity and renderer ran twice. The black Go run used two and eight workers; transparent Go used four. Transparent-run order was alternated; black runs used Go then Python. The OS file cache was not flushed.
- Per-process CPU time and RSS were sampled with macOS `ps -Ao pid=,ppid=,time=,rss=,comm=` every 200 ms. Average core-equivalents are process CPU time divided by whole-command wall time; peak RSS values are means of sampled maxima from two runs.

The original `layouts/virb-bike.xml` was also inspected. It is not used for the timing table: Python draws the whole activity profile in its chart while Go draws the chart history up to the current frame. The common layout keeps the timed widget work comparable; it does not establish full visual parity.

### Detailed Go–Python measurements

All four transparent outputs were verified with `ffprobe` as 1800 frames, 180.0 seconds, 1920×1080, 10 FPS, ProRes 4444 (`yuva444p12le`). The process tables above report mean values from two runs per renderer and activity.

### Reproduce the benchmark

Build Go before timing so compilation is excluded:

```sh
mkdir -p tmp
go build -o tmp/video-overlay-cli ./cmd/video-overlay-cli
```

Set `GOPRO_OVERLAY_DIR` to the original upstream checkout at commit `8e26ee5b452c6aefa38aa6d08557f15139b61e6c`, then install it into a temporary environment:

```sh
export GOPRO_OVERLAY_DIR=../gopro-dashboard-overlay
python3.14 -m venv tmp/gopro-overlay-bench
tmp/gopro-overlay-bench/bin/pip install -e "$GOPRO_OVERLAY_DIR"
mkdir -p tmp/gopro-overlay-bench/config tmp/gopro-overlay-bench/cache
```

Set `BENCH_FONT` to an installed Arial font file so both renderers use the same font.

Create `tmp/gopro-overlay-bench/config/ffmpeg-profiles.json`:

```json
{
  "bench-alpha": {
    "input": [],
    "output": ["-vcodec", "prores_ks", "-profile:v", "4", "-pix_fmt", "yuva444p10le", "-alpha_bits", "16", "-r", "10"]
  }
}
```

Run each activity through both commands, replacing `normal` with `slow-trail` for the second profile. Leave Python's renderer on its sequential default; use eight Go workers:

```sh
/usr/bin/time -lp tmp/video-overlay-cli render \
  --activity examples/benchmark-normal.gpx --layout examples/performance.xml \
  --output tmp/go-normal.mov --size 1920x1080 --fps 10 --workers 4 \
  --font "$BENCH_FONT" \
  --background transparent --profile alpha-prores

/usr/bin/time -lp tmp/gopro-overlay-bench/bin/gopro-dashboard.py \
  --use-gpx-only --gpx examples/benchmark-normal.gpx \
  --layout xml --layout-xml examples/performance.xml \
  --profile bench-alpha --config-dir tmp/gopro-overlay-bench/config \
  --cache-dir tmp/gopro-overlay-bench/cache --overlay-size 1920x1080 \
  --bg 0,0,0,0 --font "$BENCH_FONT" \
  tmp/python-normal.mov
```

`/usr/bin/time -lp` reports aggregate command usage; the app/FFmpeg split in the tables above comes from separate per-PID `ps` sampling.

The Python package has `--print-timings` and `--profiler`. The Go CLI prints total render time and average FPS; set `OVERLAY_STAGE_TIMING=1` to print durations for activity loading, resampling, metric preparation, layout parsing, renderer preparation, FFmpeg startup, frame streaming, and FFmpeg shutdown. Frame streaming combines Go drawing, conversion, and writes to FFmpeg, so its duration cannot be attributed to Go and FFmpeg separately. A Go CPU profile identified repeated outlined-text rasterization as the dominant renderer cost. The current renderer shares rasterized text sprites among frame workers and reuses outlined glyph data. With a draining FFmpeg test sink, Go drawing fell from 10.46 s to 4.25 s on the 1800-frame bike layout; the sink run excludes video encoding and is not part of the end-to-end table.

### Limits

- The activity files are synthetic and only three minutes long; the 120-second slow-metric window is represented, but longer-route scaling is not measured.
- The results compare `gopro-overlay` only. Go's full template parity was not benchmarked.
- The OS file cache was not flushed, so these are not cold-cache measurements. The 200 ms process sampling can miss brief RSS peaks and add slight timing overhead.
- Go stage timing separates loading and preparation, but does not split Go drawing, conversion, pipe writes, and FFmpeg encoding.
</details>
