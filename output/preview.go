package output

import (
	"encoding/binary"
	"fmt"

	"github.com/weaming/x3f-go/colorspace"
	"github.com/weaming/x3f-go/x3f"
)

// PreviewParams 预览图生成参数
type PreviewParams struct {
	reduction     uint32
	reduction2    uint32
	previewWidth  uint32
	previewHeight uint32
	levels        x3f.ImageLevels
	convMatrix    colorspace.Matrix3x3
}

// generatePreviewImage 从全分辨率 16-bit Linear Raw 图像生成 8-bit sRGB 预览图
func generatePreviewImage(imageData []byte, width, height uint32, maxWidth uint32, x3fFile *x3f.File, wb string) ([]byte, uint32, uint32) {
	params := preparePreviewParams(width, height, maxWidth, x3fFile, wb)
	previewData := make([]byte, params.previewWidth*params.previewHeight*3)
	processPreviewPixels(imageData, width, previewData, params)
	return previewData, params.previewWidth, params.previewHeight
}

// preparePreviewParams 准备预览图生成参数
func preparePreviewParams(width, height, maxWidth uint32, x3fFile *x3f.File, wb string) PreviewParams {
	reduction := calculateReduction(width, maxWidth)
	levels := getImageLevelsForWb(x3fFile, wb)
	convMatrix := buildConversionMatrix(x3fFile, wb)

	return PreviewParams{
		reduction:     reduction,
		reduction2:    reduction * reduction,
		previewWidth:  width / reduction,
		previewHeight: height / reduction,
		levels:        levels,
		convMatrix:    convMatrix,
	}
}

// calculateReduction 计算缩放因子 (C 代码算法)
func calculateReduction(width, maxWidth uint32) uint32 {
	reduction := (width + maxWidth - 1) / maxWidth
	if reduction < 1 {
		return 1
	}
	return reduction
}

// 获取黑白电平
func getImageLevelsForWb(x3fFile *x3f.File, wb string) x3f.ImageLevels {
	wbGain, ok := x3fFile.GetWhiteBalanceGain(wb)
	if !ok {
		panic(fmt.Errorf("无法获取白平衡增益: %s", wb))
	}

	levels, ok := x3fFile.GetImageLevelsWithGain(wbGain)
	if !ok {
		panic(fmt.Errorf("无法获取图像电平"))
	}
	return levels
}

// buildConversionMatrix 构建色彩转换矩阵
func buildConversionMatrix(x3fFile *x3f.File, wb string) colorspace.Matrix3x3 {
	rawToXYZ := getRawToXYZMatrix(x3fFile, wb)
	xyzToSRGB := getXYZToSRGBMatrix()
	isoScaling := getISOScaling(x3fFile)
	return combineMatrices(xyzToSRGB, rawToXYZ, isoScaling)
}

// getRawToXYZMatrix 获取 RAW -> XYZ 矩阵 (包含白平衡增益)
func getRawToXYZMatrix(x3fFile *x3f.File, wb string) colorspace.Matrix3x3 {
	rawToXYZSlice, ok := x3fFile.GetRawToXYZ(wb)
	if !ok {
		rawToXYZSlice = x3f.GetSRGBToXYZMatrix()
	}

	var matrix colorspace.Matrix3x3
	copy(matrix[:], rawToXYZSlice)
	return matrix
}

// getXYZToSRGBMatrix 获取 XYZ -> sRGB 标准矩阵
func getXYZToSRGBMatrix() colorspace.Matrix3x3 {
	xyzToSRGBSlice := x3f.GetColorMatrix1ForDNG()
	var matrix colorspace.Matrix3x3
	copy(matrix[:], xyzToSRGBSlice)
	return matrix
}

// getISOScaling 获取 ISO 缩放因子
func getISOScaling(x3fFile *x3f.File) float64 {
	sensorISO, ok1 := x3fFile.GetCAMFFloat("SensorISO")
	captureISO, ok2 := x3fFile.GetCAMFFloat("CaptureISO")
	if ok1 && ok2 {
		return captureISO / sensorISO
	}
	return 1.0
}

// combineMatrices 组合转换矩阵并应用 ISO 缩放
func combineMatrices(xyzToSRGB, rawToXYZ colorspace.Matrix3x3, isoScaling float64) colorspace.Matrix3x3 {
	result := xyzToSRGB.Multiply(rawToXYZ)
	for i := range result {
		result[i] *= isoScaling
	}
	return result
}

// processPreviewPixels 处理所有预览图像素
func processPreviewPixels(imageData []byte, width uint32, previewData []byte, params PreviewParams) {
	for row := uint32(0); row < params.previewHeight; row++ {
		for col := uint32(0); col < params.previewWidth; col++ {
			input := downsamplePixelBlock(imageData, width, row, col, params)
			normalized := normalizePixel(input, params.levels)
			rgb := applyColorConversion(normalized, params.convMatrix)
			writePixel(previewData, row, col, params.previewWidth, rgb)
		}
	}
}

// downsamplePixelBlock 平均下采样像素块
func downsamplePixelBlock(imageData []byte, width uint32, row, col uint32, params PreviewParams) [3]float64 {
	var input [3]float64

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

// normalizePixel 归一化像素值到 [0, 1]
func normalizePixel(input [3]float64, levels x3f.ImageLevels) colorspace.Vector3 {
	var normalized colorspace.Vector3

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

// applyColorConversion 应用色彩转换和 gamma 校正
func applyColorConversion(raw colorspace.Vector3, convMatrix colorspace.Matrix3x3) colorspace.Vector3 {
	rgb := convMatrix.Apply(raw)
	rgb = colorspace.ApplySRGBGamma(rgb)
	return rgb
}

// writePixel 写入 8-bit 像素值
func writePixel(previewData []byte, row, col, width uint32, rgb colorspace.Vector3) {
	rgb8 := colorspace.ConvertToUint8(rgb)
	dstIdx := (row*width + col) * 3
	previewData[dstIdx] = rgb8[0]
	previewData[dstIdx+1] = rgb8[1]
	previewData[dstIdx+2] = rgb8[2]
}
