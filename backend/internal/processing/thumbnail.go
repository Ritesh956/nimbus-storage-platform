package processing

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	_ "image/gif" // decoder registration for image.Decode
	"image/jpeg"
	_ "image/png" // decoder registration for image.Decode
	"log/slog"
	"sync"
	"time"

	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
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

// pdfium is initialized lazily and exactly once: the WebAssembly runtime
// (pdfium compiled to WASM, executed via wazero — no cgo, so the existing
// CGO_ENABLED=0 builds keep working) takes noticeable time and memory to
// instantiate, and a worker that never sees a PDF shouldn't pay for it.
// Pool of one: thumbnail processing is sequential (one NATS message at a
// time), so a second instance would only burn memory.
var (
	pdfiumOnce sync.Once
	pdfiumPool pdfium.Pool
	pdfiumErr  error
)

func pdfiumInstance() (pdfium.Pdfium, error) {
	pdfiumOnce.Do(func() {
		pdfiumPool, pdfiumErr = webassembly.Init(webassembly.Config{MinIdle: 1, MaxIdle: 1, MaxTotal: 1})
	})
	if pdfiumErr != nil {
		return nil, pdfiumErr
	}
	return pdfiumPool.GetInstance(30 * time.Second)
}

// WarmupPDFium pre-compiles the pdfium WASM module in the background at
// worker startup. Observed on the first live PDF: cold compilation took
// longer than the NATS ack window, so the event was redelivered and
// processed three times — idempotent (same thumbnail key rewritten) but
// wasteful. Warming up front-loads that cost to boot time.
func WarmupPDFium(logger *slog.Logger) {
	start := time.Now()
	instance, err := pdfiumInstance()
	if err != nil {
		logger.Warn("pdfium warmup failed; first PDF thumbnail will pay the init cost", "error", err)
		return
	}
	instance.Close()
	logger.Info("pdfium warmed up", "took", time.Since(start).Round(time.Millisecond))
}

// generatePDFThumbnail renders the PDF's first page (fit within
// maxThumbnailDimension, aspect preserved) and encodes it as JPEG — real
// page rendering, replacing the Day 9 solid-color placeholder (which
// survives below only as the fallback for PDFs pdfium can't parse). The
// render is composited onto white first: PDF pages have no intrinsic
// background and JPEG has no alpha, so encoding the raw RGBA directly
// would turn transparent paper black.
func generatePDFThumbnail(data []byte) ([]byte, error) {
	instance, err := pdfiumInstance()
	if err != nil {
		return nil, fmt.Errorf("pdfium init: %w", err)
	}
	defer instance.Close() // returns the instance to the pool

	doc, err := instance.OpenDocument(&requests.OpenDocument{File: &data})
	if err != nil {
		return nil, fmt.Errorf("open pdf: %w", err)
	}
	defer instance.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: doc.Document}) //nolint:errcheck

	render, err := instance.RenderPageInPixels(&requests.RenderPageInPixels{
		Page:   requests.Page{ByIndex: &requests.PageByIndex{Document: doc.Document, Index: 0}},
		Width:  maxThumbnailDimension,
		Height: maxThumbnailDimension,
	})
	if err != nil {
		return nil, fmt.Errorf("render pdf page: %w", err)
	}

	src := render.Result.Image
	dst := image.NewRGBA(src.Bounds())
	draw.Draw(dst, dst.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), src, src.Bounds().Min, draw.Over)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 80}); err != nil {
		return nil, fmt.Errorf("encode pdf thumbnail: %w", err)
	}
	return buf.Bytes(), nil
}

// generatePDFPlaceholder produces a fixed solid-color stand-in image —
// originally the Day 9 stand-in for all PDFs, now only the fallback when
// generatePDFThumbnail fails on a corrupt/unsupported PDF (the pipeline
// still completes end to end either way).
func generatePDFPlaceholder() ([]byte, error) {
	dst := image.NewRGBA(image.Rect(0, 0, maxThumbnailDimension, maxThumbnailDimension))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{C: color.RGBA{R: 200, G: 60, B: 60, A: 255}}, image.Point{}, draw.Src)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 80}); err != nil {
		return nil, fmt.Errorf("encode placeholder: %w", err)
	}
	return buf.Bytes(), nil
}
