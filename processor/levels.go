package processor

import (
	"math"

	"github.com/weaming/x3f-go/x3f"
)

const (
	INTERMEDIATE_DEPTH       = 14
	INTERMEDIATE_UNIT        = (1 << INTERMEDIATE_DEPTH) - 1
	INTERMEDIATE_BIAS_FACTOR = 4.0
)

type BlackLevelInfo struct {
	Level [3]float64
	Dev   [3]float64
}

// CalculateBlackLevel 计算黑电平及其标准差
func CalculateBlackLevel(file *x3f.File, section *x3f.ImageSection) (BlackLevelInfo, error) {
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
	if cameraID, ok := file.GetCAMFUint32("CAMERAID"); ok && cameraID == 0x10 { // X3F_CAMERAID_SDQH
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
	var sqdevSum [3]float64

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

// GetMaxIntermediate 计算 intermediate 表示的最大值
func GetMaxIntermediate(file *x3f.File, wb string, intermediateBias float64) ([3]uint32, bool) {
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

	debug("GetMaxIntermediate: gain=(%.4f, %.4f, %.4f), maxGain=%.4f, bias=%.2f, result=(%d, %d, %d)",
		gain[0], gain[1], gain[2], maxGain, intermediateBias,
		result[0], result[1], result[2])

	return result, true
}

// GetIntermediateBias 计算 intermediate bias
func GetIntermediateBias(file *x3f.File, wb string, blackLevel BlackLevelInfo) (float64, bool) {
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

// PreprocessData 应用 C 版本的预处理逻辑
func PreprocessData(file *x3f.File, section *x3f.ImageSection, wb string) error {
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
	var scale [3]float64
	for i := 0; i < 3; i++ {
		white := float64(maxIntermediate[i])
		black := intermediateBias
		scale[i] = (white - black) / (float64(maxRaw[i]) - blackLevelInfo.Level[i])
	}

	debug("[PREPROC] maxRaw=(%d, %d, %d), blackLevel=(%.2f, %.2f, %.2f)",
		maxRaw[0], maxRaw[1], maxRaw[2],
		blackLevelInfo.Level[0], blackLevelInfo.Level[1], blackLevelInfo.Level[2])
	debug("[PREPROC] maxIntermediate=(%d, %d, %d), intermediateBias=%.2f",
		maxIntermediate[0], maxIntermediate[1], maxIntermediate[2], intermediateBias)
	debug("[PREPROC] scale=(%.6f, %.6f, %.6f)",
		scale[0], scale[1], scale[2])

	// 应用预处理到每个像素
	decodedWidth := section.Columns
	decodedHeight := section.Rows
	if section.DecodedColumns > 0 {
		decodedWidth = section.DecodedColumns
	}
	if section.DecodedRows > 0 {
		decodedHeight = section.DecodedRows
	}

	// 应用预处理到每个像素
	for y := uint32(0); y < decodedHeight; y++ {
		for x := uint32(0); x < decodedWidth; x++ {
			idx := int(y)*int(decodedWidth) + int(x)

			for c := 0; c < 3; c++ {
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
