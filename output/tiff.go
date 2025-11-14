package output

import (
	"encoding/binary"
	"fmt"
	"os"

	"github.com/weaming/x3f-go/x3f"
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
	TagExifIFD              = 34665
)

// EXIF 标签
const (
	ExifTagExposureTime = 33434
	ExifTagFNumber      = 33437
	ExifTagISOSpeed     = 34855
	ExifTagExifVersion  = 36864
	ExifTagMake         = 271 // 在主 IFD 中
	ExifTagModel        = 272 // 在主 IFD 中
	ExifTagLensModel    = 42036
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

// 写入 TIFF 文件
func WriteTIFF(img *x3f.ProcessedImage, filename string, opts TIFFOptions) error {
	use16Bit := opts.Use16Bit
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

	// 计算需要多少个 IFD entry
	numEntries := uint16(14) // 基础 entry 数
	hasExif := opts.Exif.FNumber > 0 || opts.Exif.ExposureTime > 0 || opts.Exif.ISO > 0 || opts.Exif.LensModel != ""
	if opts.Exif.Make != "" {
		numEntries++
	}
	if opts.Exif.Model != "" {
		numEntries++
	}
	if hasExif {
		numEntries++ // ExifIFD 指针
	}

	// 图像数据偏移（在 IFD 之后）
	dataOffset := uint32(8 + 2 + 12*int(numEntries) + 4) // 头部 + 条目数 + 条目 + 下一个IFD

	// 写入 IFD
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

	// Make (271) - 必须在 StripOffsets 之前
	makeOffset := dataOffset + uint32(len(imageData)) + 6 + 16 + 6 // BitsPerSample + XY Resolution + SampleFormat
	if opts.Exif.Make != "" {
		writeIFDEntry(file, ExifTagMake, TypeASCII, uint32(len(opts.Exif.Make)+1), makeOffset)
	}

	// Model (272) - 必须在 StripOffsets 之前
	modelOffset := makeOffset
	if opts.Exif.Make != "" {
		modelOffset += uint32((len(opts.Exif.Make) + 1 + 3) / 4 * 4)
	}
	if opts.Exif.Model != "" {
		writeIFDEntry(file, ExifTagModel, TypeASCII, uint32(len(opts.Exif.Model)+1), modelOffset)
	}

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

	// ExifIFD (34665) - 放在最后
	extraDataOffset := modelOffset
	if opts.Exif.Model != "" {
		extraDataOffset += uint32((len(opts.Exif.Model) + 1 + 3) / 4 * 4)
	}
	exifIFDOffset := extraDataOffset
	if hasExif {
		writeIFDEntry(file, TagExifIFD, TypeLong, 1, exifIFDOffset)
	}

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

	// 写入额外的字符串数据
	if opts.Exif.Make != "" {
		file.Write([]byte(opts.Exif.Make))
		file.Write([]byte{0}) // null terminator
		// 填充到 4 字节对齐
		padding := (4 - ((len(opts.Exif.Make) + 1) % 4)) % 4
		for i := 0; i < padding; i++ {
			file.Write([]byte{0})
		}
	}

	if opts.Exif.Model != "" {
		file.Write([]byte(opts.Exif.Model))
		file.Write([]byte{0})
		padding := (4 - ((len(opts.Exif.Model) + 1) % 4)) % 4
		for i := 0; i < padding; i++ {
			file.Write([]byte{0})
		}
	}

	// 写入 EXIF IFD
	if hasExif {
		exifNumEntries := uint16(0)
		if opts.Exif.FNumber > 0 {
			exifNumEntries++
		}
		if opts.Exif.ExposureTime > 0 {
			exifNumEntries++
		}
		if opts.Exif.ISO > 0 {
			exifNumEntries++
		}
		if opts.Exif.LensModel != "" {
			exifNumEntries++
		}

		if exifNumEntries > 0 {
			// 写入 EXIF IFD entry 数量
			binary.Write(file, binary.LittleEndian, exifNumEntries)

			// 计算 EXIF 数据偏移
			exifDataOffset := exifIFDOffset + 2 + uint32(exifNumEntries)*12 + 4

			// ExposureTime (33434) - 必须按tag编号升序
			if opts.Exif.ExposureTime > 0 {
				writeIFDEntry(file, ExifTagExposureTime, TypeRational, 1, exifDataOffset)
				exifDataOffset += 8
			}

			// FNumber (33437)
			if opts.Exif.FNumber > 0 {
				writeIFDEntry(file, ExifTagFNumber, TypeRational, 1, exifDataOffset)
				exifDataOffset += 8
			}

			// ISO (34855)
			if opts.Exif.ISO > 0 {
				writeIFDEntry(file, ExifTagISOSpeed, TypeShort, 1, uint32(opts.Exif.ISO))
			}

			// LensModel (42036)
			lensModelOffset := exifDataOffset
			if opts.Exif.LensModel != "" {
				writeIFDEntry(file, ExifTagLensModel, TypeASCII, uint32(len(opts.Exif.LensModel)+1), lensModelOffset)
			}

			// 下一个 IFD 偏移 (0 = 没有更多 EXIF IFD)
			binary.Write(file, binary.LittleEndian, uint32(0))

			// 写入 EXIF 数据值（顺序必须与上面的 entry 一致）
			if opts.Exif.ExposureTime > 0 {
				// ExposureTime 是 rational (1/shutter_speed)
				// 如果 ExposureTime 是 1/1740，则存储为 1/1740
				numerator := uint32(1)
				denominator := uint32(1.0 / opts.Exif.ExposureTime)
				if opts.Exif.ExposureTime >= 1.0 {
					numerator = uint32(opts.Exif.ExposureTime)
					denominator = 1
				}
				binary.Write(file, binary.LittleEndian, numerator)
				binary.Write(file, binary.LittleEndian, denominator)
			}

			if opts.Exif.FNumber > 0 {
				// FNumber 是 rational (numerator/denominator)
				fNum := uint32(opts.Exif.FNumber * 10)
				binary.Write(file, binary.LittleEndian, fNum)
				binary.Write(file, binary.LittleEndian, uint32(10))
			}

			if opts.Exif.LensModel != "" {
				file.Write([]byte(opts.Exif.LensModel))
				file.Write([]byte{0})
			}
		}
	}

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
	Exif     x3f.ExifInfo // EXIF 拍摄参数
}

// 导出为 TIFF
func ExportTIFF(img *x3f.ProcessedImage, filename string, opts TIFFOptions) error {
	if img == nil {
		return fmt.Errorf("图像为空")
	}

	return WriteTIFF(img, filename, opts)
}
