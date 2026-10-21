package widgets

import (
	"fmt"
	"image"
	"image/draw"
	"math"
	"os"
	"path/filepath"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/rasterizer"
	"video-overlay-cli/assets/icons"
	"video-overlay-cli/internal/layout"
	"video-overlay-cli/internal/render"
)

func iconFactory(element *layout.Element, setup *render.Context) (render.Drawer, error) {
	name := attr(element, "file", "")
	if name == "" {
		return nil, fmt.Errorf("icon component requires file")
	}
	img, err := readIcon(setup.AssetDir, name)
	if err != nil {
		return nil, err
	}
	size, err := floatAttr(element, "size", 64)
	if err != nil || size <= 0 {
		return nil, fmt.Errorf("icon size must be positive")
	}
	invert := attr(element, "invert", "true") == "true"
	if invert {
		img = invertImage(img)
	}
	if size == math.Trunc(size) && size <= 4096 {
		img = scaleIcon(img, int(size))
		return func(ctx *render.Context) error {
			drawImage(ctx, 0, 0, img)
			return nil
		}, nil
	}
	return func(ctx *render.Context) error {
		ctx.Canvas.Push()
		defer ctx.Canvas.Pop()
		ctx.Canvas.Scale(size/float64(img.Bounds().Dx()), size/float64(img.Bounds().Dy()))
		ctx.Canvas.DrawImage(0, 0, img, canvas.DPMM(1))
		return nil
	}, nil
}

func scaleIcon(source image.Image, size int) *image.RGBA {
	scaled := image.NewRGBA(image.Rect(0, 0, size, size))
	raster := rasterizer.FromImage(scaled, canvas.DPMM(1), nil)
	context := canvas.NewContext(raster)
	context.SetCoordSystem(canvas.CartesianIV)
	context.Scale(float64(size)/float64(source.Bounds().Dx()), float64(size)/float64(source.Bounds().Dy()))
	context.DrawImage(0, 0, source, canvas.DPMM(1))
	raster.Close()
	return scaled
}

func readIcon(assetDir, name string) (image.Image, error) {
	path := name
	if !filepath.IsAbs(path) && assetDir != "" {
		path = filepath.Join(assetDir, name)
	}
	if file, err := os.Open(path); err == nil {
		defer file.Close()
		image, _, err := image.Decode(file)
		if err != nil {
			return nil, fmt.Errorf("decode icon %q: %w", name, err)
		}
		return image, nil
	}
	image, err := icons.Read(name)
	if err != nil {
		return nil, fmt.Errorf("load icon %q: %w", name, err)
	}
	return image, nil
}

func invertImage(source image.Image) image.Image {
	result := image.NewRGBA(source.Bounds())
	draw.Draw(result, result.Bounds(), source, source.Bounds().Min, draw.Src)
	for y := result.Bounds().Min.Y; y < result.Bounds().Max.Y; y++ {
		for x := result.Bounds().Min.X; x < result.Bounds().Max.X; x++ {
			at := result.PixOffset(x, y)
			result.Pix[at], result.Pix[at+1], result.Pix[at+2] = 255-result.Pix[at], 255-result.Pix[at+1], 255-result.Pix[at+2]
		}
	}
	return result
}

func gpsIcon(element *layout.Element, setup *render.Context) (render.Drawer, error) {
	size, err := floatAttr(element, "size", 64)
	if err != nil || size <= 0 {
		return nil, fmt.Errorf("gps_lock_icon size must be positive")
	}
	files := []string{
		attr(element, "lock_unknown", "gps_lock_unknown.png"),
		attr(element, "lock_none", "gps_lock_none.png"),
		attr(element, "lock_2d", "gps_lock_2d.png"),
		attr(element, "lock_3d", "gps_lock_3d.png"),
	}
	images := make([]image.Image, len(files))
	for i, name := range files {
		images[i], err = readIcon(setup.AssetDir, name)
		if err != nil {
			return nil, err
		}
	}
	return func(ctx *render.Context) error {
		fix := 0
		if ctx.Sample.GPSFix.Valid {
			fix = int(ctx.Sample.GPSFix.Number)
		}
		index := 0
		if fix >= 0 && fix < len(images) {
			index = fix
		}
		image := images[index]
		ctx.Canvas.Push()
		defer ctx.Canvas.Pop()
		ctx.Canvas.Scale(size/float64(image.Bounds().Dx()), size/float64(image.Bounds().Dy()))
		ctx.Canvas.DrawImage(0, 0, image, canvas.DPMM(1))
		return nil
	}, nil
}
