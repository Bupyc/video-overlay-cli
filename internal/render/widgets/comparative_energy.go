package widgets

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"

	"github.com/tdewolff/canvas"
	"video-overlay-cli/assets/icons"
	"video-overlay-cli/internal/activity"
	"video-overlay-cli/internal/render"
)

type energyCard struct {
	icon string
	mass float64
}

func NewComparativeEnergy(speed func() activity.Value, personKg, bikeKg, carKg, vanKg float64) render.Drawer {
	cards := []energyCard{
		{icon: "user.png", mass: personKg},
		{icon: "bicycle.png", mass: personKg + bikeKg},
		{icon: "car.png", mass: personKg + carKg},
		{icon: "van-black-side-view.png", mass: personKg + vanKg},
	}
	images := make([]image.Image, len(cards))
	var loadErr error
	for i, card := range cards {
		img, err := icons.Read(card.icon)
		if err != nil {
			loadErr = fmt.Errorf("load comparative energy icon %q: %w", card.icon, err)
			break
		}
		images[i] = invertImage(img)
	}
	return func(ctx *render.Context) error {
		if loadErr != nil {
			return loadErr
		}
		value := activity.Value{}
		if speed != nil {
			value = speed()
		}
		ctx.Canvas.Push()
		defer ctx.Canvas.Pop()
		for i, card := range cards {
			x := float64(i * 300)
			drawEnergyIcon(ctx, x+230, 0, images[i], 32)
			drawTextWithCache(ctx, x+200, 5+48*0.8, 48, kineticEnergyLabel(value, card.mass), "right", color.RGBA{R: 255, G: 255, B: 255, A: 255}, color.RGBA{A: 255}, 2, true)
			drawTextWithCache(ctx, x+200, 55+24*0.8, 24, energyMultiplier(card.mass, personKg), "right", color.RGBA{R: 255, G: 91, B: 78, A: 255}, color.RGBA{A: 255}, 2, true)
			drawTextWithCache(ctx, x+262, 70+24*0.33, 24, energyMassLabel(card.mass), "center", color.RGBA{R: 185, G: 185, B: 185, A: 255}, color.RGBA{A: 255}, 2, true)
		}
		return nil
	}
}

func kineticEnergyLabel(speed activity.Value, massKg float64) string {
	if !speed.Valid || speed.Number <= 0 || math.IsNaN(speed.Number) || math.IsInf(speed.Number, 0) || massKg <= 0 || math.IsNaN(massKg) || math.IsInf(massKg, 0) {
		return "-"
	}
	kilojoules := 0.5 * massKg * speed.Number * speed.Number / 1000
	decimals := 1
	if kilojoules > 10 {
		decimals = 0
	}
	return groupedFloat(kilojoules, decimals) + " kJ"
}

func energyMassLabel(massKg float64) string {
	if massKg <= 0 || math.IsNaN(massKg) || math.IsInf(massKg, 0) {
		return "-"
	}
	return groupedFloat(massKg, -1) + " kg"
}

func energyMultiplier(massKg, personKg float64) string {
	if massKg <= 0 || personKg <= 0 || math.IsNaN(massKg) || math.IsInf(massKg, 0) || math.IsNaN(personKg) || math.IsInf(personKg, 0) {
		return "-"
	}
	return groupedFloat(massKg/personKg, 1) + "x"
}

func groupedFloat(value float64, decimals int) string {
	text := strconv.FormatFloat(value, 'f', decimals, 64)
	parts := strings.SplitN(text, ".", 2)
	whole := parts[0]
	start := 0
	if strings.HasPrefix(whole, "-") || strings.HasPrefix(whole, "+") {
		start = 1
	}
	for i := len(whole) - 3; i > start; i -= 3 {
		whole = whole[:i] + "," + whole[i:]
	}
	if len(parts) == 2 {
		return whole + "." + parts[1]
	}
	return whole
}

func drawEnergyIcon(ctx *render.Context, x, y float64, img image.Image, size float64) {
	ctx.Canvas.Push()
	defer ctx.Canvas.Pop()
	ctx.Canvas.Translate(x, y)
	ctx.Canvas.Scale(size/float64(img.Bounds().Dx()), size/float64(img.Bounds().Dy()))
	ctx.Canvas.DrawImage(0, 0, img, canvas.DPMM(1))
}
