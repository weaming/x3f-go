package x3f

import (
	"encoding/binary"
	"testing"
)

func TestCalculateBlackLevelUsesQuattroTopLayerForThirdChannel(t *testing.T) {
	file := &File{
		CAMFSection: &CAMFData{
			Entries: []*CAMFEntry{
				camfRectEntry("KeepImageArea", 0, 0, 3, 3),
				camfRectEntry("DarkShieldTop", 0, 0, 3, 0),
			},
		},
	}

	section := &ImageSection{
		DecodedColumns: 2,
		DecodedRows:    2,
		DecodedData:    []uint16{10, 20, 0, 30, 40, 0, 100, 200, 0, 300, 400, 0},
		QuattroTopCols: 4,
		QuattroTopRows: 4,
		QuattroTopData: []uint16{1000, 2000, 3000, 4000, 9, 9, 9, 9, 8, 8, 8, 8, 7, 7, 7, 7},
	}

	blackLevel, err := CalculateBlackLevel(file, section)
	if err != nil {
		t.Fatalf("CalculateBlackLevel failed: %v", err)
	}

	if blackLevel.Level[0] != 20 {
		t.Fatalf("black level channel 0 = %f, want 20", blackLevel.Level[0])
	}
	if blackLevel.Level[1] != 30 {
		t.Fatalf("black level channel 1 = %f, want 30", blackLevel.Level[1])
	}
	if blackLevel.Level[2] != 2500 {
		t.Fatalf("black level channel 2 = %f, want 2500", blackLevel.Level[2])
	}
}

func camfRectEntry(name string, x0, y0, x1, y1 uint32) *CAMFEntry {
	matrixData := make([]byte, 16)
	values := []uint32{x0, y0, x1, y1}
	for i, value := range values {
		binary.LittleEndian.PutUint32(matrixData[i*4:], value)
	}

	return &CAMFEntry{
		ID:             CMbM,
		Name:           name,
		MatrixElements: 4,
		MatrixData:     matrixData,
	}
}
