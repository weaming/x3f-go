package x3f

import (
	"encoding/binary"
	"testing"
)

func TestGetCameraModelPrefersCAMMODELProperty(t *testing.T) {
	file := &File{
		Properties: &PropertyList{
			Properties: []Property{
				{
					Name:  "CAMMODEL",
					Value: "SIGMA SD14",
				},
			},
		},
		CAMFSection: &CAMFData{
			Entries: []*CAMFEntry{
				camfUint32Entry("CAMERAID", CameraIDDP3Q),
			},
		},
	}

	model := file.GetCameraModel()
	if model != "SIGMA SD14" {
		t.Fatalf("GetCameraModel() = %q, want %q", model, "SIGMA SD14")
	}
}

func TestDecodeCAMFType5UsesDecodingSizeAndBias(t *testing.T) {
	fullData := makeCAMFType5Header(3, 7)
	encodedData := make([]byte, 33)

	encodedData[0] = 1
	encodedData[1] = 0
	encodedData[2] = 0
	binary.LittleEndian.PutUint32(encodedData[28:32], 1)
	encodedData[32] = 0

	decoded, err := (&File{}).decodeCAMFType5(encodedData, fullData)
	if err != nil {
		t.Fatalf("decodeCAMFType5 failed: %v", err)
	}

	want := []byte{7, 7, 7}
	for i := range want {
		if decoded[i] != want[i] {
			t.Fatalf("decoded[%d] = %d, want %d", i, decoded[i], want[i])
		}
	}
}

func TestDecodeCAMFType5RejectsTruncatedPayload(t *testing.T) {
	fullData := makeCAMFType5Header(3, 7)
	encodedData := make([]byte, 33)

	encodedData[0] = 1
	encodedData[1] = 0
	encodedData[2] = 0
	binary.LittleEndian.PutUint32(encodedData[28:32], 2)

	_, err := (&File{}).decodeCAMFType5(encodedData, fullData)
	if err == nil {
		t.Fatalf("decodeCAMFType5 should reject truncated huffman payload")
	}
}

func makeCAMFType5Header(decodedDataSize, decodeBias uint32) []byte {
	fullData := make([]byte, CAMFHeaderSize)

	binary.LittleEndian.PutUint32(fullData[12:16], decodedDataSize)
	binary.LittleEndian.PutUint32(fullData[16:20], decodeBias)

	return fullData
}

func camfUint32Entry(name string, value uint32) *CAMFEntry {
	return &CAMFEntry{
		ID:             CMbM,
		Name:           name,
		MatrixElements: 1,
		MatrixDecoded:  []uint32{value},
	}
}
