package widgets

import (
	"video-overlay-cli/internal/layout"
	"video-overlay-cli/internal/render"
)

func RegisterDefaults(registry *render.Registry) {
	components := []struct {
		name    string
		factory render.Factory
	}{
		{"asi", asiFactory},
		{"bar", barFactory},
		{"chart", chartFactory},
		{"circuit_map", circuitMapFactory},
		{"cairo_circuit_map", cairoCircuitFactory},
		{"cairo_gauge_arc_annotated", func(e *layout.Element, c *render.Context) (render.Drawer, error) {
			return cairoGaugeFactory(e, c, "cairo_gauge_arc_annotated")
		}},
		{"cairo_gauge_donut", func(e *layout.Element, c *render.Context) (render.Drawer, error) {
			return cairoGaugeFactory(e, c, "cairo_gauge_donut")
		}},
		{"cairo_gauge_marker", func(e *layout.Element, c *render.Context) (render.Drawer, error) {
			return cairoGaugeFactory(e, c, "cairo_gauge_marker")
		}},
		{"cairo_gauge_round_annotated", func(e *layout.Element, c *render.Context) (render.Drawer, error) {
			return cairoGaugeFactory(e, c, "cairo_gauge_round_annotated")
		}},
		{"compass", compassFactory},
		{"compass_arrow", compassArrowFactory},
		{"datetime", datetimeFactory},
		{"gps_lock_icon", gpsLockFactory},
		{"icon", iconFactory},
		{"metric", metricFactory},
		{"metric_unit", metricUnitFactory},
		{"moving_map", movingMapFactory},
		{"journey_map", journeyMapFactory},
		{"moving_journey_map", movingJourneyMapFactory},
		{"msi", msiFactory},
		{"msi2", msi2Factory},
		{"text", textFactory},
		{"zone_bar", zoneBarFactory},
		{"gradient_chart", chartFactory},
	}
	for _, component := range components {
		if registry.Status(component.name) != "supported" {
			_ = registry.Register(component.name, component.factory)
		}
	}
}
