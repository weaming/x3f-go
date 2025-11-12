package output

import (
	"encoding/binary"

	"github.com/weaming/x3f-go/colorspace"
	"github.com/weaming/x3f-go/x3f"
)

// generatePreviewImage 从全分辨率 16-bit Linear Raw 图像生成 8-bit sRGB 预览图
// maxWidth: 预览图的最大宽度（高度按比例缩放）
// imageData: 16-bit Linear Raw 图像数据 (R,G,B,R,G,B,...)
// width, height: 原始图像尺寸
// x3fFile: X3F 文件对象（用于获取色彩矩阵）
// wb: 白平衡名称
func generatePreviewImage(imageData []byte, width, height uint32, maxWidth uint32, x3fFile *x3f.File, wb string) ([]byte, uint32, uint32) {
	// C 代码缩放算法: reduction = (width + maxWidth - 1) / maxWidth
	reduction := (width + maxWidth - 1) / maxWidth
	reduction2 := reduction * reduction

	previewWidth := width / reduction
	previewHeight := height / reduction

	if reduction < 1 {
		reduction = 1
		reduction2 = 1
		previewWidth = width
		previewHeight = height
	}

	// 应用色彩转换 (Linear Raw -> sRGB)
	wbGain, ok := x3fFile.GetWhiteBalanceGain(wb)
	if !ok {
		wbGain = [3]float64{1.0, 1.0, 1.0}
	}

	// 获取 black level 和 white level
	levels, ok := x3fFile.GetImageLevelsWithGain(wbGain)
	if !ok {
		levels = x3f.ImageLevels{
			Black: [3]float64{168.756, 168.756, 168.756},
			White: [3]uint32{16383, 16383, 16383},
		}
	}

	// 获取 ISO 缩放因子 (C 代码: capture_iso / sensor_iso)
	isoScaling := 1.0
	if sensorISO, ok := x3fFile.GetCAMFFloat("SensorISO"); ok {
		if captureISO, ok := x3fFile.GetCAMFFloat("CaptureISO"); ok {
			isoScaling = captureISO / sensorISO
		}
	}

	// 获取色彩转换矩阵 (RAW -> XYZ, 包含白平衡增益)
	// C 代码: x3f_get_raw_to_xyz = bmt_to_xyz × diag(gain)
	rawToXYZSlice, ok := x3fFile.GetRawToXYZ(wb)
	if !ok {
		// 如果无法获取，使用 sRGB_to_XYZ 作为备选
		rawToXYZSlice = x3f.GetSRGBToXYZMatrix()
	}

	// 转换为 Matrix3x3
	var rawToXYZ colorspace.Matrix3x3
	copy(rawToXYZ[:], rawToXYZSlice)

	// XYZ_to_sRGB 标准矩阵
	xyzToSRGBSlice := x3f.GetColorMatrix1ForDNG()
	var xyzToSRGB colorspace.Matrix3x3
	copy(xyzToSRGB[:], xyzToSRGBSlice)

	// 计算最终的转换矩阵: xyz_to_sRGB × raw_to_xyz
	// C 代码: x3f_3x3_3x3_mul(xyz_to_rgb, raw_to_xyz, raw_to_rgb)
	// 然后应用 ISO 缩放: x3f_scalar_3x3_mul(iso_scaling, raw_to_rgb, conv_matrix)
	rawToSRGB := xyzToSRGB.Multiply(rawToXYZ)

	// 应用 ISO 缩放
	for i := range rawToSRGB {
		rawToSRGB[i] *= isoScaling
	}

	// 创建 8-bit sRGB 预览图缓冲区
	previewData := make([]byte, previewWidth*previewHeight*3)

	// Debug: 打印一些关键参数
	if previewHeight > 0 && previewWidth > 0 {
		_ = rawToSRGB // 避免未使用警告
	}

	// 色彩转换：Linear Raw -> sRGB (使用平均下采样)
	// C 代码: x3f_process.c:966-995
	for row := uint32(0); row < previewHeight; row++ {
		for col := uint32(0); col < previewWidth; col++ {
			// 对每个颜色通道进行平均下采样
			var input [3]float64

			for color := 0; color < 3; color++ {
				var acc uint32

				// 平均 reduction × reduction 的像素块
				for r := uint32(0); r < reduction; r++ {
					for c := uint32(0); c < reduction; c++ {
						srcRow := row*reduction + r
						srcCol := col*reduction + c

						// 读取 16-bit Linear Raw 值
						srcIdx := (srcRow*width + srcCol) * 6
						value := binary.LittleEndian.Uint16(imageData[srcIdx+uint32(color)*2:])
						acc += uint32(value)
					}
				}

				// 应用黑电平和白电平归一化
				avgValue := float64(acc) / float64(reduction2)
				input[color] = (avgValue - levels.Black[color]) / (float64(levels.White[color]) - levels.Black[color])

				// 限制范围 [0, 1]
				if input[color] < 0 {
					input[color] = 0
				} else if input[color] > 1 {
					input[color] = 1
				}
			}

			// RAW -> sRGB (使用预计算的组合矩阵)
			// C 代码: x3f_3x3_3x1_mul(conv_matrix, input, output)
			raw := colorspace.Vector3(input)
			rgb := rawToSRGB.Apply(raw)

			// 应用 sRGB gamma 校正
			rgb = colorspace.ApplySRGBGamma(rgb)

			// 转换为 8-bit
			rgb8 := colorspace.ConvertToUint8(rgb)

			// 写入 8-bit sRGB 值
			dstIdx := (row*previewWidth + col) * 3
			previewData[dstIdx] = rgb8[0]
			previewData[dstIdx+1] = rgb8[1]
			previewData[dstIdx+2] = rgb8[2]
		}
	}

	return previewData, previewWidth, previewHeight
}
