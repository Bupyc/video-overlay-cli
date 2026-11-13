# Metric field reference

English | [Русский](metrics.ru.md)

Built-in metric identifiers in the table below are exact, lowercase XML field names and are not translated. For a custom FIT field, use its exact FIT field name, preserving capitalization and spaces.

```xml
<component type="metric" x="16" y="24" metric="Battery Level"
           custom_field="true" dp="0" size="32"/>
```

For a named numeric FIT field, set `custom_field="true"` on a `<component type="metric">` and use its exact FIT field name as `metric`. Matching is case-sensitive and includes Developer Fields described in the FIT file. Custom scalar fields from `record` messages vary with the activity samples and are interpolated between samples; fields from `session` messages are constant for the activity. String values, arrays, and values found only in other message types are not available through this option. FIT units are not appended or used automatically; `units` still applies the standard conversions, so leave it unset when the custom value is already in the desired unit. Missing values display as `-`.

For built-in metrics, `units` converts numeric values for display and `format` controls numeric formatting. `pace` should use `units="pace_km"` or `units="pace_mi"` (optionally with `format="pace"`). `elapsed` defaults to `HH:MM:SS`; setting `format` displays its numeric value in seconds instead. Missing built-in values display as `-` (`—` for pace).

## Fields

| XML field | Description |
| --- | --- |
| `hr` | Recorded heart rate, in beats per minute. |
| `cadence` | Recorded cadence value. For running, this is the input used to derive `running_cadence`. |
| `running_cadence` | Running cadence in steps per minute; derived as twice `cadence` when a cycling profile is not selected. |
| `power` | Recorded power, in watts. |
| `temp` | Recorded temperature, in °C by default. |
| `respiration` | Recorded respiration rate from FIT, in breaths per minute. |
| `sdps` | The FIT `sdps` value, passed through without conversion. |
| `gear.front` | Front gear number from FIT gear events; the last value carries forward until another event. |
| `gear.rear` | Rear gear number from FIT gear events; the last value carries forward until another event. |
| `speed` | Smoothed recorded speed when present; otherwise the short-window GPS speed. Base unit: m/s. |
| `raw_speed` | Recorded speed before smoothing. Base unit: m/s. |
| `cspeed` | Kalman-smoothed short-window GPS speed. Base unit: m/s. |
| `cspeed.k` | Alias for `cspeed`. |
| `cspeed.raw` | Short-window GPS speed before Kalman smoothing. Base unit: m/s. |
| `lspeed` | Long-window speed, using FIT distance when available and otherwise GPS distance. Base unit: m/s. |
| `pace` | Pace calculated from the selected speed. Use `units="pace_km"` for min/km or `units="pace_mi"` for min/mi. |
| `accel` | Change in recorded speed over the short window. Base unit: m/s²; unavailable when recorded speed is missing. |
| `dist` | Distance increment for the sample, in meters. A short-window GPS increment is used when available. |
| `odo` | Cumulative FIT distance when present; otherwise cumulative GPS distance. Base unit: meters. |
| `codo` | Cumulative GPS-derived distance, in meters. |
| `alt` | Recorded altitude, in meters by default. |
| `gradient` | Selected grade in percent: FIT-distance grade when possible, otherwise a time-window GPS estimate. |
| `cgrad` | Short-window time-based grade estimate in percent. |
| `vertical_speed` | Recorded vertical speed when present; otherwise an adaptive estimate. Base unit: m/s; `units="m/h"` or `units="ft/h"` displays an hourly rate. |
| `cvspeed` | Derived vertical speed over the short window. Base unit: m/s. |
| `lvspeed` | Derived vertical speed over the long window. Base unit: m/s. |
| `xlvspeed` | Derived vertical speed over the extra-long window. Base unit: m/s. |
| `time` | Seconds between the sample pair used for short-window GPS speed. This is a calculation interval, not time since activity start. |
| `elapsed` | Seconds since the activity start. By default, displays as `HH:MM:SS`, with hours continuing past 24. |
| `azi` | GPS bearing in signed degrees, from −180° to 180°. |
| `cog` | GPS course over ground in degrees, normalized to 0°–360°. |
| `lat` | Smoothed GPS latitude in decimal degrees. |
| `lon` | Smoothed GPS longitude in decimal degrees. |
| `gps-dop` | FIT GPS accuracy value, in meters when present. |
| `timestamp` | Sample timestamp as Unix seconds. |
| `gps-packet` | Zero-based GPX track-point number. |
| `gps-packet-index` | GPX packet index; currently set to `0` by the GPX importer. |
| `gps-lock` | GPX GPS-lock value; currently set to `3` by the GPX importer. |

The following built-in identifiers are not populated by the current FIT or GPX importers: `accl.x`, `accl.y`, `accl.z`, `grav.x`, `grav.y`, `grav.z`, `ori.pitch`, `ori.roll`, and `ori.yaw`. `custom_field="true"` can read a same-named scalar field from a `record` or `session` message if one is present; it does not unpack sensor arrays from other FIT messages.

## Developer Field examples

These names and units are present in one FIT activity. Keep their capitalization and spaces when using them in `metric`:

| FIT field name | Units in FIT | Message |
| --- | --- | --- |
| `Battery Level` | `%` | `record` |
| `Gps Level` | — | `record` |
| `GAP Pace` | `min/km` | `record` |
| `Anaerobic Reserve` | `%` | `record` |
| `Sustainable Pace` | `min/km` | `record` |
| `Accumulated Work` | `kJ/kg` | `record` |
| `Carbohydrate Remaining` | `g` | `record` |
| `Eccentric Load` | `m` | `record` |
| `Critical Speed` | `m/s` | `session` |
| `D Prime` | `m` | `session` |

The `metric` widget displays the numeric value. It does not append the FIT unit automatically; add a text label if the unit should appear in the overlay. If multiple Developer Data fields have the same name, loading fails with an ambiguity error rather than selecting one silently.

## Calculation details

- FIT cumulative distance is used for `gradient` only when every sample has a valid, nondecreasing value. Points are paired across at least 10 m. Grade is skipped if the distance span is 1 m or less, either endpoint lacks a 3D GPS fix or altitude, or the result exceeds ±80%. A complete but decreasing FIT distance series currently yields no grade.
- When FIT distance is incomplete, time-based grade uses 3-, 30-, and 120-second windows. Derived vertical speed uses the same windows when no recorded value is present; both endpoints need a 3D GPS fix and altitude. The adaptive metrics use the 30-second window at paces slower than 20 min/km and the 120-second window at paces slower than 30 min/km.
- `pace` selects between 3- and 30-second speed estimates, using 30 seconds at paces slower than 20 min/km. It is unavailable at speeds of 0.05 m/s or less; pace conversion is capped at 500 min/km.
- Recorded speed is smoothed with the upstream Kalman parameters. GPS positions use exponential smoothing with a coefficient of 0.45 before route distance, speed, and bearing are calculated.
- Running cadence is converted from revolutions per minute to steps per minute by multiplying by two. Cycling cadence remains in revolutions per minute.
- GPS distance fallback uses a spherical Haversine calculation with a mean Earth radius. It is faster, but differs slightly from the WGS84 ellipsoid calculation used upstream.
