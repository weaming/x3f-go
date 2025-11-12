package processor

import (
	"sort"
)

const O_UV = 32768 // 避免 U,V 负值被裁剪

// BMT_to_YUV_STD 将 BMT 色彩空间转换为 YUV (标准模式)
// 转换矩阵:
//
//	Y = (B + M + T) / 3
//	U = 2*B - 2*T
//	V = B - 2*M + T
func BMT_to_YUV_STD(imageData []uint16, imageWidth, imageHeight, channels uint32) {
	if channels != 3 {
		return
	}

	width := int(imageWidth)
	height := int(imageHeight)
	chans := int(channels)
	rowStride := width * chans

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			idx := y*rowStride + x*chans

			B := int32(imageData[idx+0])
			M := int32(imageData[idx+1])
			T := int32(imageData[idx+2])

			Y := (B + M + T + 1) / 3 // +1 for rounding
			U := 2*B - 2*T
			V := B - 2*M + T

			imageData[idx+0] = clampUint16(Y)
			imageData[idx+1] = clampUint16(U + O_UV)
			imageData[idx+2] = clampUint16(V + O_UV)
		}
	}
}

// YUV_to_BMT_STD 将 YUV 色彩空间转换回 BMT (标准模式)
// 转换矩阵:
//
//	B = (12*Y + 3*U + 2*V) / 12
//	M = (3*Y - V) / 3
//	T = (12*Y - 3*U + 2*V) / 12
func YUV_to_BMT_STD(imageData []uint16, imageWidth, imageHeight, channels uint32) {
	if channels != 3 {
		return
	}

	width := int(imageWidth)
	height := int(imageHeight)
	chans := int(channels)
	rowStride := width * chans

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			idx := y*rowStride + x*chans

			Y := int32(imageData[idx+0])
			U := int32(imageData[idx+1]) - O_UV
			V := int32(imageData[idx+2]) - O_UV

			B := (12*Y + 3*U + 2*V + 6) / 12 // +6 for rounding
			M := (3*Y - V + 1) / 3           // +1 for rounding
			T := (12*Y - 3*U + 2*V + 6) / 12 // +6 for rounding

			imageData[idx+0] = clampUint16(B)
			imageData[idx+1] = clampUint16(M)
			imageData[idx+2] = clampUint16(T)
		}
	}
}

// clampUint16 将 int32 值限制在 uint16 范围内
func clampUint16(val int32) uint16 {
	if val < 0 {
		return 0
	}
	if val > 65535 {
		return 65535
	}
	return uint16(val)
}

// BMT_to_YUV_STD_Area 只对指定区域进行 BMT → YUV 转换
func BMT_to_YUV_STD_Area(imageData []uint16, imageWidth, imageHeight, channels, x0, y0, x1, y1 uint32) {
	if channels != 3 {
		return
	}

	width := int(imageWidth)
	chans := int(channels)
	rowStride := width * chans

	for y := int(y0); y <= int(y1); y++ {
		for x := int(x0); x <= int(x1); x++ {
			idx := y*rowStride + x*chans

			B := int32(imageData[idx+0])
			M := int32(imageData[idx+1])
			T := int32(imageData[idx+2])

			Y := (B + M + T + 1) / 3
			U := 2*B - 2*T
			V := B - 2*M + T

			imageData[idx+0] = clampUint16(Y)
			imageData[idx+1] = clampUint16(U + O_UV)
			imageData[idx+2] = clampUint16(V + O_UV)
		}
	}
}

// YUV_to_BMT_STD_Area 只对指定区域进行 YUV → BMT 转换
func YUV_to_BMT_STD_Area(imageData []uint16, imageWidth, imageHeight, channels, x0, y0, x1, y1 uint32) {
	if channels != 3 {
		return
	}

	width := int(imageWidth)
	chans := int(channels)
	rowStride := width * chans

	for y := int(y0); y <= int(y1); y++ {
		for x := int(x0); x <= int(x1); x++ {
			idx := y*rowStride + x*chans

			Y := int32(imageData[idx+0])
			U := int32(imageData[idx+1]) - O_UV
			V := int32(imageData[idx+2]) - O_UV

			B := (12*Y + 3*U + 2*V + 6) / 12
			M := (3*Y - V + 1) / 3
			T := (12*Y - 3*U + 2*V + 6) / 12

			imageData[idx+0] = clampUint16(B)
			imageData[idx+1] = clampUint16(M)
			imageData[idx+2] = clampUint16(T)
		}
	}
}

// VMedianFilterArea 只对指定区域的 V 通道应用 3x3 中值滤波
func VMedianFilterArea(imageData []uint16, imageWidth, imageHeight, channels, x0, y0, x1, y1 uint32) {
	if channels != 3 {
		return
	}

	debug("BEGIN V median filtering")

	width := int(imageWidth)
	height := int(imageHeight)
	chans := int(channels)
	rowStride := width * chans

	// 创建临时缓冲区存储 V 通道的结果(只需要活动区域)
	areaWidth := int(x1 - x0 + 1)
	areaHeight := int(y1 - y0 + 1)
	vChannel := make([]uint16, width*height)

	// 提取整个图像的 V 通道(因为边界需要访问周围像素)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			idx := y*rowStride + x*chans
			vChannel[y*width+x] = imageData[idx+2]
		}
	}

	// 只对活动区域应用 3x3 中值滤波
	vFiltered := make([]uint16, areaWidth*areaHeight)
	for y := int(y0); y <= int(y1); y++ {
		for x := int(x0); x <= int(x1); x++ {
			areaIdx := (y-int(y0))*areaWidth + (x - int(x0))
			vFiltered[areaIdx] = medianFilter3x3(vChannel, x, y, width, height)
		}
	}

	// 将滤波后的 V 通道写回图像(只写活动区域)
	for y := int(y0); y <= int(y1); y++ {
		for x := int(x0); x <= int(x1); x++ {
			idx := y*rowStride + x*chans
			areaIdx := (y-int(y0))*areaWidth + (x - int(x0))
			imageData[idx+2] = vFiltered[areaIdx]
		}
	}

	debug("END V median filtering")
}

// medianFilter3x3 计算 3x3 窗口的中值
func medianFilter3x3(data []uint16, x, y, width, height int) uint16 {
	var values []uint16

	// 收集 3x3 窗口内的所有值
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			nx := x + dx
			ny := y + dy

			// 边界处理: 使用 BORDER_REPLICATE (复制边缘值)
			if nx < 0 {
				nx = 0
			}
			if nx >= width {
				nx = width - 1
			}
			if ny < 0 {
				ny = 0
			}
			if ny >= height {
				ny = height - 1
			}

			values = append(values, data[ny*width+nx])
		}
	}

	// 排序并返回中值
	sort.Slice(values, func(i, j int) bool {
		return values[i] < values[j]
	})

	return values[len(values)/2]
}
