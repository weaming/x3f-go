package output

import (
	"encoding/binary"
	"fmt"
	"os"

	"github.com/weaming/x3f-go/processor"
)

// TIFF 标签
const (
	TagImageWidth           = 256
	TagImageLength          = 257
	TagBitsPerSample        = 258
	TagCompression          = 259
	TagPhotometricInterpret = 262
	TagStripOffsets         = 273
	TagSamplesPerPixel      = 277
	TagRowsPerStrip         = 278
	TagStripByteCounts      = 279
	TagXResolution          = 282
	TagYResolution          = 283
	TagPlanarConfiguration  = 284
	TagResolutionUnit       = 296
	TagSoftware             = 305
	TagDateTime             = 306
	TagSampleFormat         = 339
)

// TIFF 数据类型
const (
	TypeByte      = 1
	TypeASCII     = 2
	TypeShort     = 3
	TypeLong      = 4
	TypeRational  = 5
	TypeSByte     = 6
	TypeUndefined = 7
	TypeSShort    = 8
	TypeSLong     = 9
	TypeSRational = 10
	TypeFloat     = 11
	TypeDouble    = 12
)

// IFD 条目
type IFDEntry struct {
	Tag       uint16
	Type      uint16
	Count     uint32
	ValueData uint32 // 可能是值或偏移
}

// WriteTIFF 写入 TIFF 文件
func WriteTIFF(img *processor.ProcessedImage, filename string, use16Bit bool) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	// TIFF 文件头
	// 字节序标识 (Little Endian)
	file.Write([]byte{0x49, 0x49})
	// 版本号 42
	binary.Write(file, binary.LittleEndian, uint16(42))
	// IFD 偏移（在头部之后）
	binary.Write(file, binary.LittleEndian, uint32(8))

	// 准备图像数据
	var imageData []byte
	var bitsPerSample uint16
	var sampleFormat uint16

	if use16Bit {
		bitsPerSample = 16
		sampleFormat = 1 // 无符号整数
		data16 := img.ToUint16()
		imageData = make([]byte, len(data16)*2)
		for i, v := range data16 {
			binary.LittleEndian.PutUint16(imageData[i*2:], v)
		}
	} else {
		bitsPerSample = 8
		sampleFormat = 1
		imageData = img.ToUint8()
	}

	// 图像数据偏移（在 IFD 之后）
	dataOffset := uint32(8 + 2 + 12*14 + 4) // 头部 + 条目数 + 条目 + 下一个IFD

	// 写入 IFD
	numEntries := uint16(14)
	binary.Write(file, binary.LittleEndian, numEntries)

	// ImageWidth
	writeIFDEntry(file, TagImageWidth, TypeLong, 1, uint32(img.Width))

	// ImageLength
	writeIFDEntry(file, TagImageLength, TypeLong, 1, uint32(img.Height))

	// BitsPerSample
	writeIFDEntry(file, TagBitsPerSample, TypeShort, 3, dataOffset+uint32(len(imageData)))

	// Compression (1 = 无压缩)
	writeIFDEntry(file, TagCompression, TypeShort, 1, 1)

	// PhotometricInterpretation (2 = RGB)
	writeIFDEntry(file, TagPhotometricInterpret, TypeShort, 1, 2)

	// StripOffsets
	writeIFDEntry(file, TagStripOffsets, TypeLong, 1, dataOffset)

	// SamplesPerPixel
	writeIFDEntry(file, TagSamplesPerPixel, TypeShort, 1, 3)

	// RowsPerStrip
	writeIFDEntry(file, TagRowsPerStrip, TypeLong, 1, uint32(img.Height))

	// StripByteCounts
	writeIFDEntry(file, TagStripByteCounts, TypeLong, 1, uint32(len(imageData)))

	// XResolution
	xResOffset := dataOffset + uint32(len(imageData)) + 6
	writeIFDEntry(file, TagXResolution, TypeRational, 1, xResOffset)

	// YResolution
	yResOffset := xResOffset + 8
	writeIFDEntry(file, TagYResolution, TypeRational, 1, yResOffset)

	// PlanarConfiguration (1 = chunky)
	writeIFDEntry(file, TagPlanarConfiguration, TypeShort, 1, 1)

	// ResolutionUnit (2 = inches)
	writeIFDEntry(file, TagResolutionUnit, TypeShort, 1, 2)

	// SampleFormat
	sampleFormatOffset := yResOffset + 8
	writeIFDEntry(file, TagSampleFormat, TypeShort, 3, sampleFormatOffset)

	// 下一个 IFD 偏移 (0 = 没有更多 IFD)
	binary.Write(file, binary.LittleEndian, uint32(0))

	// 写入图像数据
	file.Write(imageData)

	// 写入 BitsPerSample 值 (3 个 short)
	binary.Write(file, binary.LittleEndian, bitsPerSample)
	binary.Write(file, binary.LittleEndian, bitsPerSample)
	binary.Write(file, binary.LittleEndian, bitsPerSample)

	// 写入 XResolution (72 dpi)
	binary.Write(file, binary.LittleEndian, uint32(72))
	binary.Write(file, binary.LittleEndian, uint32(1))

	// 写入 YResolution (72 dpi)
	binary.Write(file, binary.LittleEndian, uint32(72))
	binary.Write(file, binary.LittleEndian, uint32(1))

	// 写入 SampleFormat 值 (3 个 short)
	binary.Write(file, binary.LittleEndian, sampleFormat)
	binary.Write(file, binary.LittleEndian, sampleFormat)
	binary.Write(file, binary.LittleEndian, sampleFormat)

	return nil
}

func writeIFDEntry(file *os.File, tag uint16, typ uint16, count uint32, value uint32) {
	binary.Write(file, binary.LittleEndian, tag)
	binary.Write(file, binary.LittleEndian, typ)
	binary.Write(file, binary.LittleEndian, count)
	binary.Write(file, binary.LittleEndian, value)
}

// TIFFOptions TIFF 输出选项
type TIFFOptions struct {
	Use16Bit bool
}

// ExportTIFF 导出为 TIFF
func ExportTIFF(img *processor.ProcessedImage, filename string, opts TIFFOptions) error {
	if img == nil {
		return fmt.Errorf("图像为空")
	}

	return WriteTIFF(img, filename, opts.Use16Bit)
}
