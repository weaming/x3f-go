package x3f

import (
	"runtime"
	"sync"
)

// 双线性插值上采样
func BilinearUpscale(src []uint16, srcWidth, srcHeight, srcChannels, dstWidth, dstHeight int) []uint16 {
	dst := make([]uint16, dstWidth*dstHeight*srcChannels)

	scaleX := float64(srcWidth) / float64(dstWidth)
	scaleY := float64(srcHeight) / float64(dstHeight)

	for dstY := 0; dstY < dstHeight; dstY++ {
		for dstX := 0; dstX < dstWidth; dstX++ {
			// 计算源坐标
			srcXf := (float64(dstX) + 0.5) * scaleX
			srcYf := (float64(dstY) + 0.5) * scaleY

			// 获取周围 4 个像素的坐标
			x0 := int(srcXf)
			y0 := int(srcYf)
			x1 := x0 + 1
			y1 := y0 + 1

			// 边界检查
			if x0 < 0 {
				x0 = 0
			}
			if y0 < 0 {
				y0 = 0
			}
			if x1 >= srcWidth {
				x1 = srcWidth - 1
			}
			if y1 >= srcHeight {
				y1 = srcHeight - 1
			}

			// 计算权重
			wx1 := srcXf - float64(x0)
			wy1 := srcYf - float64(y0)
			wx0 := 1.0 - wx1
			wy0 := 1.0 - wy1

			// 对每个通道进行插值
			for c := 0; c < srcChannels; c++ {
				// 获取 4 个角的像素值
				p00 := src[(y0*srcWidth+x0)*srcChannels+c]
				p10 := src[(y0*srcWidth+x1)*srcChannels+c]
				p01 := src[(y1*srcWidth+x0)*srcChannels+c]
				p11 := src[(y1*srcWidth+x1)*srcChannels+c]

				// 双线性插值
				v0 := float64(p00)*wx0 + float64(p10)*wx1
				v1 := float64(p01)*wx0 + float64(p11)*wx1
				val := v0*wy0 + v1*wy1

				// 限制范围
				if val < 0 {
					val = 0
				}
				if val > 65535 {
					val = 65535
				}

				dst[(dstY*dstWidth+dstX)*srcChannels+c] = uint16(val)
			}
		}
	}

	return dst
}

// 双三次插值核函数（Catmull-Rom）
func cubicWeight(x float64) float64 {
	x = abs(x)
	if x <= 1.0 {
		return 1.5*x*x*x - 2.5*x*x + 1.0
	} else if x < 2.0 {
		return -0.5*x*x*x + 2.5*x*x - 4.0*x + 2.0
	}
	return 0.0
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// 双三次插值上采样（使用 Catmull-Rom 样条）
func BicubicUpscale(src []uint16, srcWidth, srcHeight, srcChannels, dstWidth, dstHeight int) []uint16 {
	dst := make([]uint16, dstWidth*dstHeight*srcChannels)

	scaleX := float64(srcWidth) / float64(dstWidth)
	scaleY := float64(srcHeight) / float64(dstHeight)

	// 使用并发处理
	numWorkers := runtime.NumCPU()
	if numWorkers > dstHeight {
		numWorkers = dstHeight
	}

	rowsPerWorker := dstHeight / numWorkers
	var wg sync.WaitGroup

	for workerID := 0; workerID < numWorkers; workerID++ {
		wg.Add(1)

		startRow := workerID * rowsPerWorker
		endRow := startRow + rowsPerWorker
		if workerID == numWorkers-1 {
			endRow = dstHeight
		}

		go func(startY, endY int) {
			defer wg.Done()

			for dstY := startY; dstY < endY; dstY++ {
				for dstX := 0; dstX < dstWidth; dstX++ {
					// 计算源坐标
					srcXf := (float64(dstX) + 0.5) * scaleX
					srcYf := (float64(dstY) + 0.5) * scaleY

					// 获取中心像素坐标
					x0 := int(srcXf)
					y0 := int(srcYf)

					// 计算小数部分
					fx := srcXf - float64(x0)
					fy := srcYf - float64(y0)

					// 对每个通道进行插值
					for c := 0; c < srcChannels; c++ {
						var sum float64
						var weightSum float64

						// 使用 4x4 的邻域进行双三次插值
						for j := -1; j <= 2; j++ {
							for i := -1; i <= 2; i++ {
								srcX := x0 + i
								srcY := y0 + j

								// 边界处理
								if srcX < 0 {
									srcX = 0
								}
								if srcX >= srcWidth {
									srcX = srcWidth - 1
								}
								if srcY < 0 {
									srcY = 0
								}
								if srcY >= srcHeight {
									srcY = srcHeight - 1
								}

								// 获取像素值
								pixelVal := float64(src[(srcY*srcWidth+srcX)*srcChannels+c])

								// 计算权重
								wx := cubicWeight(float64(i) - fx)
								wy := cubicWeight(float64(j) - fy)
								weight := wx * wy

								sum += pixelVal * weight
								weightSum += weight
							}
						}

						// 归一化
						var val float64
						if weightSum != 0 {
							val = sum / weightSum
						} else {
							val = sum
						}

						// 限制范围
						if val < 0 {
							val = 0
						}
						if val > 65535 {
							val = 65535
						}

						dst[(dstY*dstWidth+dstX)*srcChannels+c] = uint16(val)
					}
				}
			}
		}(startRow, endRow)
	}

	wg.Wait()

	return dst
}

// ===============================================
// Quattro 图像处理流程
// ===============================================
//
// Quattro 传感器有三层：Bottom (B)、Middle (M)、Top (T)
// - Bottom/Middle 层：低分辨率，包含色彩信息 (1:1:1 比例)
// - Top 层：高分辨率，仅亮度信息 (4倍像素数)
//
// Expand 流程将三层数据融合为一张高分辨率彩色图像：
// 1. 将 BMT 转换为 YUV (Y=亮度, UV=色度)
// 2. 上采样 YUV 到 2 倍尺寸（使用 bicubic 插值）
// 3. 用高分辨率的 Top 层替换 Y 通道
// 4. 转换回 BMT 色彩空间
//
// ===============================================

// ExpandQuattro 主函数：协调整个 Quattro 图像处理流程
// 输入：
//   - bottomMiddleData: 低分辨率的 bottom/middle 层 (width x height x 3)
//   - topData: 高分辨率的 top 层 (topWidth x topHeight x 1)
//
// 输出：
//   - 高分辨率彩色图像 (width*2 x height*2 x 3)
func ExpandQuattro(bottomMiddleData []uint16, width, height int,
	topData []uint16, topWidth, topHeight int) []uint16 {

	dstWidth := width * 2
	dstHeight := height * 2

	debug("ExpandQuattro: 开始处理 %dx%d -> %dx%d", width, height, dstWidth, dstHeight)

	// 步骤 1: BMT → YUV (Y=4T, 为后续替换做准备)
	yuvData := quattroStep1_ConvertBMTtoYUV(bottomMiddleData, width, height)

	// 步骤 2: 上采样色度通道（UV）到 2 倍尺寸
	upscaledYUV := quattroStep2_UpscaleToFullResolution(yuvData, width, height, dstWidth, dstHeight)

	// 步骤 3: 用高分辨率 Top 层替换 Y 通道
	quattroStep3_ReplaceYChannelWithTopLayer(upscaledYUV, dstWidth, dstHeight, topData, topWidth, topHeight)

	// 步骤 4: YUV → BMT (转回原始色彩空间)
	quattroStep4_ConvertYUVtoBMT(upscaledYUV, dstWidth, dstHeight)

	debug("ExpandQuattro: 完成，输出尺寸=%dx%d", dstWidth, dstHeight)

	return upscaledYUV
}

// quattroStep1_ConvertBMTtoYUV 步骤1：将 BMT 转换为 YUV 色彩空间
// 使用 Yis4T 转换：Y = 4*T (为后续 top 层融合预留空间)
func quattroStep1_ConvertBMTtoYUV(bmtData []uint16, width, height int) []uint16 {
	debug("Quattro 步骤1: BMT → YUV (Yis4T)")

	yuvData := make([]uint16, len(bmtData))
	copy(yuvData, bmtData)

	BMT_to_YUV_Yis4T(yuvData, uint32(width), uint32(height), 3)

	return yuvData
}

// quattroStep2_UpscaleToFullResolution 步骤2：上采样到完整分辨率
// 使用双三次插值将色度通道放大到 2 倍尺寸
func quattroStep2_UpscaleToFullResolution(yuvData []uint16, srcWidth, srcHeight, dstWidth, dstHeight int) []uint16 {
	debug("Quattro 步骤2: 上采样 %dx%d → %dx%d (Bicubic)", srcWidth, srcHeight, dstWidth, dstHeight)

	return BicubicUpscale(yuvData, srcWidth, srcHeight, 3, dstWidth, dstHeight)
}

// quattroStep3_ReplaceYChannelWithTopLayer 步骤3：用高分辨率 Top 层替换 Y 通道
// Top 层包含完整的高分辨率亮度信息，乘以 4 匹配 YUV 空间的范围
func quattroStep3_ReplaceYChannelWithTopLayer(yuvData []uint16, dstWidth, dstHeight int,
	topData []uint16, topWidth, topHeight int) {

	// C 版本使用左上角对齐裁剪（不是居中裁剪）
	// rect[0] = 0, rect[1] = 0, rect[2] = 2*image.columns-1, rect[3] = 2*image.rows-1
	// 这确保 Top 层和上采样后的 UV 层正确对齐
	cropX := 0
	cropY := 0

	debug("Quattro 步骤3: 用 Top 层替换 Y 通道 (top=%dx%d, dst=%dx%d, crop偏移=%d,%d)",
		topWidth, topHeight, dstWidth, dstHeight, cropX, cropY)

	// 逐像素替换 Y 通道（并发处理）
	numWorkers := runtime.NumCPU()
	if numWorkers > dstHeight {
		numWorkers = dstHeight
	}

	rowsPerWorker := dstHeight / numWorkers
	var wg sync.WaitGroup

	for workerID := 0; workerID < numWorkers; workerID++ {
		wg.Add(1)

		startRow := workerID * rowsPerWorker
		endRow := startRow + rowsPerWorker
		if workerID == numWorkers-1 {
			endRow = dstHeight
		}

		go func(startY, endY int) {
			defer wg.Done()

			for y := startY; y < endY; y++ {
				for x := 0; x < dstWidth; x++ {
					topX := x + cropX
					topY := y + cropY

					if topX >= 0 && topX < topWidth && topY >= 0 && topY < topHeight {
						topIdx := topY*topWidth + topX
						topVal := topData[topIdx]

						// Top 层值乘以 4（匹配 C 代码：qt *= 4）
						// 这是因为 YUV_Yis4T 中 Y=4T，需要保持一致的范围
						scaledVal := uint32(topVal) * 4
						if scaledVal > 65535 {
							scaledVal = 65535
						}

						// 替换 Y 通道（索引 0）
						dstIdx := (y*dstWidth + x) * 3
						yuvData[dstIdx] = uint16(scaledVal)
					}
				}
			}
		}(startRow, endRow)
	}

	wg.Wait()
}

// quattroStep4_ConvertYUVtoBMT 步骤4：将 YUV 转换回 BMT 色彩空间
// 使用 Yis4T 转换：所有通道除以 4，抵消步骤3的 ×4 操作
func quattroStep4_ConvertYUVtoBMT(yuvData []uint16, width, height int) {
	debug("Quattro 步骤4: YUV → BMT (Yis4T)")

	YUV_to_BMT_Yis4T(yuvData, uint32(width), uint32(height), 3)
}
