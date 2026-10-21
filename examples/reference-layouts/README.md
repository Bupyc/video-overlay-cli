# Reference layouts

English | [Русский](README.ru.md)

These XML layouts are copies of files from the layout and example directories in `gopro-dashboard-overlay`. They demonstrate the currently supported component types in full dashboards, motorcycle layouts, power dashboards, route maps, and Cairo-origin gauges.

## Layouts

- `default-1920x1080.xml`, `default-2688x1512.xml`, `default-2704x1520.xml`, `default-3840x2160.xml` — default activity dashboards at the source project’s listed sizes.
- `moto_1080.xml`, `moto_1080_2bars.xml`, `moto_1080_needle.xml` — 1080p motorcycle layouts and motor-speed indicator variants.
- `moto_2160.xml`, `moto_2160_2bars.xml`, `moto_2160_needle.xml` — 2160p versions of those layouts.
- `power-1920x1080.xml` — cycling power and heart-rate dashboard.
- `example.xml` — vertical top-to-bottom text (`direction="ttb"`).
- `example-2.xml` — chart, zone bar, route, and vector-gauge examples.
- `demo-circuit.xml`, `demo-gears.xml`, `demo-speed.xml`, `layout-cairo-2704x1520.xml` — focused route, gear, speed, and vector-gauge examples.

The default and motorcycle layouts use tile-backed maps; uncached areas need a network connection on first render. Match `--size` to the layout’s dimensions because widget coordinates are absolute.

See the [widget matrix](../../docs/widget-matrix.md) for current coverage and known differences.
