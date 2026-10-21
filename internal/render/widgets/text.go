package widgets

import (
	"fmt"
	"image"
	"image/color"
	"strings"

	"video-overlay-cli/internal/layout"
	"video-overlay-cli/internal/render"
)

func textFactory(element *layout.Element, setup *render.Context) (render.Drawer, error) {
	value := strings.TrimSpace(element.Text)
	if value == "" {
		return nil, fmt.Errorf("text component must contain text")
	}
	size, err := floatAttr(element, "size", 16)
	if err != nil || size <= 0 {
		return nil, fmt.Errorf("text size must be positive")
	}
	align := attr(element, "align", "left")
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
		return nil, fmt.Errorf("text outline_width must not be negative")
	}
	var bitmap *image.RGBA
	var position image.Point
	if attr(element, "direction", "ltr") == "ttb" {
		bitmap, position = prepareStaticTopToBottomTextImage(setup, size, value, align, fill, outline, outlineWidth)
	} else {
		bitmap, position = prepareStaticTextImage(setup, size, value, align, fill, outline, outlineWidth)
	}
	return func(ctx *render.Context) error {
		drawTextBitmap(ctx, bitmap, position)
		return nil
	}, nil
}
