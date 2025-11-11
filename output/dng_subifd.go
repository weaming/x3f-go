package output

import (
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

// writeSubIFD 写入 SubIFD (包含完整分辨率 RAW 数据)
// 返回 SubIFD 的起始偏移量
func writeSubIFD(file *os.File, x3fFile *x3f.File, imageData []byte,
	targetWidth, targetHeight uint32,
	activeAreaTop, activeAreaLeft, activeAreaBottom, activeAreaRight uint32,
	wbGain [3]float64, opcodeList2Data []byte) (uint32, error) {

	// 记录 SubIFD 起始位置
	subIFDOffset, err := file.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, err
	}

	// 获取 BlackLevel 和 WhiteLevel
	// 使用外部传入的 WhiteBalance gain 计算
	levels, ok := x3fFile.GetImageLevelsWithGain(wbGain)
	if !ok {
		levels = x3f.ImageLevels{
			Black: [3]float64{168.756, 168.756, 168.756},
			White: [3]uint32{16383, 8498, 5430},
		}
	}

	// 创建 SubIFD 写入器
	subIFD := NewIFDWriter(file)

	// 计算条带信息
	rowsPerStrip := uint32(32)
	numStrips := (targetHeight + rowsPerStrip - 1) / rowsPerStrip
	bytesPerRow := targetWidth * 3 * 2

	stripByteCounts := make([]uint32, numStrips)
	for i := uint32(0); i < numStrips; i++ {
		rowsInThisStrip := rowsPerStrip
		if (i+1)*rowsPerStrip > targetHeight {
			rowsInThisStrip = targetHeight - i*rowsPerStrip
		}
		stripByteCounts[i] = rowsInThisStrip * bytesPerRow
	}

	// 添加所有标签(stripOffsets 先用临时值占位)
	subIFD.AddLong(TagNewSubfileType, 0)
	subIFD.AddLong(TagImageWidth, targetWidth)
	subIFD.AddLong(TagImageLength, targetHeight)
	subIFD.AddShortArray(TagBitsPerSample, []uint16{16, 16, 16})
	subIFD.AddShort(TagCompression, 1)
	subIFD.AddShort(TagPhotometricInterpret, PhotometricLinearRaw)

	// 先添加临时的 stripOffsets(全0占位)
	tempStripOffsets := make([]uint32, numStrips)
	subIFD.AddLongArray(TagStripOffsets, tempStripOffsets)

	subIFD.AddShort(TagSamplesPerPixel, 3)
	subIFD.AddLong(TagRowsPerStrip, rowsPerStrip)
	subIFD.AddLongArray(TagStripByteCounts, stripByteCounts)
	subIFD.AddShort(TagPlanarConfiguration, 1)

	// BlackLevel 使用固定分母 (16.16 定点格式,DNG 规范要求)
	blackLevelRationals := make([][2]uint32, 3)
	for i := 0; i < 3; i++ {
		num := uint32(levels.Black[i] * float64(DNG_BLACK_LEVEL_DENOMINATOR))
		blackLevelRationals[i] = [2]uint32{num, DNG_BLACK_LEVEL_DENOMINATOR}
	}
	subIFD.AddRationalArray(TagBlackLevel, blackLevelRationals)

	subIFD.AddLongArray(TagWhiteLevel, levels.White[:])
	subIFD.AddRationalFromFloat(TagChromaBlurRadius, 0.0, false)
	subIFD.AddLongArray(TagActiveArea, []uint32{
		activeAreaTop,
		activeAreaLeft,
		activeAreaBottom,
		activeAreaRight,
	})

	if opcodeList2Data != nil {
		subIFD.AddUndefined(TagOpcodeList2, opcodeList2Data)
	}

	// 计算 SubIFD + 辅助数据的预期结束位置
	// 现在 stripOffsets 数组已经包含在计算中
	ifdEndPos := subIFD.GetCurrentPosition()

	// 计算每个条带的正确偏移值
	stripOffsets := make([]uint32, numStrips)
	currentOffset := uint32(ifdEndPos)
	for i := uint32(0); i < numStrips; i++ {
		stripOffsets[i] = currentOffset
		currentOffset += stripByteCounts[i]
	}

	// 更新 stripOffsets 数据(找到对应的 entry 并更新其 data 字段)
	for _, entry := range subIFD.entries {
		if entry.tag == TagStripOffsets {
			// 重新生成 data (新版本用 []uint32)
			entry.data = make([]uint32, len(stripOffsets))
			for i, offset := range stripOffsets {
				entry.data[i] = offset
			}
			break
		}
	}

	// 写入 SubIFD + 辅助数据
	if _, err := subIFD.Write(); err != nil {
		return 0, err
	}

	// 写入图像数据(条带)
	if _, err := file.Write(imageData); err != nil {
		return 0, err
	}

	return uint32(subIFDOffset), nil
}
