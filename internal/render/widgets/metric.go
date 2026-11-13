package widgets

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	"video-overlay-cli/internal/activity"
	"video-overlay-cli/internal/layout"
	"video-overlay-cli/internal/metrics"
	"video-overlay-cli/internal/render"
)

func metricFactory(element *layout.Element, setup *render.Context) (render.Drawer, error) {
	name := attr(element, "metric", "")
	if name == "" {
		return nil, fmt.Errorf("metric component requires metric")
	}
	customField, err := boolAttr(element, "custom_field", false)
	if err != nil {
		return nil, err
	}
	if !customField {
		if _, _, err := metric(setup, element); err != nil {
			return nil, err
		}
	}
	size, err := floatAttr(element, "size", 16)
	if err != nil || size <= 0 {
		return nil, fmt.Errorf("metric size must be positive")
	}
	align := attr(element, "align", "left")
	cache, err := boolAttr(element, "cache", true)
	if err != nil {
		return nil, err
	}
	format := attr(element, "format", "")
	if dp := attr(element, "dp", ""); dp != "" {
		if format != "" {
			return nil, fmt.Errorf("metric cannot use both format and dp")
		}
		format = "." + dp + "f"
	}
	fill, err := colorAttr(element, "rgb", color.RGBA{R: 255, G: 255, B: 255, A: 255})
	if err != nil {
		return nil, err
	}
	outline, err := colorAttr(element, "outline", color.RGBA{A: 255})
	if err != nil {
		return nil, err
	}
	outlineWidth, err := floatAttr(element, "outline_width", 2)
	if err != nil || outlineWidth < 0 {
		return nil, fmt.Errorf("metric outline_width must not be negative")
	}
	units := attr(element, "units", "")
	conversionMetric := name
	if customField {
		conversionMetric = "custom"
	} else if name == "pace" {
		// The pace accessor selects a speed in m/s; render it with speed-to-pace conversion.
		conversionMetric = "speed"
	}
	ascent := setup.Fonts.Face(size*72/25.4, nrgba(fill)).Metrics().Ascent
	var lookahead textLookahead
	if cache {
		lookahead = func(ctx *render.Context, current string, done <-chan struct{}, prefetch func(string) bool) {
			prefetchFutureLabels(ctx, current, func(sample activity.Sample) string {
				if customField {
					return metricText(customMetricValue(ctx.Activity, sample, name), units, format, conversionMetric)
				}
				value, err := metrics.ValueAt(sample, name)
				if err != nil {
					return ""
				}
				return metricText(value, units, format, conversionMetric)
			}, prefetch, done)
		}
	}
	return func(ctx *render.Context) error {
		var value activity.Value
		if customField {
			value = customMetricValue(ctx.Activity, ctx.Sample, name)
		} else {
			metricValue, _, metricErr := metric(ctx, element)
			if metricErr != nil {
				return metricErr
			}
			value = metricValue
		}
		text := metricText(value, units, format, conversionMetric)
		drawTextWithLookahead(ctx, 0, ascent, size, text, align, fill, outline, outlineWidth, cache, lookahead)
		return nil
	}, nil
}

func customMetricValue(a *activity.Activity, sample activity.Sample, name string) activity.Value {
	if a == nil {
		return activity.Value{}
	}
	index, ok := a.CustomFieldIndices[name]
	if !ok {
		return activity.Value{}
	}
	if index < len(sample.CustomFieldValues) && sample.CustomFieldValues[index].Valid {
		return sample.CustomFieldValues[index]
	}
	if index < len(a.CustomFieldValues) {
		return a.CustomFieldValues[index]
	}
	return activity.Value{}
}

func metricUnitFactory(element *layout.Element, setup *render.Context) (render.Drawer, error) {
	name := attr(element, "metric", "")
	if name == "" {
		return nil, fmt.Errorf("metric_unit component requires metric")
	}
	if _, _, err := metric(setup, element); err != nil {
		return nil, err
	}
	size, err := floatAttr(element, "size", 16)
	if err != nil || size <= 0 {
		return nil, fmt.Errorf("metric_unit size must be positive")
	}
	fill, err := colorAttr(element, "rgb", color.RGBA{R: 255, G: 255, B: 255, A: 255})
	if err != nil {
		return nil, err
	}
	outline, err := colorAttr(element, "outline", color.RGBA{A: 255})
	if err != nil {
		return nil, err
	}
	outlineWidth, err := floatAttr(element, "outline_width", 2)
	if err != nil || outlineWidth < 0 {
		return nil, fmt.Errorf("metric_unit outline_width must not be negative")
	}
	units := attr(element, "units", "")
	label := strings.TrimSpace(element.Text)
	unit := unitText(units, name)
	if label == "" || label == "{}" {
		label = unit
	} else {
		label = strings.NewReplacer("{:~c}", unit, "{:~C}", unit, "{:~P}", unit, "{:~D}", unit).Replace(label)
	}
	align := attr(element, "align", "left")
	bitmap, position := prepareStaticTextImage(setup, size, label, align, fill, outline, outlineWidth)
	return func(ctx *render.Context) error {
		drawTextBitmap(ctx, bitmap, position)
		return nil
	}, nil
}

func datetimeFactory(element *layout.Element, setup *render.Context) (render.Drawer, error) {
	format := attr(element, "format", "")
	if format == "" {
		return nil, fmt.Errorf("datetime component requires format")
	}
	size, err := floatAttr(element, "size", 16)
	if err != nil || size <= 0 {
		return nil, fmt.Errorf("datetime size must be positive")
	}
	truncate, err := intAttr(element, "truncate", 0)
	if err != nil || truncate < 0 {
		return nil, fmt.Errorf("datetime truncate must not be negative")
	}
	fill, err := colorAttr(element, "rgb", color.RGBA{R: 255, G: 255, B: 255, A: 255})
	if err != nil {
		return nil, err
	}
	outline, err := colorAttr(element, "outline", color.RGBA{A: 255})
	if err != nil {
		return nil, err
	}
	outlineWidth, err := floatAttr(element, "outline_width", 2)
	if err != nil || outlineWidth < 0 {
		return nil, fmt.Errorf("datetime outline_width must not be negative")
	}
	align := attr(element, "align", "left")
	cache, err := boolAttr(element, "cache", true)
	if err != nil {
		return nil, err
	}
	dateLayout := timeFormat(format)
	var lookahead textLookahead
	if cache {
		lookahead = func(ctx *render.Context, current string, done <-chan struct{}, prefetch func(string) bool) {
			prefetchFutureLabels(ctx, current, func(sample activity.Sample) string {
				return truncateText(sample.Timestamp.In(time.Local).Format(dateLayout), truncate)
			}, prefetch, done)
		}
	}
	ascent := setup.Fonts.Face(size*72/25.4, nrgba(fill)).Metrics().Ascent
	return func(ctx *render.Context) error {
		value := ctx.Sample.Timestamp.In(time.Local).Format(dateLayout)
		drawTextWithLookahead(ctx, 0, ascent, size, truncateText(value, truncate), align, fill, outline, outlineWidth, cache, lookahead)
		return nil
	}, nil
}
