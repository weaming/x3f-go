package x3f

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestQuattroFormatIncludesSDQ(t *testing.T) {
	formats := []uint32{
		ImageRAWQuattro,
		ImageRAWSDQ,
		ImageRAWSDQH,
		FormatTypeQuattro,
		FormatTypeSDQ,
		FormatTypeSDQH,
	}

	for _, format := range formats {
		if !isQuattroFormat(format) {
			t.Fatalf("format 0x%08x should be treated as Quattro", format)
		}
	}
}

func TestSDQCameraIDConstantsMatchX3FTools(t *testing.T) {
	if CameraIDSDQ != 40 {
		t.Fatalf("CameraIDSDQ = %d, want 40", CameraIDSDQ)
	}
	if CameraIDSDQH != 41 {
		t.Fatalf("CameraIDSDQH = %d, want 41", CameraIDSDQH)
	}
}

func TestStoreTRUEPlaneDataDiscardsExtraRightColumns(t *testing.T) {
	decodedData := make([]uint16, 2*2*3)
	colorData := []uint16{
		1, 2, 99,
		3, 4, 88,
	}

	storeTRUEPlaneData(decodedData, colorData, 2, 3, 2, 2, 2)

	want := []uint16{
		0, 0, 1,
		0, 0, 2,
		0, 0, 3,
		0, 0, 4,
	}

	for i := range want {
		if decodedData[i] != want[i] {
			t.Fatalf("decodedData[%d] = %d, want %d", i, decodedData[i], want[i])
		}
	}
}

func TestLoadImageSectionLoadsSDQAsQuattroTRUE(t *testing.T) {
	sectionData := buildMinimalTRUESection(t, ImageRAWSDQ)
	file := &File{
		reader: bytes.NewReader(sectionData),
	}

	entry := &DirectoryEntry{
		Offset: 0,
		Length: uint32(len(sectionData)),
		Type:   SECi,
	}

	if err := file.LoadImageSection(entry); err != nil {
		t.Fatalf("LoadImageSection failed: %v", err)
	}

	if len(file.ImageData) != 1 {
		t.Fatalf("expected one RAW image section, got %d", len(file.ImageData))
	}

	section := file.ImageData[0]
	if section.Format != ImageRAWSDQ {
		t.Fatalf("unexpected format: 0x%08x", section.Format)
	}
	if section.QuattroLayout != 1 {
		t.Fatalf("expected Quattro layout, got %d", section.QuattroLayout)
	}
	if section.QuattroPlanes[2].Columns != 4 || section.QuattroPlanes[2].Rows != 4 {
		t.Fatalf("unexpected top plane size: %dx%d",
			section.QuattroPlanes[2].Columns,
			section.QuattroPlanes[2].Rows)
	}
}

func buildMinimalTRUESection(t *testing.T, format uint32) []byte {
	t.Helper()

	var buffer bytes.Buffer

	writeUint32(t, &buffer, SECi)
	writeUint32(t, &buffer, Version40)
	writeUint32(t, &buffer, 1)
	writeUint32(t, &buffer, format)
	writeUint32(t, &buffer, 4)
	writeUint32(t, &buffer, 4)
	writeUint32(t, &buffer, 12)

	writeUint16(t, &buffer, 2)
	writeUint16(t, &buffer, 2)
	writeUint16(t, &buffer, 2)
	writeUint16(t, &buffer, 2)
	writeUint16(t, &buffer, 4)
	writeUint16(t, &buffer, 4)

	writeUint16(t, &buffer, 0)
	writeUint16(t, &buffer, 0)
	writeUint16(t, &buffer, 0)
	writeUint16(t, &buffer, 0)

	buffer.WriteByte(0)
	buffer.WriteByte(0)

	writeUint32(t, &buffer, 0)

	writeUint32(t, &buffer, 0)
	writeUint32(t, &buffer, 0)
	writeUint32(t, &buffer, 0)

	return buffer.Bytes()
}

func writeUint16(t *testing.T, buffer *bytes.Buffer, value uint16) {
	t.Helper()

	if err := binary.Write(buffer, binary.LittleEndian, value); err != nil {
		t.Fatalf("failed to write uint16: %v", err)
	}
}

func writeUint32(t *testing.T, buffer *bytes.Buffer, value uint32) {
	t.Helper()

	if err := binary.Write(buffer, binary.LittleEndian, value); err != nil {
		t.Fatalf("failed to write uint32: %v", err)
	}
}
