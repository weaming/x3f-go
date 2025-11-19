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

// GetEXIFOrientation 获取 EXIF Orientation 值
// X3F Rotation 字段表示相机拍摄时的旋转角度（0, 90, 180, 270）
//
// EXIF Orientation 的含义（参考 TIFF 6.0 规范）：
//
//	1 = 正常方向（0°）- 图像顶部在上
//	3 = 旋转 180° - 图像倒置
//	6 = 顺时针旋转 90° - 竖拍，相机向右
//	8 = 顺时针旋转 270°（逆时针 90°）- 竖拍，相机向左
//
// 映射关系：
//
//	X3F Rotation 0°   → EXIF Orientation 1（正常横拍）
//	X3F Rotation 90°  → EXIF Orientation 6（竖拍，相机向右转）
//	X3F Rotation 180° → EXIF Orientation 3（倒置）
//	X3F Rotation 270° → EXIF Orientation 8（竖拍，相机向左转）
func (f *File) GetEXIFOrientation() uint16 {
	if f.Header == nil {
		return 1
	}

	rotation := f.Header.Rotation

	switch rotation {
	case 0:
		return 1
	case 90:
		return 6
	case 180:
		return 3
	case 270:
		return 8
	default:
		Debug("未知的 Rotation 值: %d, 默认使用 Orientation 1", rotation)
		return 1
	}
}
