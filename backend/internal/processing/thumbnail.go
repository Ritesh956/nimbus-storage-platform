package processing

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	_ "image/gif" // decoder registration for image.Decode
	"image/jpeg"
	_ "image/png" // decoder registration for image.Decode

	"golang.org/x/image/draw"
)

// generateImageThumbnail decodes an image (jpeg/png/gif — registered above
// via blank imports) and re-encodes a scaled-down JPEG, longer edge capped
// at maxThumbnailDimension. This is real thumbnailing, not a stub.
func generateImageThumbnail(data []byte) ([]byte, error) {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}

	bounds := src.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w == 0 || h == 0 {
		return nil, fmt.Errorf("zero-sized image")
	}

	scale := float64(maxThumbnailDimension) / float64(max(w, h))
	if scale > 1 {
		scale = 1 // never upscale
	}
	dstW, dstH := max(int(float64(w)*scale), 1), max(int(float64(h)*scale), 1)

	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, bounds, draw.Over, nil)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 80}); err != nil {
		return nil, fmt.Errorf("encode thumbnail: %w", err)
	}
	return buf.Bytes(), nil
}

// generatePDFPlaceholder produces a fixed solid-color stand-in image.
// True PDF page rendering needs a system tool (poppler/ghostscript) that's
// out of scope for this project — a documented simplification, not an
// oversight (docs/09-roadmap.md Day 9): it still exercises the full
// pipeline (storage write, thumbnail_key update, activity event) end to
// end, just without a real rendered preview.
func generatePDFPlaceholder() ([]byte, error) {
	dst := image.NewRGBA(image.Rect(0, 0, maxThumbnailDimension, maxThumbnailDimension))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{C: color.RGBA{R: 200, G: 60, B: 60, A: 255}}, image.Point{}, draw.Src)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 80}); err != nil {
		return nil, fmt.Errorf("encode placeholder: %w", err)
	}
	return buf.Bytes(), nil
}
