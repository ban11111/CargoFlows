package app

import (
	"image"
	"image/color"
	"testing"
)

func TestStyleDerivativeRemovesEveryMaskedSourcePixel(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	source.Set(0, 0, color.NRGBA{R: 250, A: 255})
	source.Set(1, 0, color.NRGBA{G: 240, A: 255})
	mask := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	mask.Set(0, 0, color.NRGBA{A: 0})
	mask.Set(1, 0, color.NRGBA{A: 255})
	derived, changed, protected := neutralizeMaskedProduct(source, mask)
	if !changed || !protected {
		t.Fatal("valid exclusion mask rejected")
	}
	removed := derived.NRGBAAt(0, 0)
	if removed.R == 250 || removed.A != 255 {
		t.Fatalf("masked product pixel leaked: %#v", removed)
	}
	retained := derived.NRGBAAt(1, 0)
	if retained.G != 240 {
		t.Fatalf("style pixel changed: %#v", retained)
	}
}

func TestStyleDerivativeRedactsMaskBoundingBoxToAvoidSilhouetteLeak(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 5, 5))
	mask := image.NewNRGBA(image.Rect(0, 0, 5, 5))
	for y := 0; y < 5; y++ {
		for x := 0; x < 5; x++ {
			source.Set(x, y, color.NRGBA{R: uint8(20*x + y), G: 80, A: 255})
			mask.Set(x, y, color.NRGBA{A: 255})
		}
	}
	mask.Set(1, 1, color.NRGBA{A: 0})
	mask.Set(3, 3, color.NRGBA{A: 0})

	derived, changed, protected := neutralizeMaskedProduct(source, mask)
	if !changed || !protected {
		t.Fatal("valid exclusion mask rejected")
	}
	for y := 1; y <= 3; y++ {
		for x := 1; x <= 3; x++ {
			if pixel := derived.NRGBAAt(x, y); pixel.R != 128 || pixel.G != 128 || pixel.B != 128 {
				t.Fatalf("product bounding box leaked at %d,%d: %#v", x, y, pixel)
			}
		}
	}
	if outside := derived.NRGBAAt(0, 0); outside.G != 80 {
		t.Fatalf("surrounding style pixel changed: %#v", outside)
	}
}
