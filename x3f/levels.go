package x3f

import (
	"fmt"
	"math"
	"runtime"
	"sync"
)

// ============================================================================
// 标准常量定义 (来自 C 版本 src/x3f_process.c)
// ============================================================================

const (
	// INTERMEDIATE_DEPTH 中间层深度 (14-bit)
	INTERMEDIATE_DEPTH = 14
	// INTERMEDIATE_UNIT 中间层单位值
	INTERMEDIATE_UNIT = (1 << INTERMEDIATE_DEPTH) - 1 // 16383
	// INTERMEDIATE_BIAS_FACTOR 中间层偏移系数
	INTERMEDIATE_BIAS_FACTOR = 4.0
)

// ============================================================================
// 临时兜底常量 (TODO: 待实现动态计算后移除)
// ============================================================================
// 注意: 以下常量是临时方案，C 代码中这些值是动态计算或从 CAMF 读取的
// - 黑电平: C 代码从 DarkShield 区域统计计算 (get_black_level)
// - 白平衡: C 代码从 CAMF 读取 (x3f_get_gain)
// - 色彩矩阵: C 代码从 CAMF 读取并矩阵运算 (x3f_get_bmt_to_xyz)
//
// Go 版本当前因为 CAMF 解析不完整，使用硬编码的测量值作为兜底

var (
	// DefaultBlackLevel 默认黑电平均值 (临时兜底值)
	// 来源: C 版本 -v 对 dp2m01.x3f 的 DarkShield 区域测量结果
	// TODO: 实现 DarkShield 区域统计计算后移除
	DefaultBlackLevel = Vector3{16.112489, 16.053343, 16.193610}

	// DefaultBlackDev 默认黑电平标准差 (临时兜底值)
	// 根据 C 版本的 intermediate_bias = 175.424 反推得出
	// TODO: 实现 DarkShield 区域统计计算后移除
	DefaultBlackDev = Vector3{10.918860, 10.918860, 10.918860}

	// DefaultWhiteBalanceGain 默认白平衡增益 (临时兜底值)
	// 来源: C 版本 -v 对 dp2m01.x3f 的输出
	// 计算公式: AutoWBGain * SensorAdjustmentGainFact * TempGainFact * FNumberGainFact
	// TODO: 实现完整的 CAMF 白平衡读取后移除
	DefaultWhiteBalanceGain = Vector3{1.96768, 1.15026, 0.777087}
)

type BlackLevelInfo struct {
	Level Vector3
	Dev   Vector3
}

// 计算黑电平及其标准差
func CalculateBlackLevel(file *File, section *ImageSection) (BlackLevelInfo, error) {
	decodedWidth := section.Columns
	decodedHeight := section.Rows
	if section.DecodedColumns > 0 {
		decodedWidth = section.DecodedColumns
	}
	if section.DecodedRows > 0 {
		decodedHeight = section.DecodedRows
	}

	mainColors := 3
	if len(section.QuattroTopData) > 0 {
		mainColors = 2
	}

	result, ok := calculateBlackLevelForData(file, section.DecodedData,
		decodedWidth, decodedHeight, 3, decodedWidth*3, true, mainColors)
	if !ok {
		return result, nil
	}

	if len(section.QuattroTopData) > 0 {
		topResult, topOk := calculateBlackLevelForData(file, section.QuattroTopData,
			uint32(section.QuattroTopCols), uint32(section.QuattroTopRows), 1, uint32(section.QuattroTopCols), false, 1)
		if !topOk {
			return result, fmt.Errorf("无法计算 Quattro top 黑电平")
		}
		result.Level[2] = topResult.Level[0]
		result.Dev[2] = topResult.Dev[0]
	}

	debug("CalculateBlackLevel: level=(%.2f, %.2f, %.2f), dev=(%.2f, %.2f, %.2f)",
		result.Level[0], result.Level[1], result.Level[2],
		result.Dev[0], result.Dev[1], result.Dev[2])

	return result, nil
}

type blackLevelArea struct {
	x0   uint32
	y0   uint32
	x1   uint32
	y1   uint32
	name string
}

func calculateBlackLevelForData(
	file *File,
	data []uint16,
	width, height, channels, rowStride uint32,
	rescale bool,
	colors int,
) (BlackLevelInfo, bool) {
	var result BlackLevelInfo
	var areas []blackLevelArea

	if len(data) == 0 || width == 0 || height == 0 || channels == 0 || rowStride == 0 || colors <= 0 {
		return result, false
	}

	if x0, y0, x1, y1, ok := file.GetCAMFRectScaled("DarkShieldTop", width, height, rescale); ok {
		debug("Calculate black level for DarkShieldTop: [%d,%d,%d,%d]", x0, y0, x1, y1)
		areas = append(areas, blackLevelArea{x0: x0, y0: y0, x1: x1, y1: y1, name: "DarkShieldTop"})
	} else {
		debug("Do not calculate black level for DarkShieldTop")
	}

	useDarkShieldBottom := true
	if model, ok := file.GetProperty("CAMMODEL"); ok && model == "SIGMA DP2" {
		useDarkShieldBottom = false
		debug("Skip DarkShieldBottom for SIGMA DP2")
	}
	if cameraID, ok := file.GetCAMFUint32("CAMERAID"); ok && cameraID == CameraIDSDQH { // X3F_CAMERAID_SDQH
		useDarkShieldBottom = false
		debug("Skip DarkShieldBottom for sd Quattro H")
	}
	if useDarkShieldBottom {
		if x0, y0, x1, y1, ok := file.GetCAMFRectScaled("DarkShieldBottom", width, height, rescale); ok {
			debug("Calculate black level for DarkShieldBottom: [%d,%d,%d,%d]", x0, y0, x1, y1)
			areas = append(areas, blackLevelArea{x0: x0, y0: y0, x1: x1, y1: y1, name: "DarkShieldBottom"})
		} else {
			debug("Do not calculate black level for DarkShieldBottom (not found)")
		}
	}

	if colRange, ok := file.GetCAMFMatrixUint32("DarkShieldColRange", 2, 2); ok {
		if x0, y0, x1, y1, ok := transformCAMFRectToImage(file, colRange[0], 0, colRange[1], ^uint32(0), width, height, rescale); ok {
			debug("Calculate black level for Left: [%d,%d,%d,%d]", x0, y0, x1, y1)
			areas = append(areas, blackLevelArea{x0: x0, y0: y0, x1: x1, y1: y1, name: "Left"})
		}
		if x0, y0, x1, y1, ok := transformCAMFRectToImage(file, colRange[2], 0, colRange[3], ^uint32(0), width, height, rescale); ok {
			debug("Calculate black level for Right: [%d,%d,%d,%d]", x0, y0, x1, y1)
			areas = append(areas, blackLevelArea{x0: x0, y0: y0, x1: x1, y1: y1, name: "Right"})
		}
	}

	if len(areas) == 0 {
		return result, false
	}

	var sum [3]uint64
	var count uint64

	for _, area := range areas {
		for y := area.y0; y <= area.y1 && y < height; y++ {
			for x := area.x0; x <= area.x1 && x < width; x++ {
				baseIdx := int(y)*int(rowStride) + int(x)*int(channels)
				for color := 0; color < colors; color++ {
					sum[color] += uint64(data[baseIdx+color])
				}
				count++
			}
		}
	}

	if count == 0 {
		return result, false
	}

	for color := 0; color < colors; color++ {
		result.Level[color] = float64(sum[color]) / float64(count)
	}

	var sqdevSum Vector3

	for _, area := range areas {
		for y := area.y0; y <= area.y1 && y < height; y++ {
			for x := area.x0; x <= area.x1 && x < width; x++ {
				baseIdx := int(y)*int(rowStride) + int(x)*int(channels)
				for color := 0; color < colors; color++ {
					value := float64(data[baseIdx+color])
					diff := value - result.Level[color]
					sqdevSum[color] += diff * diff
				}
			}
		}
	}

	for color := 0; color < colors; color++ {
		result.Dev[color] = math.Sqrt(sqdevSum[color] / float64(count))
	}

	return result, true
}

func transformCAMFRectToImage(
	file *File,
	rectX0, rectY0, rectX1, rectY1 uint32,
	imageWidth, imageHeight uint32,
	rescale bool,
) (x0, y0, x1, y1 uint32, ok bool) {
	keepX0, keepY0, keepX1, keepY1, keepOk := file.GetCAMFRect("KeepImageArea")
	if !keepOk {
		return 0, 0, 0, 0, false
	}

	if rectX0 > keepX1 || rectY0 > keepY1 || rectX1 < keepX0 || rectY1 < keepY0 {
		return 0, 0, 0, 0, false
	}

	if rectX0 < keepX0 {
		rectX0 = keepX0
	}
	if rectY0 < keepY0 {
		rectY0 = keepY0
	}
	if rectX1 > keepX1 {
		rectX1 = keepX1
	}
	if rectY1 > keepY1 {
		rectY1 = keepY1
	}

	x0 = rectX0 - keepX0
	y0 = rectY0 - keepY0
	x1 = rectX1 - keepX0
	y1 = rectY1 - keepY0

	if rescale {
		keepCols := keepX1 - keepX0 + 1
		keepRows := keepY1 - keepY0 + 1
		x0 = x0 * imageWidth / keepCols
		y0 = y0 * imageHeight / keepRows
		x1 = x1 * imageWidth / keepCols
		y1 = y1 * imageHeight / keepRows
	}

	return x0, y0, x1, y1, true
}

// 计算 intermediate 表示的最大值
func GetMaxIntermediate(file *File, wb string, intermediateBias float64) ([3]uint32, bool) {
	gain, ok := file.GetWhiteBalanceGain(wb)
	if !ok {
		debug("GetMaxIntermediate: GetWhiteBalanceGain('%s') failed", wb)
		return [3]uint32{0, 0, 0}, false
	}

	// 找到最大增益，用于归一化
	maxGain := gain[0]
	if gain[1] > maxGain {
		maxGain = gain[1]
	}
	if gain[2] > maxGain {
		maxGain = gain[2]
	}

	var result [3]uint32
	for i := 0; i < 3; i++ {
		result[i] = uint32(math.Round(gain[i]*(INTERMEDIATE_UNIT-intermediateBias)/maxGain + intermediateBias))
	}

	return result, true
}

// 计算 intermediate bias
func GetIntermediateBias(file *File, wb string, blackLevel BlackLevelInfo) (float64, bool) {
	maxRaw, ok := file.GetMaxRAW()
	if !ok {
		maxRaw = [3]uint32{4095, 4095, 4095}
	}

	maxIntermediate, ok := GetMaxIntermediate(file, wb, 0)
	if !ok {
		return 0, false
	}

	bias := 0.0
	for i := 0; i < 3; i++ {
		b := INTERMEDIATE_BIAS_FACTOR * blackLevel.Dev[i] *
			float64(maxIntermediate[i]) / (float64(maxRaw[i]) - blackLevel.Level[i])
		if b > bias {
			bias = b
		}
	}

	debug("GetIntermediateBias: maxRaw=(%d, %d, %d), maxIntermediate=(%d, %d, %d), bias=%.2f",
		maxRaw[0], maxRaw[1], maxRaw[2],
		maxIntermediate[0], maxIntermediate[1], maxIntermediate[2],
		bias)

	return bias, true
}

// 应用 C 版本的预处理逻辑
func PreprocessData(file *File, section *ImageSection, wb string) (string, error) {
	debug("PreprocessData: starting preprocessing with wb='%s'", wb)

	blackLevelInfo, err := CalculateBlackLevel(file, section)
	if err != nil {
		debug("PreprocessData: CalculateBlackLevel failed: %v", err)
		return "", err
	}

	maxRaw, ok := file.GetMaxRAW()
	if !ok {
		maxRaw = [3]uint32{4095, 4095, 4095}
	}

	intermediateBias, ok := GetIntermediateBias(file, wb, blackLevelInfo)
	if !ok {
		debug("PreprocessData: GetIntermediateBias failed, using 0")
		intermediateBias = 0
	}

	maxIntermediate, ok := GetMaxIntermediate(file, wb, intermediateBias)
	if !ok {
		debug("PreprocessData: GetMaxIntermediate failed, skipping preprocessing")
		return "跳过", nil
	}

	// 计算 scale
	var scale Vector3
	for i := 0; i < 3; i++ {
		white := float64(maxIntermediate[i])
		black := intermediateBias
		scale[i] = (white - black) / (float64(maxRaw[i]) - blackLevelInfo.Level[i])
	}

	// 应用预处理到每个像素
	decodedWidth := section.Columns
	decodedHeight := section.Rows
	if section.DecodedColumns > 0 {
		decodedWidth = section.DecodedColumns
	}
	if section.DecodedRows > 0 {
		decodedHeight = section.DecodedRows
	}

	// 检测是否是 Quattro 格式
	isQuattro := len(section.QuattroTopData) > 0
	colorsToProcess := 3
	if isQuattro {
		colorsToProcess = 2 // 对 Quattro 只处理前两个通道 (B, M)
		debug("[PREPROC] Quattro detected, processing only first 2 channels")
	}

	// 应用预处理到每个像素（并发处理）
	numWorkers := runtime.NumCPU()
	if numWorkers > int(decodedHeight) {
		numWorkers = int(decodedHeight)
	}

	rowsPerWorker := int(decodedHeight) / numWorkers
	var wg sync.WaitGroup

	for workerID := 0; workerID < numWorkers; workerID++ {
		wg.Add(1)

		startRow := workerID * rowsPerWorker
		endRow := startRow + rowsPerWorker
		if workerID == numWorkers-1 {
			endRow = int(decodedHeight)
		}

		go func(startY, endY int) {
			defer wg.Done()

			for y := uint32(startY); y < uint32(endY); y++ {
				for x := uint32(0); x < decodedWidth; x++ {
					idx := int(y)*int(decodedWidth) + int(x)

					for c := 0; c < colorsToProcess; c++ {
						val := float64(section.DecodedData[idx*3+c])
						out := math.Round(scale[c]*(val-blackLevelInfo.Level[c]) + intermediateBias)

						if out < 0 {
							section.DecodedData[idx*3+c] = 0
						} else if out > 65535 {
							section.DecodedData[idx*3+c] = 65535
						} else {
							section.DecodedData[idx*3+c] = uint16(out)
						}
					}
				}
			}
		}(startRow, endRow)
	}

	wg.Wait()

	// 坏点修复（在预处理之后）
	badPixels := CollectBadPixels(file, decodedWidth, decodedHeight, 3)
	// 使用纯 Go 实现的插值算法修复坏点
	InpaintBadPixels(section.DecodedData, decodedWidth, decodedHeight, 3, badPixels)

	// 构建返回信息
	channelInfo := "3通道"
	if isQuattro {
		channelInfo = "2通道(BM)"
	}
	info := fmt.Sprintf("%s Scale R:%.2f G:%.2f B:%.2f | 坏点:%d",
		channelInfo, scale[0], scale[1], scale[2], len(badPixels))

	return info, nil
}

// PreprocessQuattroTop 对 Quattro top 层数据进行预处理
// 包括：1. 全分辨率 top 层预处理（用于 expand）
//  2. 降采样后放到 DecodedData 的第 2 通道（用于正常的 BMT 处理）
func PreprocessQuattroTop(
	file *File,
	section *ImageSection,
	blackLevelInfo BlackLevelInfo,
	intermediateBias float64,
	maxIntermediate [3]uint32,
) error {
	if len(section.QuattroTopData) == 0 {
		return nil
	}

	debug("PreprocessQuattroTop: starting top layer preprocessing and downsampling")

	topBlackLevel := blackLevelInfo.Level[2]

	maxRaw, ok := file.GetMaxRAW()
	if !ok {
		maxRaw = [3]uint32{4095, 4095, 4095}
	}

	// 使用第 2 通道（T 通道）的参数
	white := float64(maxIntermediate[2])
	black := intermediateBias
	scale := (white - black) / (float64(maxRaw[2]) - topBlackLevel)

	// 1. 保存原始 top 层数据的副本（用于降采样）
	originalTopData := make([]uint16, len(section.QuattroTopData))
	copy(originalTopData, section.QuattroTopData)

	// 2. 降采样 top 层并预处理后放到 DecodedData 的第 2 通道
	// C 代码：先取 2x2 区域的 4 个像素求平均，再对平均值进行预处理
	decodedWidth := int(section.DecodedColumns)
	decodedHeight := int(section.DecodedRows)
	topWidth := section.QuattroTopCols
	topHeight := section.QuattroTopRows

	debug("PreprocessQuattroTop: downsampling top layer from %dx%d to %dx%d",
		topWidth, topHeight, decodedWidth, decodedHeight)

	downsampledCount := 0
	for row := 0; row < decodedHeight; row++ {
		for col := 0; col < decodedWidth; col++ {
			// 从 top 层的 2x2 区域取 4 个像素
			topRow := row * 2
			topCol := col * 2

			// 边界检查
			if topRow+1 >= topHeight || topCol+1 >= topWidth {
				if row == 0 && col < 10 {
					debug("PreprocessQuattroTop: skip [%d,%d] (topRow=%d, topCol=%d, bounds=%dx%d)",
						row, col, topRow, topCol, topWidth, topHeight)
				}
				continue
			}

			// 计算 2x2 区域的平均值（使用原始值）
			row1Col1 := topRow*topWidth + topCol
			row1Col2 := topRow*topWidth + topCol + 1
			row2Col1 := (topRow+1)*topWidth + topCol
			row2Col2 := (topRow+1)*topWidth + topCol + 1

			sum := uint32(originalTopData[row1Col1]) +
				uint32(originalTopData[row1Col2]) +
				uint32(originalTopData[row2Col1]) +
				uint32(originalTopData[row2Col2])

			avgVal := float64(sum) / 4.0

			// 对平均值进行预处理（使用 top 层的黑电平）
			out := math.Round(scale*(avgVal-topBlackLevel) + intermediateBias)

			// 放到 DecodedData 的第 2 通道
			dstIdx := row*decodedWidth + col
			if out < 0 {
				section.DecodedData[dstIdx*3+2] = 0
			} else if out > 65535 {
				section.DecodedData[dstIdx*3+2] = 65535
			} else {
				section.DecodedData[dstIdx*3+2] = uint16(out)
			}

			downsampledCount++
			if downsampledCount == 1 {
				debug("PreprocessQuattroTop: 第一个像素 avgVal=%.2f, out=%.2f, DecodedData[2]=%d",
					avgVal, out, section.DecodedData[dstIdx*3+2])
			}
		}
	}

	debug("PreprocessQuattroTop: downsampled %d pixels", downsampledCount)

	// 3. 预处理全分辨率 top 层（用于后续 expand）
	for i := 0; i < len(section.QuattroTopData); i++ {
		val := float64(section.QuattroTopData[i])
		out := math.Round(scale*(val-topBlackLevel) + intermediateBias)

		if out < 0 {
			section.QuattroTopData[i] = 0
		} else if out > 65535 {
			section.QuattroTopData[i] = 65535
		} else {
			section.QuattroTopData[i] = uint16(out)
		}
	}

	debug("PreprocessQuattroTop: completed")

	// C 版本在这里对 qtop 执行了坏点修复
	// if (fix_bad) interpolate_bad_pixels(x3f, &qtop, 1);
	debug("PreprocessQuattroTop: fixing bad pixels on top layer")
	qtopBadPixels := CollectBadPixels(file, uint32(section.QuattroTopCols), uint32(section.QuattroTopRows), 1)
	if len(qtopBadPixels) > 0 {
		InpaintBadPixels(section.QuattroTopData, uint32(section.QuattroTopCols), uint32(section.QuattroTopRows), 1, qtopBadPixels)
		debug("PreprocessQuattroTop: fixed %d bad pixels on top layer", len(qtopBadPixels))
	}

	return nil
}
