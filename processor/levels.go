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

	// 尝试从 CAMF 获取 DarkShieldTop 区域
	var areas []struct {
		x0, y0, x1, y1 uint32
		name           string
	}

	if x0, y0, x1, y1, ok := file.GetCAMFRect("DarkShieldTop"); ok {
		areas = append(areas, struct {
			x0, y0, x1, y1 uint32
			name           string
		}{x0, y0, x1, y1, "DarkShieldTop"})
	}

	// 对于某些相机，DarkShieldBottom 有问题，暂时不使用

	// 如果没有 DarkShield 区域，使用左右边缘列
	if len(areas) == 0 {
		// 使用左边 64 列和右边 64 列作为黑电平区域
		const edgeWidth = 64
		if decodedWidth > edgeWidth*2 {
			areas = append(areas, struct {
				x0, y0, x1, y1 uint32
				name           string
			}{0, 0, edgeWidth - 1, decodedHeight - 1, "LeftEdge"})

			areas = append(areas, struct {
				x0, y0, x1, y1 uint32
				name           string
			}{decodedWidth - edgeWidth, 0, decodedWidth - 1, decodedHeight - 1, "RightEdge"})
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

	debug("PreprocessData: scale=(%.6f, %.6f, %.6f), intermediateBias=%.2f",
		scale[0], scale[1], scale[2], intermediateBias)

	// 应用预处理到每个像素
	decodedWidth := section.Columns
	decodedHeight := section.Rows
	if section.DecodedColumns > 0 {
		decodedWidth = section.DecodedColumns
	}
	if section.DecodedRows > 0 {
		decodedHeight = section.DecodedRows
	}

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

	return nil
}
