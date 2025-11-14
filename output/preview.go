package output

import (
	"encoding/binary"

	"github.com/weaming/x3f-go/x3f"
)

// PreviewParams 预览图生成参数
type PreviewParams struct {
	reduction     uint32
	reduction2    uint32
	previewWidth  uint32
	previewHeight uint32
	levels        x3f.ImageLevels
	convMatrix    x3f.Matrix3x3
}

// 从全分辨率 16-bit linear sRGB 图像生成 8-bit sRGB 预览图
// imageData: 已经转换为 XYZ 色彩空间的图像数据 (uint16, 范围 0-65535)
func generatePreviewImage(imageData []byte, width, height uint32, maxWidth uint32) ([]byte, uint32, uint32) {
	params := preparePreviewParams(width, height, maxWidth)
	previewData := make([]byte, params.previewWidth*params.previewHeight*3)
	processPreviewPixels(imageData, width, previewData, params)
	return previewData, params.previewWidth, params.previewHeight
}

// 准备预览图生成参数
func preparePreviewParams(width, height, maxWidth uint32) PreviewParams {
	reduction := calculateReduction(width, maxWidth)
	convMatrix := x3f.GetColorMatrix1()

	return PreviewParams{
		reduction:     reduction,
		reduction2:    reduction * reduction,
		previewWidth:  width / reduction,
		previewHeight: height / reduction,
		levels:        stdLevels,
		convMatrix:    convMatrix,
	}
}

// 计算缩放因子 (C 代码算法)
func calculateReduction(width, maxWidth uint32) uint32 {
	reduction := (width + maxWidth - 1) / maxWidth
	if reduction < 1 {
		return 1
	}
	return reduction
}

// 处理所有预览图像素（旧版本，从 uint16）
func processPreviewPixels(imageData []byte, width uint32, previewData []byte, params PreviewParams) {
	for row := uint32(0); row < params.previewHeight; row++ {
		for col := uint32(0); col < params.previewWidth; col++ {
			input := downsamplePixelBlock(imageData, width, row, col, params)
			normalized := normalizePixel(input, params.levels)
			rgb := applyColorConvForPreview(normalized, params.convMatrix)
			writePixel(previewData, row, col, params.previewWidth, rgb)
		}
	}
}

// 平均下采样像素块（旧版本，从 uint16）
func downsamplePixelBlock(imageData []byte, width uint32, row, col uint32, params PreviewParams) x3f.Vector3 {
	var input x3f.Vector3

	for color := 0; color < 3; color++ {
		var acc uint32
		for r := uint32(0); r < params.reduction; r++ {
			for c := uint32(0); c < params.reduction; c++ {
				srcRow := row*params.reduction + r
				srcCol := col*params.reduction + c
				srcIdx := (srcRow*width + srcCol) * 6
				value := binary.LittleEndian.Uint16(imageData[srcIdx+uint32(color)*2:])
				acc += uint32(value)
			}
		}
		input[color] = float64(acc) / float64(params.reduction2)
	}

	return input
}

// 归一化像素值到 [0, 1]
func normalizePixel(input x3f.Vector3, levels x3f.ImageLevels) x3f.Vector3 {
	var normalized x3f.Vector3

	for i := 0; i < 3; i++ {
		value := (input[i] - levels.Black[i]) / (float64(levels.White[i]) - levels.Black[i])
		if value < 0 {
			value = 0
		} else if value > 1 {
			value = 1
		}
		normalized[i] = value
	}

	return normalized
}

// 应用色彩转换和 gamma 校正
func applyColorConvForPreview(raw x3f.Vector3, convMatrix x3f.Matrix3x3) x3f.Vector3 {
	rgb := convMatrix.Apply(raw)
	rgb = x3f.ApplySRGBGamma(rgb)
	return rgb
}

// 写入 8-bit 像素值
func writePixel(previewData []byte, row, col, width uint32, rgb x3f.Vector3) {
	rgb8 := convertToUint8WithProportionalClip(rgb)
	dstIdx := (row*width + col) * 3
	previewData[dstIdx] = rgb8[0]
	previewData[dstIdx+1] = rgb8[1]
	previewData[dstIdx+2] = rgb8[2]
}

// convertToUint8WithProportionalClip 转换为 8-bit，保持色彩通道比例
func convertToUint8WithProportionalClip(rgb x3f.Vector3) [3]uint8 {
	val0 := rgb[0] * 255.0
	val1 := rgb[1] * 255.0
	val2 := rgb[2] * 255.0

	// 如果有任何通道超过 255，按比例缩放所有通道以保持色彩比例
	maxVal := val0
	if val1 > maxVal {
		maxVal = val1
	}
	if val2 > maxVal {
		maxVal = val2
	}

	if maxVal > 255.0 {
		scale := 255.0 / maxVal
		val0 *= scale
		val1 *= scale
		val2 *= scale
	}

	// 处理负值（裁剪到 0）
	if val0 < 0 {
		val0 = 0
	}
	if val1 < 0 {
		val1 = 0
	}
	if val2 < 0 {
		val2 = 0
	}

	return [3]uint8{
		uint8(val0 + 0.5),
		uint8(val1 + 0.5),
		uint8(val2 + 0.5),
	}
}
