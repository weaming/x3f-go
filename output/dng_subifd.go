package output

import (
	"fmt"
	"io"
	"os"

	"github.com/weaming/x3f-go/x3f"
)

// ============================================================================
// 临时兜底常量 (TODO: 待改为与 C 代码一致的实现后移除)
// ============================================================================
// 注意: C 代码直接使用 float 数组传给 libtiff，由 libtiff 自动转换 RATIONAL
// C 代码实现: src/x3f_output_dng.c
//   vec_double_to_float(ilevels.black, black_level, 3);
//   TIFFSetField(f_out, TIFFTAG_BLACKLEVEL, 3, black_level);
//
// Go 版本当前使用手动 RATIONAL 转换，需要指定固定分母 65536
// TODO: 考虑使用 libtiff 库或改进 RATIONAL 写入逻辑

const (
	// DNG_BLACK_LEVEL_DENOMINATOR Black Level 固定分母 (16.16 定点格式)
	// 分母 = 2^16 = 65536
	DNG_BLACK_LEVEL_DENOMINATOR = 65536
)

// stripInfo 条带信息
type stripInfo struct {
	rowsPerStrip    uint32
	numStrips       uint32
	bytesPerRow     uint32
	stripByteCounts []uint32
	stripOffsets    []uint32
}

// writeSubIFD 写入 SubIFD (包含完整分辨率 RAW 数据)
func writeSubIFD(file *os.File, x3fFile *x3f.File, imageData []byte,
	targetWidth, targetHeight uint32,
	activeAreaTop, activeAreaLeft, activeAreaBottom, activeAreaRight uint32,
	wbGain [3]float64, opcodeList2Data []byte) (uint32, error) {

	subIFDOffset := recordSubIFDOffset(file)
	levels := getImageLevelsForWbGain(x3fFile, wbGain)
	strips := calculateStripInfo(targetWidth, targetHeight)

	subIFD := createSubIFD(file)
	addBasicTags(subIFD, targetWidth, targetHeight, strips)
	addImageLevelTags(subIFD, levels)
	addActiveAreaTag(subIFD, activeAreaTop, activeAreaLeft, activeAreaBottom, activeAreaRight)
	addOpcodeList2Tag(subIFD, opcodeList2Data)

	updateStripOffsets(subIFD, strips)
	writeSubIFDAndImageData(subIFD, file, imageData)

	return subIFDOffset, nil
}

// recordSubIFDOffset 记录 SubIFD 起始位置
func recordSubIFDOffset(file *os.File) uint32 {
	offset, err := file.Seek(0, io.SeekCurrent)
	if err != nil {
		panic(err)
	}
	return uint32(offset)
}

// 获取图像黑白电平
func getImageLevelsForWbGain(x3fFile *x3f.File, wbGain [3]float64) x3f.ImageLevels {
	levels, ok := x3fFile.GetImageLevelsWithGain(wbGain)
	if !ok {
		panic(fmt.Errorf("无法获取图像电平"))
	}
	return levels
}

// calculateStripInfo 计算条带信息
func calculateStripInfo(targetWidth, targetHeight uint32) stripInfo {
	rowsPerStrip := uint32(32)
	numStrips := (targetHeight + rowsPerStrip - 1) / rowsPerStrip
	bytesPerRow := targetWidth * 3 * 2

	stripByteCounts := make([]uint32, numStrips)
	for i := uint32(0); i < numStrips; i++ {
		rowsInStrip := calculateRowsInStrip(i, rowsPerStrip, targetHeight)
		stripByteCounts[i] = rowsInStrip * bytesPerRow
	}

	return stripInfo{
		rowsPerStrip:    rowsPerStrip,
		numStrips:       numStrips,
		bytesPerRow:     bytesPerRow,
		stripByteCounts: stripByteCounts,
	}
}

// calculateRowsInStrip 计算条带中的行数
func calculateRowsInStrip(stripIndex, rowsPerStrip, targetHeight uint32) uint32 {
	if (stripIndex+1)*rowsPerStrip > targetHeight {
		return targetHeight - stripIndex*rowsPerStrip
	}
	return rowsPerStrip
}

// createSubIFD 创建 SubIFD 写入器
func createSubIFD(file *os.File) *IFDWriter {
	return NewIFDWriter(file)
}

// addBasicTags 添加基础标签
func addBasicTags(subIFD *IFDWriter, targetWidth, targetHeight uint32, strips stripInfo) {
	subIFD.AddLong(TagNewSubfileType, 0)
	subIFD.AddLong(TagImageWidth, targetWidth)
	subIFD.AddLong(TagImageLength, targetHeight)
	subIFD.AddShortArray(TagBitsPerSample, []uint16{16, 16, 16})
	subIFD.AddShort(TagCompression, 1)
	subIFD.AddShort(TagPhotometricInterpret, PhotometricLinearRaw)

	tempStripOffsets := make([]uint32, strips.numStrips)
	subIFD.AddLongArray(TagStripOffsets, tempStripOffsets)

	subIFD.AddShort(TagSamplesPerPixel, 3)
	subIFD.AddLong(TagRowsPerStrip, strips.rowsPerStrip)
	subIFD.AddLongArray(TagStripByteCounts, strips.stripByteCounts)
	subIFD.AddShort(TagPlanarConfiguration, 1)
}

// addImageLevelTags 添加图像电平标签
func addImageLevelTags(subIFD *IFDWriter, levels x3f.ImageLevels) {
	subIFD.AddRationalFromFloat(TagChromaBlurRadius, 0.0, false)

	blackLevelRationals := convertBlackLevelToRationals(levels.Black)
	subIFD.AddRationalArray(TagBlackLevel, blackLevelRationals)
	subIFD.AddLongArray(TagWhiteLevel, levels.White[:])
}

// convertBlackLevelToRationals 将 BlackLevel 转换为有理数
func convertBlackLevelToRationals(blackLevel [3]float64) [][2]uint32 {
	rationals := make([][2]uint32, 3)
	for i := 0; i < 3; i++ {
		num := uint32(blackLevel[i] * float64(DNG_BLACK_LEVEL_DENOMINATOR))
		rationals[i] = [2]uint32{num, DNG_BLACK_LEVEL_DENOMINATOR}
	}
	return rationals
}

// addActiveAreaTag 添加 ActiveArea 标签
func addActiveAreaTag(subIFD *IFDWriter, top, left, bottom, right uint32) {
	subIFD.AddLongArray(TagActiveArea, []uint32{top, left, bottom, right})
}

// addOpcodeList2Tag 添加 OpcodeList2 标签
func addOpcodeList2Tag(subIFD *IFDWriter, opcodeList2Data []byte) {
	if opcodeList2Data != nil {
		subIFD.AddUndefined(TagOpcodeList2, opcodeList2Data)
	}
}

// updateStripOffsets 更新条带偏移量
func updateStripOffsets(subIFD *IFDWriter, strips stripInfo) {
	ifdEndPos := subIFD.GetCurrentPosition()
	stripOffsets := calculateStripOffsets(ifdEndPos, strips)

	for _, entry := range subIFD.entries {
		if entry.tag == TagStripOffsets {
			entry.data = make([]uint32, len(stripOffsets))
			for i, offset := range stripOffsets {
				entry.data[i] = offset
			}
			break
		}
	}

	strips.stripOffsets = stripOffsets
}

// calculateStripOffsets 计算条带偏移量
func calculateStripOffsets(ifdEndPos int64, strips stripInfo) []uint32 {
	offsets := make([]uint32, strips.numStrips)
	currentOffset := uint32(ifdEndPos)

	for i := uint32(0); i < strips.numStrips; i++ {
		offsets[i] = currentOffset
		currentOffset += strips.stripByteCounts[i]
	}

	return offsets
}

// writeSubIFDAndImageData 写入 SubIFD 和图像数据
func writeSubIFDAndImageData(subIFD *IFDWriter, file *os.File, imageData []byte) {
	if _, err := subIFD.Write(); err != nil {
		panic(err)
	}

	if _, err := file.Write(imageData); err != nil {
		panic(err)
	}
}
