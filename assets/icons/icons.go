package icons

import (
	"embed"
	"fmt"
	"image"
	_ "image/png"
	"path"
)

//go:embed *.png
var files embed.FS

func Read(name string) (image.Image, error) {
	file, err := files.Open(path.Base(name))
	if err != nil {
		return nil, fmt.Errorf("read embedded icon %q: %w", name, err)
	}
	defer file.Close()
	image, _, err := image.Decode(file)
	if err != nil {
		return nil, fmt.Errorf("decode embedded icon %q: %w", name, err)
	}
	return image, nil
}
