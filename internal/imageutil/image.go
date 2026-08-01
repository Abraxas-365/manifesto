package imageutil

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	xdraw "golang.org/x/image/draw"
)

// SupportedImageExts maps file extensions to MIME types.
var SupportedImageExts = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
}

// IsImageFile returns true if path has a supported image extension.
func IsImageFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	_, ok := SupportedImageExts[ext]
	return ok
}

// imageTargetBytes is the target raw size for images sent to the API.
// Base64 encoding adds ~33% overhead, so 3.75MB raw stays under the 5MB base64 limit.
const imageTargetBytes = 3_750_000

// imageMaxDimension is the maximum pixel width or height allowed before
// downscaling. Anthropic's hard limit is 2000px for many-image requests;
// we use 1568px (their recommended optimal size) to stay safely within limits.
const imageMaxDimension = 1568

// resizeImage scales img down so neither dimension exceeds imageMaxDimension.
// If both dimensions are within limit, returns img unchanged. Never upscales.
func resizeImage(img image.Image) image.Image {
	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()

	if w <= imageMaxDimension && h <= imageMaxDimension {
		return img
	}

	scaleW := float64(imageMaxDimension) / float64(w)
	scaleH := float64(imageMaxDimension) / float64(h)
	scale := scaleW
	if scaleH < scaleW {
		scale = scaleH
	}

	newW := int(float64(w) * scale)
	newH := int(float64(h) * scale)
	if newW < 1 {
		newW = 1
	}
	if newH < 1 {
		newH = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	xdraw.BiLinear.Scale(dst, dst.Bounds(), img, bounds, draw.Over, nil)
	return dst
}

// encodePNG encodes img as a PNG and returns the bytes.
func encodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.DefaultCompression}
	if err := enc.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// encodeJPEG encodes img as JPEG at the given quality and returns the bytes.
func encodeJPEG(img image.Image, quality int) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// CompressImage reduces image size using a multi-strategy pipeline that mirrors
// claude-code's approach:
//  1. Return original if already within size and dimension limits.
//  2. For PNGs within dimension limits but oversized: try PNG compression first
//     (preserves transparency), then fall through to JPEG.
//  3. JPEG quality ladder: 80 → 60 → 40 → 20.
//  4. If dimensions also exceed the limit, downscale first then repeat the ladder.
//  5. Last resort: downscale to 1000px and encode JPEG at quality 20.
//
// Returns the (possibly compressed) bytes and the resulting media type.
func CompressImage(raw []byte, originalMediaType string) ([]byte, string) {
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		// Can't decode — return as-is.
		return raw, originalMediaType
	}

	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	needsResize := w > imageMaxDimension || h > imageMaxDimension
	isPNG := strings.Contains(originalMediaType, "png")

	// Fast path: already within all limits.
	if !needsResize && len(raw) <= imageTargetBytes {
		return raw, originalMediaType
	}

	// --- Strategy 1: PNG compression (only when no resize needed and source is PNG) ---
	if isPNG && !needsResize {
		if b, err := encodePNG(img); err == nil && len(b) <= imageTargetBytes {
			return b, "image/png"
		}
	}

	// --- Strategy 2: JPEG quality ladder (no resize needed) ---
	if !needsResize {
		for _, q := range []int{80, 60, 40, 20} {
			if b, err := encodeJPEG(img, q); err == nil && len(b) <= imageTargetBytes {
				return b, "image/jpeg"
			}
		}
	}

	// --- Strategy 3: Downscale then try PNG (PNG source only) ---
	resized := resizeImage(img)
	if isPNG {
		if b, err := encodePNG(resized); err == nil && len(b) <= imageTargetBytes {
			return b, "image/png"
		}
	}

	// --- Strategy 4: Downscale then JPEG quality ladder ---
	for _, q := range []int{80, 60, 40, 20} {
		if b, err := encodeJPEG(resized, q); err == nil && len(b) <= imageTargetBytes {
			return b, "image/jpeg"
		}
	}

	// --- Strategy 5: Last resort — shrink to 1000px and compress hard ---
	const lastResortDim = 1000
	bounds2 := resized.Bounds()
	rw, rh := bounds2.Dx(), bounds2.Dy()
	if rw > lastResortDim || rh > lastResortDim {
		scaleW := float64(lastResortDim) / float64(rw)
		scaleH := float64(lastResortDim) / float64(rh)
		scale := scaleW
		if scaleH < scaleW {
			scale = scaleH
		}
		nw := max(1, int(float64(rw)*scale))
		nh := max(1, int(float64(rh)*scale))
		small := image.NewRGBA(image.Rect(0, 0, nw, nh))
		xdraw.BiLinear.Scale(small, small.Bounds(), resized, resized.Bounds(), draw.Over, nil)
		resized = small
	}
	if b, err := encodeJPEG(resized, 20); err == nil {
		return b, "image/jpeg"
	}

	return raw, originalMediaType
}

// ReadImageFile reads an image from disk and returns its base64 data and MIME type.
// Images larger than imageTargetBytes are compressed before encoding.
func ReadImageFile(path string) (data string, mediaType string, err error) {
	ext := strings.ToLower(filepath.Ext(path))
	mt, ok := SupportedImageExts[ext]
	if !ok {
		return "", "", fmt.Errorf("unsupported image format: %s", ext)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return "", "", fmt.Errorf("reading image: %w", err)
	}

	// Claude API hard limit: ~5MB base64 (~3.75MB raw)
	const maxSize = 3_750_000

	compressed, mt := CompressImage(raw, mt)
	if len(compressed) > maxSize {
		return "", "", fmt.Errorf("image too large (%s, max ~3.75MB)", HumanFileSize(len(compressed)))
	}

	return base64.StdEncoding.EncodeToString(compressed), mt, nil
}

// HumanFileSize returns a human-readable size string.
func HumanFileSize(n int) string {
	switch {
	case n >= 1024*1024:
		return fmt.Sprintf("%.1fMB", float64(n)/(1024*1024))
	case n >= 1024:
		return fmt.Sprintf("%.1fkB", float64(n)/1024)
	default:
		return fmt.Sprintf("%dB", n)
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
