package output

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/weaming/x3f-go/x3f"
)

func TestCreateDNGFileCreatesParentDirectories(t *testing.T) {
	outputPath := filepath.Join(t.TempDir(), "nested", "dir", "x.dng")

	file, err := createDNGFile(outputPath)
	if err != nil {
		t.Fatalf("createDNGFile failed: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close DNG file failed: %v", err)
	}

	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("DNG file was not created: %v", err)
	}
}

func TestCompressLinearSRGBHighlightLeavesInGamutPixelUnchanged(t *testing.T) {
	input := x3f.Vector3{0.4, 0.6, 0.8}
	if got := compressLinearSRGBHighlight(input); got != input {
		t.Fatalf("in-gamut pixel changed: got %v want %v", got, input)
	}
}

func TestCompressLinearSRGBHighlightNeutralizesBrightOutOfGamutPixel(t *testing.T) {
	input := x3f.Vector3{2.28581644, 1.38755836, 1.90238677}
	got := compressLinearSRGBHighlight(input)

	for channel := 0; channel < 3; channel++ {
		if got[channel] < 0 || got[channel] > 1 {
			t.Fatalf("channel %d remains out of gamut: %v", channel, got)
		}
	}

	if got[0] < 0.9 || got[1] < 0.9 || got[2] < 0.9 {
		t.Fatalf("bright highlight should become near-neutral: %v", got)
	}
	if got[0] != got[1] || got[1] != got[2] {
		t.Fatalf("bright highlight should be neutral: %v", got)
	}
}
