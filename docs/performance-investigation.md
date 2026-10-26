# Go renderer performance investigation

## Goal and scope

Reduce Go-side CPU use and peak memory while preserving the rendered image. Keep Go rendering separate from actual FFmpeg encoding. Preserve the user's layout and unrelated working-tree edits. Do not revisit outline/stroke algorithms: the earlier raster-dilation attempt visibly diverged and was reverted.

## Current benchmark

Full `i195882617.fit`, `layouts/virb-run.xml` (26 widgets), 1920×1080, 10 fps, black/YUV420P, 8 workers. A draining fake FFmpeg (`cat >/dev/null`) isolates Go rendering and pipe delivery; it is not an encode benchmark. The run renders 83,080 frames.

| State | Profile wall time | Go CPU samples | Allocated | Unprofiled wall / peak RSS |
|---|---:|---:|---:|---:|
| After resample change, before marker atlases | 78.80 s | 153.06 CPU-s | 8.80 GB | 79.17 s / 840.9 MB |
| After chart and circuit-map marker atlases | 74.61 s | 144.15 CPU-s | 3.24 GB | 73.58 s / 834.4 MB |

Marker atlases reduced allocation by 63%, profile CPU by 5.8%, and profiled wall time by 5.3%. Peak RSS fell by about 6.5 MB (0.8%), a small change against an ~835 MB peak. The earlier resample change removed about 525 MB of allocations versus its own baseline, with negligible speed change.

The marker image uses an 8×8 fractional-position atlas shared by the widget's worker renderers. The chart and circuit-map marker shapes/styles are rasterized once during renderer setup. The vector path remains for scaled contexts and large markers. Comparing the first transparent frame before and after changed 78 pixels out of 2,073,600 (0.0038%); the maximum channel difference was 11/255, localized to the markers. A test checks all 64 atlas phases against vector rendering on transparent and black backgrounds.

## Earlier profile evidence

Earlier 75-second CPU profile: `runtime.pthread_cond_signal` 30.8%, `syscall.rawsyscalln` 28.7%, `runtime.memclrNoHeapPointers` 9.2%, `runtime.madvise` 8.4%, YUV conversion 7.4%, `runtime.kevent` 5.7%, and `runtime.memmove` 4.0%. The drain sink includes pipe writes/syscalls, so these numbers are not an FFmpeg encode profile.

Largest allocation path is dynamic chart drawing: `chartFactory` is 2.08 GB cumulative (filled area 647 MB, line 1.40 GB). `canvas.Path.Copy` accounts for 1.77 GB flat. The canvas rasterizer copies paths to transform/fill them and expands stroked paths; the chart creates these changing paths each frame. Text rendering/lookahead is 431 MB cumulative. The text-key change reduced allocations attributed to `textImageKey` from about 99 MB to 50.5 MB, but total allocations stayed around 3.2 GB, within run-to-run variation.

## Current render-pipeline findings — 2026-10-26

- Keep this investigation on Go CPU/RAM in the existing render pipeline. Do not return to outline algorithms: the raster-dilation attempt was visibly different and reverted. The short-lived dynamic-gauge cache experiment was discarded after repeated same-time frames differed; it left no source change.
- Current queue path in `internal/cli/run.go`: workers render 4-frame batches, results arrive as batches, the master orders them through `pending`, then sends individual frames to the writer. Transparent mode adjusts the in-flight batch window from 4 to 32 frames; black mode uses `workers × 4`.
- Reusing the Canvas Context removed 153.63 MB attributed to `canvas.Context.Push` and about 230 MB total allocated, but did not show a speedup. It remains an allocation-only optimization.
- The path-fill comparison used the same full FIT, `virb-run.xml` (26 widgets), 1920×1080, 10 fps, 8 workers, black/YUV420P, and drain sink. Dynamic chart area and monotonic line polygons now use `DrawOwnedFilledPath`: transform the disposable per-frame path in place and send it to scanx. Static chart background remains on Canvas. A preceding `Path.CopyTo` attempt was ineffective: the library checks destination length, not capacity, so it still allocated about 1.79 GB. Do not repeat that approach.
- On the corrected path-fill pair, allocation samples fell from 2.996 GB to 1.172 GB (−61%); the 1.838 GB `canvas.Path.Copy` hotspot disappeared from the top allocation list. An unprofiled full run produced the same 83,081 frames: 72.65 s / 1,151.4 fps / 833,290,240-byte max RSS before, and 72.29 s / 1,159.6 fps / 772,128,768-byte max RSS after. Peak RSS fell about 61 MB (7.3%). CPU did not materially change: profiled totals were 140.82 versus 140.37 CPU-s, within run noise. Keep this for the measured memory reduction; do not claim a meaningful speedup.
- The corrected post-change profile's remaining allocation leaders are `image.NewRGBA` (438 MB), `canvas.Path.LineTo` (93 MB), and `canvas.Path.offset` (76 MB). `rasterizeTextGlyphs` accounts for 264.6 MB cumulative allocated space, including 180.2 MB for composed text bitmaps; `textImageKey` accounts for 44 MB. In the matching CPU profile, runtime/pipe work dominates (`pthread_cond_signal` 32.2%, `syscall.rawsyscalln` 27.4%, `madvise` 17.4%); text-cache building is under 1.2% cumulative. This is allocation evidence, not proof that text rasterization is the CPU bottleneck. Next measure text-cache hit/eviction/rebuild behavior before changing the cache. The drain-sink profile is not a real FFmpeg encode profile. Do not return to queue batching or outline algorithms.
- Follow-up source check: `virb-run.xml` contains no `cairo_gauge_*` widgets, so the per-frame radial-gradient path does not explain its CPU profile. The separate Cairo-gauge test profile sampled gradient evaluation at about 0.3% CPU; the old gauge-cache detour was already reverted. Do not optimize that path for this benchmark.
- A same-input full drain run with `GODEBUG=gctrace=1` recorded 12 total GC cycles, with only three during the 72-second render; each reported pause was around 1–2 ms. A separate `scavtrace=1` run reported 0 KiB of background/eager scavenging work at each sample. This does not explain the 17.4% `runtime.madvise` CPU sample; pprof places it under allocator `sysUsed`/`mheap.allocSpan`, so keep the cause open rather than labeling it GC cost.
- The corrected CPU profile attributes 27.4% cumulative to `Encoder.WriteYUV420P` through `bufio.Writer.Write`/`syscall.write`, 32.2% to runtime wakeups, and 9.0% cumulative (7.3% flat) to `rgbaToYUV420PInPlace`. A targeted 1920×1080 sparse-black converter benchmark showed that combining luma byte stores with `binary.LittleEndian.PutUint16` was slower: median 763 µs/frame versus 735 µs/frame for the current stores. That attempt was reverted; the reproducible benchmark remains in `internal/cli/pixel_format_test.go`. Do not retry pair stores.
- One mid-run goroutine snapshot showed the writer inside `syscall.write` to FFmpeg stdin; 2 of 8 workers were converting YUV while 6 waited for jobs, and the master was receiving results. This confirms a live pipe write and intermittent worker idling at that instant, but one snapshot cannot quantify how long that state lasts. The CPU profile's `pthread_cond_signal` samples do not identify which channel caused each wakeup.
- Writer batching was already tested: 80.58 s / 157.40 CPU-s batched versus 78.99 s / 153.24 CPU-s current. Do not repeat it or replace `pending` based only on operation counts; queue bookkeeping is not a measured allocation source.
- Next separate application CPU from runtime and pipe cost with a real FFmpeg run. The drain-sink profile's clear Go-side hotspot is YUV conversion; the write and wakeup samples are not direct evidence of expensive widget code. Before changing raster/text output, preserve same-frame visual comparison. Do not revisit the failed `Path.CopyTo` buffer, YUV pair stores, queue batching, or outline algorithms without new evidence.

## Real FFmpeg run — 2026-10-26

Full `i195882617.fit` with the same `virb-run.xml`, 1920×1080, 10 fps, black/YUV420P, 8 workers, and the `black-h264` encoder profile. FFmpeg encoded to the null muxer, so the codec ran without writing a large video file. The requested frame count was 83,081; the 111-second profile duration implies about 749 fps if the run completed. The helper discarded CLI progress output, so its final written-frame count was not captured.

- One-second process samples: Go averaged 162% CPU (peak 189%) and 567 MiB RSS average (747 MiB peak); FFmpeg averaged 496% CPU (peak 535%) and about 501 MiB RSS. Go pprof sampled 169.63 CPU-s over 111 s (153%); small difference from `ps` sampling is expected.
- Go CPU profile: `Encoder.WriteYUV420P` → `writeAll`/`write` 29.9%; `rgbaToYUV420PInPlace` 19.1% flat / 24.7% cumulative; `image/draw.DrawMask` memory copy 10.3%; runtime condition signaling 7.7%, `kevent` 6.9%, `madvise` 5.5%.
- At the single 30-second goroutine snapshot, the writer was in the FFmpeg stdin `write` syscall, the master was waiting for worker results, and all 8 workers were waiting for jobs. Treat this as a momentary pipe-backpressure snapshot, not a whole-run worker-idle measurement.
- This confirms the real run spends Go CPU in the pipe-write path and YUV conversion; it does not show that queue handoff itself is expensive. Master-to-writer batching already showed no improvement in the drain benchmark, so do not repeat it without new evidence. The drain benchmark's wakeup share (32%) also does not carry over: this real-run profile sampled 7.7% in condition signaling.
- The code also redrew an opaque black pixel into every RGBA pixel each frame using `image/draw`. A `clear(frame.Pix)` is enough for the YUV path: the renderer still composites widgets as black-backed (`opaque` stays true), and the YUV converter ignores alpha. A five-timepoint comparison against the original renderer on `virb-run.xml` found zero differing RGB bytes, including after buffer reuse. A converter check confirms alpha is ignored.
- Full rerun with that black-YUV clear path: CLI confirmed all 83,081 frames, 1m45.775s, 787.4 fps. The previous profile ran 111s but did not save a final frame count, so treat the roughly 5% speed gain against it as indicative. Go CPU samples fell from 169.63 to 159.33 CPU-s per run (−6.1%); process sampling averaged 159.7% Go CPU versus 162.1% before. FFmpeg remained near five cores (513.8% average versus 495.5%); peak Go RSS was effectively unchanged (748.5 vs 747.2 MiB), while sampled average RSS was 554 vs 567 MiB.
- `draw.Draw` background fill accounted for 17.68 CPU-s in the old profile; `clear` accounts for 1.78 CPU-s in the new profile. The isolated 1080p check measured `clear` at ~30.6 µs/frame, versus ~124 µs for `image/draw`; adding alpha back with a strided loop took ~537 µs, and copying a full prebuilt black frame was about the same as `image/draw`. Keep the faster clear only on the YUV420P path; the regular RGBA API still returns opaque black.
- After the clear optimization, the same full FIT was tested with 4, 8, and auto (12) workers. All runs encoded 83,081 frames through real FFmpeg to the null muxer. FPS was 785.5 / 787.4 / 787.0 respectively; Go peak RSS was 521 / 748.5 / 919.8 MiB. Mean Go CPU stayed near 159%, and FFmpeg near 5.1 cores. Auto 12 used ~399 MiB more peak Go RSS than 4 for 0.2% more throughput. Auto now caps at 4; explicit `--workers N` still overrides it. These are 1-second RSS samples and apply to this M4 Pro / layout / encoder.
- The repository currently contains only layouts with 19–26 components, not the 66-widget stress layout. Recheck the auto cap on that heavier workload if its template becomes available; explicit worker counts remain available.
- That allocation profile identified the next memory target: `Renderer.NewFrameBuffer` allocated 253 MB of RGBA frame storage (~31 full frames), while composed text bitmaps allocated 180 MB cumulatively. The compact YUV output path is now implemented and measured below. Text lookup/rasterization CPU was below 1.5% cumulative, so the text cache remains unchanged.

## Older queue trace — 62 widgets

The separate 62-widget profile used a sanitized copy of `examples/reference-layouts/example.xml`; five moving/journey map widgets were removed to avoid tile requests, so it was not a full map workload. With 8 workers and a 32-frame window, workers returned four-frame batches, the master reordered by index, and the writer still received individual frames.

- In the 9.69-second CPU-profile run, worker rendering used 14.90 CPU-s (33.6%), YUV conversion 4.20 CPU-s (9.5%), and FFmpeg-pipe writes 3.75 CPU-s (8.5%). Master reorder/send used about 10 ms CPU. The writer waited for its queue 52.6 ms total; the master waited for worker results about 9.1 s aggregate, and workers waited for jobs about 25.5 s aggregate.
- `runtime.pthread_cond_signal` accounted for 19.8% of samples, but the stack loses the app caller at the Go runtime transition. Scheduler trace showed 367 ms total delay over 9.7 s; this indicates pipeline waits, not that channel wakeups themselves dominate CPU.
- Same-binary runs without profilers: a 32-frame window took 9.74 s, used 793 MB peak RSS, and 57.42 CPU-s; a 16-frame window took 14.03 s, 515 MB, and 54.25 CPU-s. The lower window saved 278 MB (35%) but cost 44% wall time. Keep the current throughput-oriented window unless the memory/throughput tradeoff is explicitly revisited.

## Earlier allocation reductions already in the renderer

- Reusing the frame rasterizer and bounding the frame pool reduced `NewFrameBuffer` allocation from 1.03 GB to 253 MB and total allocation from 18.41 GB to 17.60 GB on the 62-widget profile; no speed gain was established. `prepareFrame` also reuses its scratch image/rasterizer, and chart scratch/precomputed points are retained.
- Limiting the shared text cache to 4,096 entries reduced observed high-water RSS from 1,036 MB to 941 MB versus 8,192 entries, with no measured CPU/wall penalty. Disabling full-string caching increased CPU by about 4.5% and allocations by about 2.2× for only 24 MB less end heap; keep the cache.
- Precomputing metric/date-time ascent reduced `FontFamily.Face` allocations by about 132 MB and total allocation by about 1.6% on a 26-widget `virb-run` profile, without a measured speed gain. Text remains a secondary allocation target, not the dominant CPU cost.

## Recent allocation optimizations — 2026-10-26

The latest same-input full profiles use `i195882617.fit`, `layouts/virb-run.xml` (26 widgets), 1920×1080, 10 fps, black/YUV420P, 4 workers, and the same FFmpeg null-output setup. They show fewer allocations, but no demonstrated end-to-end speedup: the CPU profile remains dominated by pipe writes and YUV conversion.

| State | `alloc_space` | Full-run time / FPS | Go CPU | Peak RSS |
|---|---:|---:|---:|---:|
| Reused draw context | 947.88 MB | 105.58 s / 792.0 fps | 147.55% | 504 MiB |
| Cache-hit fast path | 860.98 MB | about 105 s / 796.3 fps | 148.37% | about 504 MiB |
| Structured text cache key | 813.60 MB | 104.93 s profile duration | 147.42% | not captured on this run |

The reusable `FrameBuffer` now retains its render context, removing the per-frame context allocation. The fast cache-hit path checks for an already-rasterized text image before creating builder/lookahead closures; its pprof diff attributes 44.50 MB fewer allocations to `drawTextWithLookahead`. Text image keys are now comparable structs instead of serialized strings; the latest allocation profile fell a further 47.38 MB and no longer lists `textImageKey` among its main allocation sites. Across these runs, `alloc_space` is down 134.28 MB (14.2%) from the context-reuse profile to the structured-key profile. Treat this as an allocation reduction, not an RSS reduction: latest peak RSS was not recorded, and peak RSS remained about 504 MiB in the prior runs.

The latest CPU profile sampled 154.69 CPU-s over 104.93 s (147.42%). Top costs were raw syscalls, mostly the pipe-write path (32.8% flat), RGBA→YUV420P conversion (25.1% flat; 31.0% cumulative), runtime condition signaling (8.8%), `kevent` (7.8%), and compositing (6.6%). This confirms the text-cache work targets allocation churn rather than the dominant CPU path. The changes passed `go test ./internal/... ./cmd/... -count=1`; no separate visual frame comparison was run for the latest cache-key changes.

The user also reports lower process-monitor readings during export: CPU about 190% before to 140–150% now, and memory about 1,110 MB to 250–330 MB. The exact workload and sampling basis for those readings were not recorded, so keep them separate from the controlled pprof and RSS measurements above.

## Compact YUV output — 2026-10-26

The previous converter wrote Y into the RGBA allocation, kept U/V in scratch, then copied both chroma planes into the frame before the writer could send it. The current converter writes Y/U/V directly into a compact output buffer. Workers return each 8.3 MB RGBA `FrameBuffer` immediately after conversion; the ordered queue retains pooled 3.1 MB YUV frames instead. The YUV pool is bounded by the existing in-flight frame window.

- Same full FIT/layout/size/rate, 4 workers, real `black-h264` FFmpeg to the null muxer. All 83,081 frames completed in 104.94 s at 793.7 fps. The previous 4-worker run completed at 785.5 fps; this ~1% change is within run noise, so do not claim a speedup.
- Go pprof: 155.84 CPU-s over 104.94 s (148.5%), versus 159.64 CPU-s over 106.04 s (150.5%) in the prior 4-worker profile. CPU use is slightly lower, but the difference is small. The current top costs are pipe `write` 33.1%, YUV conversion 23.5%, `drawRGBAOverOpaque` 5.9%, and `binary.LittleEndian.Uint64` in the converter 5.8%.
- Allocation profile: 1.002 GB versus 1.075 GB before (−6.8%). `image.NewRGBA` fell from 318 MB to 219 MB (−31%); the compact YUV pool added about 48 MB. This matches returning RGBA frames sooner and keeping compact outputs in flight.
- `/usr/bin/time -l` reported 504 MiB maximum Go-process RSS. The earlier 521 MiB was a one-second `ps` sample, so treat the apparent 17 MiB reduction as indicative, not a like-for-like peak measurement.
- The converter benchmark was essentially unchanged: prior in-place runs were 720–725 µs/frame; direct-output runs were 711–732 µs/frame. Exact output is checked against the reference converter, alpha independence remains covered, and the full FFmpeg run accepted every frame.
- The 26-widget `virb-run.xml` has no `cairo_gauge_*` widgets. The separate gauge test profile attributed about 0.3% CPU to radial gradient evaluation, so it is not a current optimization target. The text cache also remains low in CPU cost. Do not revisit outline, gradient, cache, or queue batching without new profile evidence.

## Opaque cached-image compositing — 2026-10-26

`drawRGBAOverOpaque` now uses an equivalent byte-range blend expression, retaining its alpha-zero and alpha-255 fast paths. An exhaustive test compares it with the previous formula for every alpha, source byte, and destination byte. The 512×128 microbenchmark improved from 120.2–120.4 µs to 111.1–111.2 µs (about 7.6%), with zero allocations.

- Full 4-worker run, same FIT/layout/FFmpeg null muxer: all 83,081 frames completed at 793.8 fps in about 105 s. Previous run was 793.7 fps in 104.94 s; throughput did not measurably change.
- Go pprof total was 155.29 CPU-s / 104.95 s (148.0%) versus 155.84 / 104.94 s (148.5%) before. That small difference is within run variation; peak RSS remained 504 MiB. The compositing share was sampled at 6.3% versus 5.9% before, so the profile does not establish an end-to-end CPU reduction.
- Current top profile shares: pipe/syscall 33.4%, YUV conversion 24.2%, compositing 6.3%, and `binary.LittleEndian.Uint64` 4.8%. The 64-bit read compiles to one machine load; replacing it with `unsafe` is not justified.

Keep the exact blend rewrite for its isolated measured gain and exhaustive equivalence check, but do not present it as a whole-render speedup. The pipe and YUV converter remain the larger targets; master-to-writer batching has already been tested without gain.

## Already checked — do not repeat without a new hypothesis

- Master reorder/send consumed about 10 ms in the 62-widget profile; writer queue wait was about 53 ms total. Queue handoff was not a measured CPU hotspot.
- Master-to-writer batching was tried and reverted without measurable gain.
- Reducing the black-mode in-flight window from 32 to 16 cut peak RSS by 278 MB (35%) but increased wall time from 9.74 s to 14.03 s (44%). Keep 32 for throughput.
- Turning off the shared full-string text cache increased CPU about 4.5% and allocation about 2.2× for only 24 MB less end heap. Keep the cache.
- Writing pairs of black-background Y samples with `binary.LittleEndian.PutUint16` was about 5% slower in the existing full-frame conversion benchmark (713 µs to 746–758 µs); the trial was reverted.
- The gauge-cache detour was reverted. Do not repeat it without new profile evidence.
- The bitmap-dilation outline experiment changed appearance and was reverted. Do not revisit outline/stroke algorithms here.

## Repro files and workflow

Raw profile dumps, generated images, and scratch binaries in `tmp/profile-audit/` were removed after their measurements were consolidated here. This file is the single investigation record. Make one source change at a time, then compare the same full FIT profile and a visual frame. Keep the user's separate font-comparison artifacts under `tmp/font-compare/`.

The profiling harness was temporary and is not part of the repository. Run the repository test suite with `go test ./...`.
