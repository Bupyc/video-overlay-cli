# XML widget coverage

English | [Русский](widget-matrix.ru.md)

The catalog below is based on the original Python implementations in `gopro_overlay/layout_xml.py` and `gopro_overlay/layout_xml_cairo.py`. “Supported” means the Go CLI can render the component from XML.

## XML elements

| XML name | Upstream factory | Status | Notes |
|---|---|---|---|
| `composite` | container | Supported | Groups and positions child widgets. |
| `translate` | alias of `composite` | Supported | Same behavior as `composite`. |
| `frame` | container | Supported | Framed group with opacity and fade options. |

## XML component factories

| Component type | Upstream factory | Status | Notes |
|---|---|---|---|
| `asi` | `create_asi` | Supported | Airspeed indicator. |
| `bar` | `create_bar` | Supported | Scalar bar. |
| `cairo_circuit_map` | `create_cairo_circuit_map` | Supported | Vector route, marker, rotation, and privacy-aware route gaps. |
| `cairo_gauge_arc_annotated` | `create_cairo_gauge_arc_annotated` | Supported | Vector dial with optional metric/value arc. |
| `cairo_gauge_donut` | `create_cairo_gauge_donut` | Supported | Vector annular dial with gradient fill. |
| `cairo_gauge_marker` | `create_cairo_gauge_marker` | Supported | Vector gauge arc and current-value marker. |
| `cairo_gauge_round_annotated` | `create_cairo_gauge_round_annotated` | Supported | Vector dial, labels, and needle. |
| `chart` | `create_chart` | Supported | Supports time- and distance-based windows. |
| `circuit_map` | `create_circuit_map` | Supported | Draws the recorded route without map tiles. |
| `compass` | `create_compass` | Supported | Heading gauge. |
| `compass_arrow` | `create_compass_arrow` | Supported | Heading arrow. |
| `datetime` | `create_datetime` | Supported | Activity timestamp. |
| `gps_lock_icon` | `create_gps_lock_icon` | Supported | GPS fix indicator and icon assets. |
| `icon` | `create_icon` | Supported | Static icon asset. |
| `journey_map` | `create_journey_map` | Supported | Fits the public route to the widget and draws its current position. Uses the configured tile provider. |
| `metric` | `create_metric` | Supported | Formatted activity metric. |
| `metric_unit` | `create_metric_unit` | Supported | Unit for a metric. |
| `moving_journey_map` | `create_moving_journey_map` | Supported | Shows the route in a moving viewport. Uses the configured tile provider. |
| `moving_map` | `create_moving_map` | Supported | Heading-oriented moving map with route marker. Uses the configured tile provider. |
| `msi` | `create_msi` | Supported | Motor speed indicator. |
| `msi2` | `create_msi2` | Supported | Second motor speed indicator variant. |
| `text` | `create_text` | Supported | Text from element content; supports left-to-right and top-to-bottom directions. |
| `zone_bar` | `create_zone_bar` | Supported | Zone indicator. |

`gradient_chart` is a deprecated alias handled by `create_gradient_chart`, which delegates to `create_chart`. Component names are normalized by replacing `-` with `_`, so hyphenated spellings such as `cairo-gauge-donut` resolve to the same factory. Container and frame traversal lives in `internal/render`, keeping the renderer independent from the widget package.

All component types used by the bundled upstream layout files have Go factories. Text supports the upstream `ltr` and `ttb` directions. XML validation currently checks component types but does not reject unsupported attributes.

## Non-XML widgets

| Widget | Status | Notes |
|---|---|---|
| `SparkLine` | Internal only | Constructible by Go code inside this module; not available in XML or the CLI. Upstream has no XML factory. |
| `ComparativeEnergy` | Internal only | Constructible by Go code inside this module; not available in XML or the CLI. Upstream has no XML factory. |

Tile-backed maps default to OpenStreetMap tiles, show attribution, and use the OS cache directory. `--tile-url`, `--tile-cache`, and `--tile-attribution` select another compatible tile source. `--privacy LAT,LON,KM` suppresses map output inside the specified radius. Unknown component types produce an explicit error before FFmpeg starts.

The visual template editor is planned as a separate project in [`video-overlay-ui/`](../../video-overlay-ui/); it is not implemented yet. This CLI reads and renders XML layouts but does not edit or preview them.
