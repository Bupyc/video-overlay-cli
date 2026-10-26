package render

import (
	"image"
	"image/color"

	"github.com/srwiley/scanx"
	"github.com/tdewolff/canvas"
)

type pathFillRenderer struct {
	image   *image.RGBA
	scanner *scanx.Scanner
}

type pathFillSpanner struct {
	*scanx.ImgSpanner
	span scanx.SpanFunc
}

func (s *pathFillSpanner) GetSpanFunc() scanx.SpanFunc {
	return s.span
}

func newPathFillRenderer(frame *image.RGBA) *pathFillRenderer {
	spanner := scanx.NewImgSpanner(frame)
	scannerSpanner := &pathFillSpanner{ImgSpanner: spanner, span: spanner.SpanFgColor}
	return &pathFillRenderer{
		image:   frame,
		scanner: scanx.NewScanner(scannerSpanner, frame.Bounds().Dx(), frame.Bounds().Dy()),
	}
}

// DrawOwnedFilledPath transforms path in place; the caller must discard or rebuild it afterward.
func (ctx *Context) DrawOwnedFilledPath(x, y float64, path *canvas.Path, fill color.Color) bool {
	if ctx == nil || ctx.Canvas == nil || ctx.pathFill == nil || path == nil || fill == nil {
		return false
	}
	ctx.pathFill.draw(ctx.Canvas, x, y, path, fill)
	return true
}

func (r *pathFillRenderer) draw(canvasContext *canvas.Context, x, y float64, path *canvas.Path, fill color.Color) {
	coord := canvasContext.CoordView().Dot(canvas.Point{X: x, Y: y})
	matrix := canvasContext.CoordSystemView().Mul(canvasContext.View()).Translate(coord.X, coord.Y)
	path.Transform(matrix)

	r.scanner.Clear()
	r.scanner.SetWinding(canvasContext.Style.FillRule == canvas.NonZero)
	r.scanner.SetColor(canvas.DefaultColorSpace.ToLinear(fill))
	path.ToScanxScanner(r.scanner, float64(r.image.Bounds().Dy()), canvas.DPMM(1))
	r.scanner.Draw()
}
