package output

import (
	"os"
	"path/filepath"
	"testing"
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
