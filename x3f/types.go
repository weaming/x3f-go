package x3f

import (
	"io"
)

// File represents an X3F file
type File struct {
	Header      *FileHeader
	Directory   *Directory
	Properties  *PropertyList
	CAMFSection *CAMFData
	ImageData   []*ImageSection
	reader      io.ReaderAt
	size        int64
}

// FileHeader represents the X3F file header
type FileHeader struct {
	Identifier        [4]byte
	Version           uint32
	UniqueIdentifier  [16]byte
	MarkBits          uint32
	Columns           uint32
	Rows              uint32
	Rotation          uint32
	WhiteBalance      [32]byte
	ColorMode         [32]byte
	ExtendedData      [64]float32
	ExtendedDataTypes [64]uint8
}

// Directory represents the X3F directory section
type Directory struct {
	Identifier [4]byte
	Version    uint32
	NumEntries uint32
	Entries    []DirectoryEntry
}

// DirectoryEntry represents a single directory entry
type DirectoryEntry struct {
	Offset uint32
	Length uint32
	Type   uint32
}

// PropertyList represents the PROP section
type PropertyList struct {
	NumProperties   uint32
	CharacterFormat uint32
	Reserved        uint32
	TotalLength     uint32
	Properties      []Property
	Data            []byte
}

// Property represents a single property entry
type Property struct {
	NameOffset  uint32
	ValueOffset uint32
	Name        string
	Value       string
	NameUTF16   []uint16
	ValueUTF16  []uint16
}

// Area8 represents 8-bit image area
type Area8 struct {
	Data      []uint8
	Rows      uint32
	Columns   uint32
	Channels  uint32
	RowStride uint32
}

// Area16 represents 16-bit image area
type Area16 struct {
	Data      []uint16
	Rows      uint32
	Columns   uint32
	Channels  uint32
	RowStride uint32
}

// ImageLevels represents black and white levels for image
type ImageLevels struct {
	Black Vector3
	White [3]uint32
}

// SpatialGainCorrection represents spatial gain correction data
type SpatialGainCorrection struct {
	RowOffset uint32
	ColOffset uint32
	RowPitch  uint32
	ColPitch  uint32
	Rows      uint32
	Cols      uint32
	Channel   uint32
	Channels  uint32
	Gain      []float32
}
