package services

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/automax/backend/internal/config"
)

func testImageValidationConfig() config.ImageValidationConfig {
	return config.ImageValidationConfig{
		BlackMeanThreshold:        20,
		WhiteMeanThreshold:        235,
		LowDetailStdDevThreshold:  12,
		BlurVarianceThreshold:     250,
		TileGridSize:              8,
		BlackCoverageFraction:     0.5,
		WhiteCoverageFraction:     0.5,
		LowDetailCoverageFraction: 0.6,
		BlurCoverageFraction:      0.6,
	}
}

func encodeTestPNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func solidTestImage(w, h int, c color.Color) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

// checkerboardTestImage builds a high-frequency pattern at a typical
// phone-camera resolution, used to exercise the blur check: real cameras
// produce photos in this size range, and the sharpness score is only
// meaningful relative to analysisMaxDim at that scale (see the
// analysisMaxDim comment in image_validation_service.go for why a fixed
// small analysis size made blur detection ineffective on real photos).
func checkerboardTestImage(w, h, cell int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if ((x/cell)+(y/cell))%2 == 0 {
				img.Set(x, y, color.RGBA{20, 20, 20, 255})
			} else {
				img.Set(x, y, color.RGBA{230, 230, 230, 255})
			}
		}
	}
	return img
}

type blurPixel struct{ r, g, b float64 }

// boxBlurTestImage applies a separable box blur (row pass then column
// pass, each a sliding-window running sum) so it runs fast enough at
// 4000x3000 in a unit test regardless of radius.
func boxBlurTestImage(img *image.RGBA, radius int) *image.RGBA {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	src := make([]blurPixel, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			src[y*w+x] = blurPixel{float64(r >> 8), float64(g >> 8), float64(b >> 8)}
		}
	}

	clampTo := func(i, n int) int {
		if i < 0 {
			return 0
		}
		if i >= n {
			return n - 1
		}
		return i
	}

	rowBlurred := make([]blurPixel, w*h)
	for y := 0; y < h; y++ {
		row := src[y*w : y*w+w]
		var sr, sg, sb float64
		for x := -radius; x <= radius; x++ {
			p := row[clampTo(x, w)]
			sr, sg, sb = sr+p.r, sg+p.g, sb+p.b
		}
		n := float64(2*radius + 1)
		for x := 0; x < w; x++ {
			rowBlurred[y*w+x] = blurPixel{sr / n, sg / n, sb / n}
			add, rem := row[clampTo(x+radius+1, w)], row[clampTo(x-radius, w)]
			sr += add.r - rem.r
			sg += add.g - rem.g
			sb += add.b - rem.b
		}
	}

	fullyBlurred := make([]blurPixel, w*h)
	for x := 0; x < w; x++ {
		var sr, sg, sb float64
		for y := -radius; y <= radius; y++ {
			p := rowBlurred[clampTo(y, h)*w+x]
			sr, sg, sb = sr+p.r, sg+p.g, sb+p.b
		}
		n := float64(2*radius + 1)
		for y := 0; y < h; y++ {
			fullyBlurred[y*w+x] = blurPixel{sr / n, sg / n, sb / n}
			add, rem := rowBlurred[clampTo(y+radius+1, h)*w+x], rowBlurred[clampTo(y-radius, h)*w+x]
			sr += add.r - rem.r
			sg += add.g - rem.g
			sb += add.b - rem.b
		}
	}

	out := image.NewRGBA(bounds)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			p := fullyBlurred[y*w+x]
			out.Set(x, y, color.RGBA{uint8(p.r), uint8(p.g), uint8(p.b), 255})
		}
	}
	return out
}

func TestValidateImageQuality_MostlyBlack(t *testing.T) {
	svc := NewImageValidationService()
	data := encodeTestPNG(t, solidTestImage(1200, 900, color.RGBA{0, 0, 0, 255}))
	res, err := svc.ValidateImageQuality(data, testImageValidationConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != ImageValidationMostlyBlack {
		t.Errorf("got %v, want %v", res, ImageValidationMostlyBlack)
	}
}

func TestValidateImageQuality_MostlyWhite(t *testing.T) {
	svc := NewImageValidationService()
	data := encodeTestPNG(t, solidTestImage(1200, 900, color.RGBA{255, 255, 255, 255}))
	res, err := svc.ValidateImageQuality(data, testImageValidationConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != ImageValidationMostlyWhite {
		t.Errorf("got %v, want %v", res, ImageValidationMostlyWhite)
	}
}

func TestValidateImageQuality_SharpPhotoPasses(t *testing.T) {
	svc := NewImageValidationService()
	sharp := checkerboardTestImage(4000, 3000, 100)
	data := encodeTestPNG(t, sharp)
	res, err := svc.ValidateImageQuality(data, testImageValidationConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != ImageValidationOK {
		t.Errorf("got %v, want %v", res, ImageValidationOK)
	}
}

// TestValidateImageQuality_BlurredPhotoRejected guards against the
// regression where a real phone-camera-resolution photo (~4000x3000) with
// clearly visible motion/focus blur was scored "ok" because the fixed,
// too-small analysisMaxDim diluted the blur away before the sharpness
// check ever saw it.
func TestValidateImageQuality_BlurredPhotoRejected(t *testing.T) {
	svc := NewImageValidationService()
	sharp := checkerboardTestImage(4000, 3000, 100)
	blurred := boxBlurTestImage(sharp, 10)
	data := encodeTestPNG(t, blurred)
	res, err := svc.ValidateImageQuality(data, testImageValidationConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != ImageValidationBlurry {
		t.Errorf("got %v, want %v", res, ImageValidationBlurry)
	}
}
