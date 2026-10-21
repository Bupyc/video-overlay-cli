# Activity metric semantics

English | [Русский](metrics.ru.md)

The comparison baseline is the unmodified `time4tea/gopro-dashboard-overlay` at commit `8e26ee5`. Relative to it, this Go CLI adds trail-specific adaptive windows, FIT-distance gradient, vertical-speed handling, and running cadence in steps per minute. The customized local Python checkout used for performance comparison also contains some of these changes, but is not the baseline. Metric preparation currently runs serially before rendering, as in upstream.

The render CLI processes the loaded activity before preparing the layout, so these derived values are available to XML widgets.

- FIT cumulative distance is used for distance-based gradient only when every sample has a valid, nondecreasing value. Samples are paired across at least 10 m; gradients are skipped when the distance span is 1 m or less, either endpoint lacks a 3D GPS fix or altitude, or the result exceeds ±80%.
- The `speed` metric returns Kalman-smoothed FIT speed when present, otherwise the 3-second GPS-derived estimate. `pace` selects between the 3-second and 30-second speed estimates, using 30 seconds at paces slower than 20 min/km. It is unavailable at speeds of 0.05 m/s or less; pace conversion is capped at 500 min/km.
- When FIT distance is incomplete, time-based gradient uses 3-, 30-, and 120-second windows. Derived vertical speed uses the same windows when no recorded value is present; both endpoints need a 3D GPS fix and altitude. These metrics use the 30-second window at paces slower than 20 min/km and 120 seconds at paces slower than 30 min/km. A complete but decreasing FIT distance series currently yields no gradient.
- Recorded speed is smoothed with the upstream Kalman parameters. GPS positions use the upstream 0.45 exponential smoothing before route distance and bearing are calculated.
- Running cadence converts revolutions per minute to steps per minute by multiplying by two. Cycling cadence remains revolutions per minute.
- GPS distance fallback uses a spherical Haversine calculation with a mean Earth radius; it is fast and differs slightly from the upstream WGS84 ellipsoid calculation.
