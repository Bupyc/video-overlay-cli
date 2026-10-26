package cli

import (
	"bytes"
	"image"
	"testing"
)

func TestRGBAtoYUV420PMatchesReference(t *testing.T) {
	const width, height = 8, 6
	frame := image.NewRGBA(image.Rect(0, 0, width, height))
	for i := 3; i < len(frame.Pix); i += 4 {
		frame.Pix[i] = 255
	}
	for _, pixel := range []struct {
		x, y    int
		r, g, b uint8
	}{
		{2, 0, 255, 0, 0}, {3, 0, 0, 255, 0},
		{2, 1, 0, 0, 255}, {3, 1, 240, 240, 240},
		{6, 2, 21, 87, 193}, {7, 2, 250, 40, 4},
		{6, 3, 0, 0, 0}, {7, 3, 122, 3, 91},
		{0, 4, 23, 95, 204}, {1, 4, 255, 255, 255},
		{0, 5, 255, 0, 255}, {1, 5, 0, 255, 255},
	} {
		offset := frame.PixOffset(pixel.x, pixel.y)
		frame.Pix[offset], frame.Pix[offset+1], frame.Pix[offset+2] = pixel.r, pixel.g, pixel.b
	}
	original := append([]byte(nil), frame.Pix...)
	want := referenceRGBAtoYUV420P(frame)
	got := bytes.Repeat([]byte{0xa5}, len(want)+8)
	if err := rgbaToYUV420P(frame, got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got[:len(want)], want) {
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("byte %d: got %d, want %d", i, got[i], want[i])
			}
		}
	}
	if !bytes.Equal(frame.Pix, original) {
		t.Fatal("conversion modified the RGBA input")
	}
	if !bytes.Equal(got[len(want):], bytes.Repeat([]byte{0xa5}, 8)) {
		t.Fatal("conversion modified bytes beyond the YUV420P output")
	}
}

func TestRGBAtoYUV420PIgnoresAlpha(t *testing.T) {
	const width, height = 4, 4
	opaque := image.NewRGBA(image.Rect(0, 0, width, height))
	transparent := image.NewRGBA(image.Rect(0, 0, width, height))
	for i := 0; i < len(opaque.Pix); i += 4 {
		r, g, b := byte(i*3), byte(i*5), byte(i*7)
		opaque.Pix[i], opaque.Pix[i+1], opaque.Pix[i+2], opaque.Pix[i+3] = r, g, b, 255
		transparent.Pix[i], transparent.Pix[i+1], transparent.Pix[i+2], transparent.Pix[i+3] = r, g, b, byte(i*13)
	}
	outputSize := width * height * 3 / 2
	opaqueYUV, transparentYUV := make([]byte, outputSize), make([]byte, outputSize)
	if err := rgbaToYUV420P(opaque, opaqueYUV); err != nil {
		t.Fatal(err)
	}
	if err := rgbaToYUV420P(transparent, transparentYUV); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(opaqueYUV, transparentYUV) {
		t.Fatal("YUV420P output differs when only input alpha changes")
	}
}

func TestRGBAToBGRAInPlaceSwapsRedAndBlue(t *testing.T) {
	frame := image.NewRGBA(image.Rect(0, 0, 2, 1))
	copy(frame.Pix, []byte{1, 2, 3, 4, 10, 20, 30, 40})

	if err := rgbaToBGRAInPlace(frame); err != nil {
		t.Fatal(err)
	}

	want := []byte{3, 2, 1, 4, 30, 20, 10, 40}
	if !bytes.Equal(frame.Pix, want) {
		t.Fatalf("BGRA bytes = %v, want %v", frame.Pix, want)
	}
}

func BenchmarkRGBAtoYUV420P(b *testing.B) {
	const width, height = 1920, 1080
	source := image.NewRGBA(image.Rect(0, 0, width, height))
	for i := 3; i < len(source.Pix); i += 4 {
		source.Pix[i] = 255
	}
	for y := 200; y < 440; y++ {
		for x := 400; x < 1000; x++ {
			offset := source.PixOffset(x, y)
			source.Pix[offset], source.Pix[offset+1], source.Pix[offset+2] = 40, 180, 250
		}
	}
	output := make([]byte, width*height*3/2)
	b.SetBytes(int64(len(source.Pix)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := rgbaToYUV420P(source, output); err != nil {
			b.Fatal(err)
		}
	}
}

func referenceRGBAtoYUV420P(frame *image.RGBA) []byte {
	width, height := frame.Bounds().Dx(), frame.Bounds().Dy()
	pixels := width * height
	chromaPlaneSize := pixels / 4
	out := make([]byte, pixels+chromaPlaneSize*2)
	src := frame.Pix
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			offset := (y*width + x) * 4
			r, g, b := int(src[offset]), int(src[offset+1]), int(src[offset+2])
			out[y*width+x] = byte(((66*r + 129*g + 25*b + 128) >> 8) + 16)
			if y%2 != 0 || x%2 != 0 {
				continue
			}
			right, below := offset+4, offset+width*4
			belowRight := below + 4
			sumR := r + int(src[right]) + int(src[below]) + int(src[belowRight])
			sumG := g + int(src[right+1]) + int(src[below+1]) + int(src[belowRight+1])
			sumB := b + int(src[right+2]) + int(src[below+2]) + int(src[belowRight+2])
			chroma := pixels + (y/2)*(width/2) + x/2
			out[chroma] = byte(((-38*sumR - 74*sumG + 112*sumB + 512) >> 10) + 128)
			out[pixels+chromaPlaneSize+(y/2)*(width/2)+x/2] = byte(((112*sumR - 94*sumG - 18*sumB + 512) >> 10) + 128)
		}
	}
	return out
}
