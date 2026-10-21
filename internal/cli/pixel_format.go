package cli

import (
	"encoding/binary"
	"fmt"
	"image"
)

func rgbaToBGRAInPlace(frame *image.RGBA) error {
	width := frame.Bounds().Dx()
	if frame.Rect.Min != (image.Point{}) || frame.Stride != width*4 {
		return fmt.Errorf("frame is not a tightly packed RGBA image")
	}
	for i := 0; i < len(frame.Pix); i += 4 {
		frame.Pix[i], frame.Pix[i+2] = frame.Pix[i+2], frame.Pix[i]
	}
	return nil
}

func rgbaToYUV420PInPlace(frame *image.RGBA, scratch []byte) error {
	width, height := frame.Bounds().Dx(), frame.Bounds().Dy()
	if frame.Rect.Min != (image.Point{}) || frame.Stride != width*4 || width%2 != 0 || height%2 != 0 {
		return fmt.Errorf("frame is not a tightly packed even-sized RGBA image")
	}
	pixels := width * height
	chromaPlaneSize := pixels / 4
	if len(scratch) < width+chromaPlaneSize*2 {
		return fmt.Errorf("YUV420P scratch buffer is too small")
	}
	yScratch := scratch[:width]
	uPlane := scratch[width : width+chromaPlaneSize]
	vPlane := scratch[width+chromaPlaneSize : width+chromaPlaneSize*2]
	pix := frame.Pix
	for y := 0; y < height; y += 2 {
		srcRow0 := y * frame.Stride
		srcRow1 := srcRow0 + frame.Stride
		yRow0 := y * width
		yRow1 := yRow0 + width
		chroma := (y / 2) * (width / 2)
		for x := 0; x < width; x += 2 {
			src00 := srcRow0 + x*4
			row0 := binary.LittleEndian.Uint64(pix[src00 : src00+8])
			row1 := binary.LittleEndian.Uint64(pix[srcRow1+x*4 : srcRow1+x*4+8])
			if (row0|row1)&0x00ffffff00ffffff == 0 {
				pix[yRow0+x], pix[yRow0+x+1] = 16, 16
				yScratch[x], yScratch[x+1] = 16, 16
				uPlane[chroma], vPlane[chroma] = 128, 128
				chroma++
				continue
			}
			p00, p01 := uint32(row0), uint32(row0>>32)
			p10, p11 := uint32(row1), uint32(row1>>32)
			r0, g0, b0 := int(byte(p00)), int(byte(p00>>8)), int(byte(p00>>16))
			r1, g1, b1 := int(byte(p01)), int(byte(p01>>8)), int(byte(p01>>16))
			r2, g2, b2 := int(byte(p10)), int(byte(p10>>8)), int(byte(p10>>16))
			r3, g3, b3 := int(byte(p11)), int(byte(p11>>8)), int(byte(p11>>16))
			pix[yRow0+x] = byte(((66*r0 + 129*g0 + 25*b0 + 128) >> 8) + 16)
			pix[yRow0+x+1] = byte(((66*r1 + 129*g1 + 25*b1 + 128) >> 8) + 16)
			yScratch[x] = byte(((66*r2 + 129*g2 + 25*b2 + 128) >> 8) + 16)
			yScratch[x+1] = byte(((66*r3 + 129*g3 + 25*b3 + 128) >> 8) + 16)
			sumR, sumG, sumB := r0+r1+r2+r3, g0+g1+g2+g3, b0+b1+b2+b3
			uPlane[chroma] = byte(((-38*sumR - 74*sumG + 112*sumB + 512) >> 10) + 128)
			vPlane[chroma] = byte(((112*sumR - 94*sumG - 18*sumB + 512) >> 10) + 128)
			chroma++
		}
		copy(pix[yRow1:yRow1+width], yScratch)
	}
	copy(pix[pixels:pixels+chromaPlaneSize], uPlane)
	copy(pix[pixels+chromaPlaneSize:pixels+chromaPlaneSize*2], vPlane)
	return nil
}
