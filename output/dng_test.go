package output

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/weaming/x3f-go/x3f"
)

func TestBuildOpenEXRHeaderDeclaresACEScg(t *testing.T) {
	header := buildOpenEXRHeader(4, 3)
	for _, attribute := range [][]byte{
		[]byte("chromaticities\x00"),
		[]byte("acesImageContainerFlag\x00"),
		[]byte("scene-linear ACEScg (AP1)"),
	} {
		if !bytes.Contains(header, attribute) {
			t.Fatalf("OpenEXR header is missing %q", attribute)
		}
	}
}

func TestFloat32PixelsToBytesPreservesHDRValues(t *testing.T) {
	pixels := []float32{-0.25, 1, 2.5}
	data := float32PixelsToBytes(pixels)

	if len(data) != len(pixels)*4 {
		t.Fatalf("byte length mismatch: got %d want %d", len(data), len(pixels)*4)
	}

	for i, want := range pixels {
		got := math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
		if got != want {
			t.Fatalf("pixel %d mismatch: got %v want %v", i, got, want)
		}
	}
}

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
