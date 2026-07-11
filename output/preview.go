package output

import (
	"encoding/binary"

	"github.com/weaming/x3f-go/x3f"
)

// generateLinearSRGBPreview 从 baked linear sRGB 数据生成 SDR 预览。
func generateLinearSRGBPreview(imageData []byte, width, height, maxWidth uint32) ([]byte, uint32, uint32) {
	reduction := calculateReduction(width, maxWidth)
	previewWidth := width / reduction
	previewHeight := height / reduction
	preview := make([]byte, previewWidth*previewHeight*3)

	for row := uint32(0); row < previewHeight; row++ {
		for col := uint32(0); col < previewWidth; col++ {
			var average x3f.Vector3
			for reductionY := uint32(0); reductionY < reduction; reductionY++ {
				for reductionX := uint32(0); reductionX < reduction; reductionX++ {
					sourceX := col*reduction + reductionX
					sourceY := row*reduction + reductionY
					sourceIndex := (sourceY*width + sourceX) * 6
					average[0] += float64(binary.LittleEndian.Uint16(imageData[sourceIndex:]))
					average[1] += float64(binary.LittleEndian.Uint16(imageData[sourceIndex+2:]))
					average[2] += float64(binary.LittleEndian.Uint16(imageData[sourceIndex+4:]))
				}
			}

			average = average.Scale(1.0 / float64(reduction*reduction*65535))
			writePixel(preview, row, col, previewWidth, x3f.ApplySRGBGamma(average))
		}
	}

	return preview, previewWidth, previewHeight
}

// 计算缩放因子 (C 代码算法)
func calculateReduction(width, maxWidth uint32) uint32 {
	reduction := (width + maxWidth - 1) / maxWidth
	if reduction < 1 {
		return 1
	}
	return reduction
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
