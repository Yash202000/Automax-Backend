package services

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"

	"github.com/automax/backend/internal/config"
	_ "golang.org/x/image/webp"
)

// ImageValidationReason identifies why an image was accepted or rejected.
type ImageValidationReason string

const (
	ImageValidationOK          ImageValidationReason = "ok"
	ImageValidationMostlyBlack ImageValidationReason = "mostly_black"
	ImageValidationMostlyWhite ImageValidationReason = "mostly_white"
	ImageValidationLowDetail   ImageValidationReason = "low_detail"
	ImageValidationBlurry      ImageValidationReason = "blurry"
)

// analysisMaxDim is the longer-side pixel dimension every image is resized
// to (via area averaging, preserving aspect ratio) before analysis, so
// every image is compared on the same scale regardless of its original
// resolution. Changing this shifts what a given BlurVarianceThreshold means
// — the two are calibrated together (see ImageValidationConfig).
//
// Was 400, which made real-world blur detection ineffective: a modern phone
// photo (~3000-4000px wide) that is genuinely, visibly blurred (motion blur
// or missed focus) gets down-averaged 10x before the Laplacian ever sees
// it, which smooths away most of the blur signal along with the noise —
// only extreme, unmistakable blur cleared the BlurVarianceThreshold. Raised
// to 1600 so the sharpness check runs on a much less-diluted image; a
// checkerboard test pattern at 4000x3000 with a mild/moderate blur (box
// blur radius 5-20px) scores sharpness ~8-133 at this size, comfortably
// under BlurVarianceThreshold=250, while the same pattern sharp scores
// ~4495 — re-check against your own sample set if you see false
// positives/negatives.
const analysisMaxDim = 1600

// maxDecodePixels caps the pixel count of an image this service will fully
// decode/analyze, as a defense against decompression-bomb-style uploads (a
// small file that decodes to an enormous pixel grid).
const maxDecodePixels = 50_000_000 // ~50MP

// ImageValidationService decides whether an uploaded image shows any
// meaningful, in-focus detail — as opposed to being completely or mostly
// black, completely or mostly white, otherwise flat with no discernible
// content, or too blurry to make out what it shows. The image is divided
// into a grid of tiles and classified per-tile, so a defect covering only
// part of the frame (a partially covered lens, a corner blown out by
// flash) is judged on how much of the frame it actually covers rather than
// being diluted away by a single whole-image average.
type ImageValidationService interface {
	ValidateImageQuality(data []byte, cfg config.ImageValidationConfig) (ImageValidationReason, error)
}

type imageValidationService struct{}

func NewImageValidationService() ImageValidationService {
	return &imageValidationService{}
}

func (s *imageValidationService) ValidateImageQuality(data []byte, cfg config.ImageValidationConfig) (ImageValidationReason, error) {
	decodedConfig, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("decode image header: %w", err)
	}
	if pixels := decodedConfig.Width * decodedConfig.Height; pixels > maxDecodePixels {
		return "", fmt.Errorf("image dimensions %dx%d (%d px) exceed the %d px analysis limit", decodedConfig.Width, decodedConfig.Height, pixels, maxDecodePixels)
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("decode image: %w", err)
	}

	bounds := img.Bounds()
	if bounds.Dx() == 0 || bounds.Dy() == 0 {
		return "", fmt.Errorf("decoded image has zero dimensions")
	}

	grid := resizeGrayscaleArea(img, analysisMaxDim)

	tilesPerSide := cfg.TileGridSize
	if tilesPerSide < 1 {
		tilesPerSide = 1
	}
	tiles := computeTileStats(grid, tilesPerSide)

	return classify(tiles, cfg), nil
}

type tileStats struct {
	mean      float64
	stddev    float64
	sharpness float64
}

// computeTileStats partitions grid into tilesPerSide x tilesPerSide regions
// and computes brightness (mean/stddev) and sharpness (variance of
// Laplacian) independently within each region.
func computeTileStats(grid [][]float64, tilesPerSide int) []tileStats {
	h := len(grid)
	if h == 0 {
		return nil
	}
	w := len(grid[0])
	if w == 0 {
		return nil
	}

	tileCount := tilesPerSide * tilesPerSide
	sums := make([]float64, tileCount)
	sumSquares := make([]float64, tileCount)
	counts := make([]int, tileCount)
	lapSums := make([]float64, tileCount)
	lapSumSquares := make([]float64, tileCount)
	lapCounts := make([]int, tileCount)

	tileIndex := func(y, x int) int {
		ty := y * tilesPerSide / h
		if ty >= tilesPerSide {
			ty = tilesPerSide - 1
		}
		tx := x * tilesPerSide / w
		if tx >= tilesPerSide {
			tx = tilesPerSide - 1
		}
		return ty*tilesPerSide + tx
	}

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			idx := tileIndex(y, x)
			v := grid[y][x]
			sums[idx] += v
			sumSquares[idx] += v * v
			counts[idx]++
		}
	}

	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			idx := tileIndex(y, x)
			laplacian := grid[y-1][x] + grid[y+1][x] + grid[y][x-1] + grid[y][x+1] - 4*grid[y][x]
			lapSums[idx] += laplacian
			lapSumSquares[idx] += laplacian * laplacian
			lapCounts[idx]++
		}
	}

	stats := make([]tileStats, 0, tileCount)
	for i := 0; i < tileCount; i++ {
		if counts[i] == 0 {
			continue
		}
		mean := sums[i] / float64(counts[i])
		variance := sumSquares[i]/float64(counts[i]) - mean*mean
		if variance < 0 {
			variance = 0
		}
		stddev := math.Sqrt(variance)

		var sharpness float64
		if lapCounts[i] > 0 {
			lapMean := lapSums[i] / float64(lapCounts[i])
			lapVariance := lapSumSquares[i]/float64(lapCounts[i]) - lapMean*lapMean
			if lapVariance < 0 {
				lapVariance = 0
			}
			sharpness = lapVariance
		}

		stats = append(stats, tileStats{mean: mean, stddev: stddev, sharpness: sharpness})
	}
	return stats
}

// classify decides the overall verdict from the fraction of tiles that fall
// into each bad category, per the *CoverageFraction thresholds in cfg.
func classify(tiles []tileStats, cfg config.ImageValidationConfig) ImageValidationReason {
	total := len(tiles)
	if total == 0 {
		return ImageValidationOK
	}

	blackThreshold := float64(cfg.BlackMeanThreshold)
	whiteThreshold := float64(cfg.WhiteMeanThreshold)
	lowDetailThreshold := cfg.LowDetailStdDevThreshold
	blurThreshold := cfg.BlurVarianceThreshold

	var blackTiles, whiteTiles, lowDetailTiles, blurryTiles int
	for _, tile := range tiles {
		flat := tile.stddev <= lowDetailThreshold
		switch {
		case tile.mean <= blackThreshold && flat:
			blackTiles++
		case tile.mean >= whiteThreshold && flat:
			whiteTiles++
		case flat:
			lowDetailTiles++
		case tile.sharpness <= blurThreshold:
			// Only tiles with real brightness variation (not already flat)
			// count toward "blurry" — a naturally smooth region (sky, a
			// plain wall) has low edge variance too, but that's featurelessness,
			// not a camera focus problem, and is already covered by the
			// low-detail bucket above.
			blurryTiles++
		}
	}

	n := float64(total)
	switch {
	case float64(blackTiles)/n >= cfg.BlackCoverageFraction:
		return ImageValidationMostlyBlack
	case float64(whiteTiles)/n >= cfg.WhiteCoverageFraction:
		return ImageValidationMostlyWhite
	case float64(lowDetailTiles)/n >= cfg.LowDetailCoverageFraction:
		return ImageValidationLowDetail
	case float64(blurryTiles)/n >= cfg.BlurCoverageFraction:
		return ImageValidationBlurry
	default:
		return ImageValidationOK
	}
}

// resizeGrayscaleArea downsamples img to at most maxDim pixels on its longer
// side by AREA-AVERAGING every source pixel into its destination cell —
// deliberately not nearest-neighbor skip-sampling, which would let
// per-pixel sensor noise and JPEG block artifacts on a large photo
// masquerade as sharp edges and defeat the blur check.
func resizeGrayscaleArea(img image.Image, maxDim int) [][]float64 {
	bounds := img.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()

	dstW, dstH := srcW, srcH
	if srcW > maxDim || srcH > maxDim {
		if srcW >= srcH {
			dstW = maxDim
			dstH = srcH * maxDim / srcW
		} else {
			dstH = maxDim
			dstW = srcW * maxDim / srcH
		}
	}
	if dstW < 1 {
		dstW = 1
	}
	if dstH < 1 {
		dstH = 1
	}

	sums := make([][]float64, dstH)
	counts := make([][]int, dstH)
	for y := range sums {
		sums[y] = make([]float64, dstW)
		counts[y] = make([]int, dstW)
	}

	for y := 0; y < srcH; y++ {
		dy := y * dstH / srcH
		if dy >= dstH {
			dy = dstH - 1
		}
		for x := 0; x < srcW; x++ {
			dx := x * dstW / srcW
			if dx >= dstW {
				dx = dstW - 1
			}
			r, g, b, _ := img.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			// RGBA() returns 16-bit-scaled components; downscale to 8-bit
			// before applying the standard luminance formula.
			luminance := 0.299*float64(r>>8) + 0.587*float64(g>>8) + 0.114*float64(b>>8)
			sums[dy][dx] += luminance
			counts[dy][dx]++
		}
	}

	grid := make([][]float64, dstH)
	for y := 0; y < dstH; y++ {
		grid[y] = make([]float64, dstW)
		for x := 0; x < dstW; x++ {
			if counts[y][x] > 0 {
				grid[y][x] = sums[y][x] / float64(counts[y][x])
			}
		}
	}
	return grid
}
