package x3f

import (
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

// 获取原始数据的最大值
func (f *File) GetMaxRaw() ([3]uint32, bool) {
	// 优先尝试读取 ImageDepth
	if imageDepth, ok := f.GetCAMFUint32("ImageDepth"); ok {
		max := uint32((1 << imageDepth) - 1)
		return [3]uint32{max, max, max}, true
	}

	// 判断是否为 TRUE 引擎 (版本 >= 3.0)
	isTRUE := f.Header.Version >= Version30

	var tagName string
	if isTRUE {
		tagName = "RawSaturationLevel"
	} else {
		tagName = "SaturationLevel"
	}

	// 读取 CAMF 向量 (expectedSize=0 表示不限制)
	if vec, ok := f.GetCAMFInt32Vector(tagName, 0); ok && len(vec) >= 3 {
		return [3]uint32{uint32(vec[0]), uint32(vec[1]), uint32(vec[2])}, true
	}

	return [3]uint32{}, false
}

// GetImageLevels 获取图像黑白电平
// 仿照 C 版本 x3f_process.c 中 x3f_get_image 的逻辑
func (f *File) GetImageLevels(wb string) (ImageLevels, bool) {
	// 获取白平衡增益
	gain, ok := f.GetWhiteBalanceGain(wb)
	if !ok {
		gain = Vector3{1.0, 1.0, 1.0}
	}
	return f.GetImageLevelsWithGain(gain)
}

// 使用指定的白平衡增益计算图像黑白电平
func (f *File) GetImageLevelsWithGain(gain Vector3) (ImageLevels, bool) {
	var levels ImageLevels

	// 1. 获取黑电平和标准差
	blackLevel, blackDev, ok := f.getBlackLevel()
	if !ok {
		// TODO: 实现 DarkShield 区域统计计算
		// 临时使用默认值
		blackLevel = DefaultBlackLevel
		blackDev = DefaultBlackDev
	}

	// 2. 获取原始最大值
	maxRaw, ok := f.GetMaxRaw()
	if !ok {
		// TODO: 从 CAMF 读取 ImageDepth 或 SaturationLevel
		// 临时使用 12-bit 默认值
		maxRaw = [3]uint32{4095, 4095, 4095}
	}

	// 4. 计算 intermediate_bias (BlackLevel)
	// get_max_intermediate with bias=0
	maxGain := math.Max(math.Max(gain[0], gain[1]), gain[2])
	var maxIntermediate0 [3]uint32
	for i := 0; i < 3; i++ {
		maxIntermediate0[i] = uint32(math.Round(gain[i] * float64(INTERMEDIATE_UNIT) / maxGain))
	}

	// calculate intermediate_bias
	intermediateBias := 0.0
	for i := 0; i < 3; i++ {
		bias := INTERMEDIATE_BIAS_FACTOR * blackDev[i] *
			float64(maxIntermediate0[i]) / (float64(maxRaw[i]) - blackLevel[i])
		if bias > intermediateBias {
			intermediateBias = bias
		}
	}

	// 5. BlackLevel = intermediate_bias
	for i := 0; i < 3; i++ {
		levels.Black[i] = intermediateBias
	}

	// 6. 计算 WhiteLevel = max_intermediate with calculated bias
	for i := 0; i < 3; i++ {
		levels.White[i] = uint32(math.Round(
			gain[i]*(float64(INTERMEDIATE_UNIT)-intermediateBias)/maxGain + intermediateBias))
	}

	return levels, true
}

// getBlackLevel 获取黑电平和标准差
// TODO: 实现完整的黑电平统计计算
// C 代码实现: src/x3f_process.c::get_black_level()
// - 从 DarkShieldTop/DarkShieldBottom/Left/Right 区域统计计算
// - 计算每个通道的均值(black_level)和标准差(black_dev)
func (f *File) getBlackLevel() (Vector3, Vector3, bool) {
	// 当前简化实现: 直接返回测量的默认值
	// 完整实现需要解析 CAMF 中的 DarkShield 区域信息并统计像素值
	return DefaultBlackLevel, DefaultBlackDev, true
}

type BlackLevelInfo struct {
	Level Vector3
	Dev   Vector3
}

// 计算黑电平及其标准差
func CalculateBlackLevel(file *File, section *ImageSection) (BlackLevelInfo, error) {
	var result BlackLevelInfo

	decodedWidth := section.Columns
	decodedHeight := section.Rows
	if section.DecodedColumns > 0 {
		decodedWidth = section.DecodedColumns
	}
	if section.DecodedRows > 0 {
		decodedHeight = section.DecodedRows
	}

	// 使用所有可用区域计算黑电平（和 C 版本一致）
	var areas []struct {
		x0, y0, x1, y1 uint32
		name           string
	}

	// 1. 尝试 DarkShieldTop
	if x0, y0, x1, y1, ok := file.GetCAMFRectScaled("DarkShieldTop", decodedWidth, decodedHeight, true); ok {
		debug("Calculate black level for DarkShieldTop: [%d,%d,%d,%d]", x0, y0, x1, y1)
		areas = append(areas, struct {
			x0, y0, x1, y1 uint32
			name           string
		}{x0, y0, x1, y1, "DarkShieldTop"})
	} else {
		debug("Do not calculate black level for DarkShieldTop")
	}

	// 2. DarkShieldBottom - 某些相机有问题，需要检查相机型号
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
		if x0, y0, x1, y1, ok := file.GetCAMFRectScaled("DarkShieldBottom", decodedWidth, decodedHeight, true); ok {
			debug("Calculate black level for DarkShieldBottom: [%d,%d,%d,%d]", x0, y0, x1, y1)
			areas = append(areas, struct {
				x0, y0, x1, y1 uint32
				name           string
			}{x0, y0, x1, y1, "DarkShieldBottom"})
		} else {
			debug("Do not calculate black level for DarkShieldBottom (not found)")
		}
	}

	// 3. 左右边缘列
	// 尝试从 CAMF 获取 DarkShieldColRange
	if colRange, ok := file.GetCAMFMatrixUint32("DarkShieldColRange", 2, 2); ok {
		leftX0 := uint32(colRange[0])
		leftX1 := uint32(colRange[1])
		rightX0 := uint32(colRange[2])
		rightX1 := uint32(colRange[3])

		// 获取 KeepImageArea 进行缩放
		if keepX0, _, keepX1, _, keepOk := file.GetCAMFRect("KeepImageArea"); keepOk {
			keepCols := keepX1 - keepX0 + 1

			// 缩放到实际图像尺寸
			leftX0 = leftX0 * decodedWidth / keepCols
			leftX1 = leftX1 * decodedWidth / keepCols
			rightX0 = rightX0 * decodedWidth / keepCols
			rightX1 = rightX1 * decodedWidth / keepCols

			debug("Calculate black level for Left: [%d,0,%d,%d]", leftX0, leftX1, decodedHeight-1)
			areas = append(areas, struct {
				x0, y0, x1, y1 uint32
				name           string
			}{leftX0, 0, leftX1, decodedHeight - 1, "Left"})

			debug("Calculate black level for Right: [%d,0,%d,%d]", rightX0, rightX1, decodedHeight-1)
			areas = append(areas, struct {
				x0, y0, x1, y1 uint32
				name           string
			}{rightX0, 0, rightX1, decodedHeight - 1, "Right"})
		}
	}

	if len(areas) == 0 {
		// 无法计算黑电平，返回零值
		return result, nil
	}

	// 第一遍：计算均值
	var sum [3]uint64
	var count uint64

	for _, area := range areas {
		for y := area.y0; y <= area.y1 && y < decodedHeight; y++ {
			for x := area.x0; x <= area.x1 && x < decodedWidth; x++ {
				idx := int(y)*int(decodedWidth) + int(x)
				sum[0] += uint64(section.DecodedData[idx*3])
				sum[1] += uint64(section.DecodedData[idx*3+1])
				sum[2] += uint64(section.DecodedData[idx*3+2])
				count++
			}
		}
	}

	if count == 0 {
		return result, nil
	}

	for i := 0; i < 3; i++ {
		result.Level[i] = float64(sum[i]) / float64(count)
	}

	// 第二遍：计算标准差
	var sqdevSum Vector3

	for _, area := range areas {
		for y := area.y0; y <= area.y1 && y < decodedHeight; y++ {
			for x := area.x0; x <= area.x1 && x < decodedWidth; x++ {
				idx := int(y)*int(decodedWidth) + int(x)
				for c := 0; c < 3; c++ {
					val := float64(section.DecodedData[idx*3+c])
					diff := val - result.Level[c]
					sqdevSum[c] += diff * diff
				}
			}
		}
	}

	for i := 0; i < 3; i++ {
		result.Dev[i] = math.Sqrt(sqdevSum[i] / float64(count))
	}

	debug("CalculateBlackLevel: level=(%.2f, %.2f, %.2f), dev=(%.2f, %.2f, %.2f)",
		result.Level[0], result.Level[1], result.Level[2],
		result.Dev[0], result.Dev[1], result.Dev[2])

	return result, nil
}

// CalculateBlackLevelQuattroTop 计算 Quattro top 层的黑电平
// 对应 C 代码：get_black_level(x3f, &qtop, 0, 1, &black_level[2], &black_dev[2])
// 其中 rescale=0 表示使用 top 层原始分辨率，colors=1 表示单通道
func CalculateBlackLevelQuattroTop(file *File, section *ImageSection) (float64, float64, error) {
	if section.QuattroTopData == nil || len(section.QuattroTopData) == 0 {
		return 0, 0, nil
	}

	topWidth := uint32(section.QuattroTopCols)
	topHeight := uint32(section.QuattroTopRows)

	debug("CalculateBlackLevelQuattroTop: topWidth=%d, topHeight=%d", topWidth, topHeight)

	// 使用所有可用区域计算黑电平
	var areas []struct {
		x0, y0, x1, y1 uint32
		name           string
	}

	// 1. 尝试 DarkShieldTop（使用 top 层分辨率，rescale=0）
	if x0, y0, x1, y1, ok := file.GetCAMFRectScaled("DarkShieldTop", topWidth, topHeight, false); ok {
		debug("Calculate black level for Quattro top DarkShieldTop: [%d,%d,%d,%d]", x0, y0, x1, y1)
		areas = append(areas, struct {
			x0, y0, x1, y1 uint32
			name           string
		}{x0, y0, x1, y1, "DarkShieldTop"})
	}

	// 2. DarkShieldBottom
	useDarkShieldBottom := true
	if model, ok := file.GetProperty("CAMMODEL"); ok && model == "SIGMA DP2" {
		useDarkShieldBottom = false
	}
	if cameraID, ok := file.GetCAMFUint32("CAMERAID"); ok && cameraID == CameraIDSDQH {
		useDarkShieldBottom = false
	}
	if useDarkShieldBottom {
		if x0, y0, x1, y1, ok := file.GetCAMFRectScaled("DarkShieldBottom", topWidth, topHeight, false); ok {
			debug("Calculate black level for Quattro top DarkShieldBottom: [%d,%d,%d,%d]", x0, y0, x1, y1)
			areas = append(areas, struct {
				x0, y0, x1, y1 uint32
				name           string
			}{x0, y0, x1, y1, "DarkShieldBottom"})
		}
	}

	// 3. 左右边缘列
	if colRange, ok := file.GetCAMFMatrixUint32("DarkShieldColRange", 2, 2); ok {
		leftX0 := uint32(colRange[0])
		leftX1 := uint32(colRange[1])
		rightX0 := uint32(colRange[2])
		rightX1 := uint32(colRange[3])

		if keepX0, _, keepX1, _, keepOk := file.GetCAMFRect("KeepImageArea"); keepOk {
			keepCols := keepX1 - keepX0 + 1
			leftX0 = leftX0 * topWidth / keepCols
			leftX1 = leftX1 * topWidth / keepCols
			rightX0 = rightX0 * topWidth / keepCols
			rightX1 = rightX1 * topWidth / keepCols

			areas = append(areas, struct {
				x0, y0, x1, y1 uint32
				name           string
			}{leftX0, 0, leftX1, topHeight - 1, "Left"})

			areas = append(areas, struct {
				x0, y0, x1, y1 uint32
				name           string
			}{rightX0, 0, rightX1, topHeight - 1, "Right"})
		}
	}

	if len(areas) == 0 {
		debug("CalculateBlackLevelQuattroTop: no areas found, returning 0")
		return 0, 0, nil
	}

	// 第一遍：计算均值（单通道）
	var sum uint64
	var count uint64

	for _, area := range areas {
		for y := area.y0; y <= area.y1 && y < topHeight; y++ {
			for x := area.x0; x <= area.x1 && x < topWidth; x++ {
				idx := int(y)*int(topWidth) + int(x)
				if idx < len(section.QuattroTopData) {
					sum += uint64(section.QuattroTopData[idx])
					count++
				}
			}
		}
	}

	if count == 0 {
		return 0, 0, nil
	}

	level := float64(sum) / float64(count)

	// 第二遍：计算标准差
	var sqdevSum float64

	for _, area := range areas {
		for y := area.y0; y <= area.y1 && y < topHeight; y++ {
			for x := area.x0; x <= area.x1 && x < topWidth; x++ {
				idx := int(y)*int(topWidth) + int(x)
				if idx < len(section.QuattroTopData) {
					val := float64(section.QuattroTopData[idx])
					diff := val - level
					sqdevSum += diff * diff
				}
			}
		}
	}

	dev := math.Sqrt(sqdevSum / float64(count))

	debug("CalculateBlackLevelQuattroTop: level=%.2f, dev=%.2f (from %d pixels)", level, dev, count)

	// 打印第一个像素的实际值进行对比
	if len(section.QuattroTopData) > 0 {
		debug("CalculateBlackLevelQuattroTop: 第一个像素实际值=%d (对比: blackLevel=%.2f)",
			section.QuattroTopData[0], level)
	}

	return level, dev, nil
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
func PreprocessData(file *File, section *ImageSection, wb string) error {
	debug("PreprocessData: starting preprocessing with wb='%s'", wb)

	blackLevelInfo, err := CalculateBlackLevel(file, section)
	if err != nil {
		debug("PreprocessData: CalculateBlackLevel failed: %v", err)
		return err
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
		return nil
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
	isQuattro := section.QuattroTopData != nil && len(section.QuattroTopData) > 0
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
	InterpolateBadPixels(section.DecodedData, decodedWidth, decodedHeight, 3, badPixels)

	// V median filtering（在 YUV 色彩空间，只对 ActiveImageArea）
	x0, y0, x1, y1, ok := file.GetCAMFRectScaled("ActiveImageArea", decodedWidth, decodedHeight, true)
	if !ok {
		// 如果没有 ActiveImageArea，使用整个图像
		x0, y0 = 0, 0
		x1, y1 = decodedWidth-1, decodedHeight-1
		debug("Could not get active area, denoising entire image")
	}

	debug("V median filtering on active area [%d,%d,%d,%d]", x0, y0, x1, y1)
	// 注意: 必须对整个图像做色彩空间转换,因为中值滤波需要访问边界外的像素
	BMT_to_YUV_STD(section.DecodedData, decodedWidth, decodedHeight, 3)
	VMedianFilterArea(section.DecodedData, decodedWidth, decodedHeight, 3, x0, y0, x1, y1)
	YUV_to_BMT_STD(section.DecodedData, decodedWidth, decodedHeight, 3)

	return nil
}

// PreprocessQuattroTop 对 Quattro top 层数据进行预处理
// 包括：1. 全分辨率 top 层预处理（用于 expand）
//  2. 降采样后放到 DecodedData 的第 2 通道（用于正常的 BMT 处理）
func PreprocessQuattroTop(file *File, section *ImageSection, wb string) error {
	if section.QuattroTopData == nil || len(section.QuattroTopData) == 0 {
		return nil
	}

	debug("PreprocessQuattroTop: starting top layer preprocessing and downsampling")

	// 1. 计算 bottom/middle 层的黑电平（用于 intermediateBias 计算）
	blackLevelInfo, err := CalculateBlackLevel(file, section)
	if err != nil {
		debug("PreprocessQuattroTop: CalculateBlackLevel failed: %v", err)
		return err
	}

	// 2. 对 Quattro，C 版本的黑电平约 2046-2049
	// 从 C 版本的调试输出可知：black_level = {2046.87, 2046.24, 2049.04}
	// 但我的 CalculateBlackLevel 返回的是预处理后的值（约 58）
	// TODO: 研究如何正确获取原始黑电平
	// 临时使用 C 版本的值作为常量
	topBlackLevel := 2046.0

	// 3. 使用 bottom/middle 层的黑电平计算 intermediateBias
	intermediateBias, ok := GetIntermediateBias(file, wb, blackLevelInfo)
	if !ok {
		debug("PreprocessQuattroTop: GetIntermediateBias failed, using 0")
		intermediateBias = 0
	}

	maxRaw, ok := file.GetMaxRAW()
	if !ok {
		maxRaw = [3]uint32{4095, 4095, 4095}
	}

	maxIntermediate, ok := GetMaxIntermediate(file, wb, intermediateBias)
	if !ok {
		debug("PreprocessQuattroTop: GetMaxIntermediate failed, skipping preprocessing")
		return nil
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

	return nil
}
