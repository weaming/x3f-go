package output

import (
	"encoding/binary"
	"math"

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
	// 计算缩放比例
	scale := float64(maxWidth) / float64(width)
	previewWidth := maxWidth
	previewHeight := uint32(math.Round(float64(height) * scale))

	if scale >= 1.0 {
		previewWidth = width
		previewHeight = height
		scale = 1.0
	}

	// 创建 16-bit 预览图缓冲区（用于色彩转换）
	preview16Data := make([]uint16, previewWidth*previewHeight*3)

	// 简单的最近邻插值缩放（16-bit）
	for py := uint32(0); py < previewHeight; py++ {
		for px := uint32(0); px < previewWidth; px++ {
			// 映射到原始图像坐标
			sx := uint32(float64(px) / scale)
			sy := uint32(float64(py) / scale)

			// 确保不越界
			if sx >= width {
				sx = width - 1
			}
			if sy >= height {
				sy = height - 1
			}

			// 读取原始 16-bit Linear Raw 像素
			srcIdx := (sy*width + sx) * 6 // 16-bit = 2 bytes per channel
			dstIdx := (py*previewWidth + px) * 3

			// 读取 16-bit 值（Little Endian）
			preview16Data[dstIdx] = binary.LittleEndian.Uint16(imageData[srcIdx:])     // R
			preview16Data[dstIdx+1] = binary.LittleEndian.Uint16(imageData[srcIdx+2:]) // G
			preview16Data[dstIdx+2] = binary.LittleEndian.Uint16(imageData[srcIdx+4:]) // B
		}
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

	// 获取色彩转换矩阵 (RAW -> XYZ)
	bmtToXYZSlice, ok := x3fFile.GetBMTToXYZ(wb)
	if !ok {
		// 如果无法获取，使用 sRGB_to_XYZ 作为备选
		bmtToXYZSlice = x3f.GetSRGBToXYZMatrix()
	}

	// 转换为 Matrix3x3
	var rawToXYZ colorspace.Matrix3x3
	copy(rawToXYZ[:], bmtToXYZSlice)

	// XYZ_to_sRGB 标准矩阵
	xyzToSRGBSlice := x3f.GetColorMatrix1ForDNG()
	var xyzToSRGB colorspace.Matrix3x3
	copy(xyzToSRGB[:], xyzToSRGBSlice)

	// 创建 8-bit sRGB 预览图缓冲区
	previewData := make([]byte, previewWidth*previewHeight*3)

	// 色彩转换：Linear Raw -> sRGB
	for i := uint32(0); i < previewWidth*previewHeight; i++ {
		srcIdx := i * 3

		// 读取 16-bit Linear Raw 值
		r := float64(preview16Data[srcIdx])
		g := float64(preview16Data[srcIdx+1])
		b := float64(preview16Data[srcIdx+2])

		// 应用黑电平和白电平归一化
		r = (r - levels.Black[0]) / (float64(levels.White[0]) - levels.Black[0])
		g = (g - levels.Black[1]) / (float64(levels.White[1]) - levels.Black[1])
		b = (b - levels.Black[2]) / (float64(levels.White[2]) - levels.Black[2])

		// 限制范围 [0, 1]
		if r < 0 {
			r = 0
		} else if r > 1 {
			r = 1
		}
		if g < 0 {
			g = 0
		} else if g > 1 {
			g = 1
		}
		if b < 0 {
			b = 0
		} else if b > 1 {
			b = 1
		}

		// RAW -> XYZ -> sRGB
		raw := colorspace.Vector3{r, g, b}
		xyz := colorspace.ConvertRAWToXYZ(raw, rawToXYZ)
		rgb := colorspace.ConvertXYZToRGB(xyz, xyzToSRGB)

		// 应用 sRGB gamma 校正
		rgb = colorspace.ApplySRGBGamma(rgb)

		// 转换为 8-bit
		rgb8 := colorspace.ConvertToUint8(rgb)

		// 写入 8-bit sRGB 值
		previewData[srcIdx] = rgb8[0]
		previewData[srcIdx+1] = rgb8[1]
		previewData[srcIdx+2] = rgb8[2]
	}

	return previewData, previewWidth, previewHeight
}
