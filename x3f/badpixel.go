package x3f

import "fmt"

// BadPixel 坏点信息
type BadPixel struct {
	Col int
	Row int
}

// 收集所有坏点位置
func CollectBadPixels(file *File, imageWidth, imageHeight uint32, colors int) []BadPixel {
	var badPixels []BadPixel
	badPixelMap := make(map[int]bool) // 用于去重
	stats := make(map[string]int)     // 统计各来源

	outOfBounds := make(map[string]int)
	duplicates := make(map[string]int)

	addBadPixel := func(col, row int, source string) {
		if col < 0 || col >= int(imageWidth) || row < 0 || row >= int(imageHeight) {
			outOfBounds[source]++
			return
		}

		key := row*int(imageWidth) + col
		if !badPixelMap[key] {
			badPixelMap[key] = true
			badPixels = append(badPixels, BadPixel{Col: col, Row: row})
			stats[source]++
		} else {
			duplicates[source]++
		}
	}

	if colors == 3 {
		// 1. BadPixels（需要减去 KeepImageArea 的偏移）
		keep, keepOk := file.GetCAMFMatrixUint32("KeepImageArea", 4, 0)
		if keepOk {
			if bp, bpOk := file.GetCAMFMatrixUint32("BadPixels", 0, 0); bpOk {
				for _, val := range bp {
					col := int((val&0x000fff00)>>8) - int(keep[0])
					row := int((val&0xfff00000)>>20) - int(keep[1])
					addBadPixel(col, row, "BadPixels")
				}
			}
		}

		// 2. BadPixelsF20（注意：行列数互换是固件 bug）
		if data, dims, ok := file.GetCAMFMatrix("BadPixelsF20"); ok && len(dims) == 2 && dims[0] == 3 {
			if matrix, ok := data.([]uint32); ok {
				rows := int(dims[1])
				for i := 0; i < rows; i++ {
					col := int(matrix[i*3+1])
					row := int(matrix[i*3+0])
					addBadPixel(col, row, "BadPixelsF20")
				}
			}
		}

		// 3. Jpeg_BadClusters（注意：行列数互换是固件 bug）
		if data, dims, ok := file.GetCAMFMatrix("Jpeg_BadClusters"); ok && len(dims) == 2 && dims[0] == 3 {
			if matrix, ok := data.([]uint32); ok {
				rows := int(dims[1])
				for i := 0; i < rows; i++ {
					col := int(matrix[i*3+1])
					row := int(matrix[i*3+0])
					addBadPixel(col, row, "Jpeg_BadClusters")
				}
			}
		}

		// 4. HighlightPixelsInfo（网格模式）
		if hpinfo, ok := file.GetCAMFMatrixUint32("HighlightPixelsInfo", 2, 2); ok {
			startCol := int(hpinfo[0])
			startRow := int(hpinfo[1])
			pitchCol := int(hpinfo[2])
			pitchRow := int(hpinfo[3])

			for row := startRow; row < int(imageHeight); row += pitchRow {
				for col := startCol; col < int(imageWidth); col += pitchCol {
					addBadPixel(col, row, "HighlightPixelsInfo")
				}
			}
		}
	}

	// 5. BadPixelsChromaF23 or BadPixelsLumaF23
	matrixName := "BadPixelsChromaF23"
	if colors == 1 {
		matrixName = "BadPixelsLumaF23"
	}

	if data, _, ok := file.GetCAMFMatrix(matrixName); ok {
		if matrix, ok := data.([]uint32); ok {
			totalElements := len(matrix)
			rowCount := 0
			pixelCount := 0
			zeroCount := 0

			currentRow := -1
			for i := 0; i < len(matrix); {
				if currentRow == -1 {
					currentRow = int(matrix[i])
					rowCount++
					i++
				} else if matrix[i] == 0 {
					currentRow = -1
					zeroCount++
					i++
				} else {
					col := int(matrix[i])
					addBadPixel(col, currentRow, matrixName)
					pixelCount++
					i++
				}
			}

			debug("%s 格式解析: 总元素=%d, 行标记=%d, 列数据=%d, 零分隔符=%d",
				matrixName, totalElements, rowCount, pixelCount, zeroCount)
		}
	}

	// 6. 自动对焦网格（sd Quattro 和 sd Quattro H）
	if cameraID, ok := file.GetCAMFUint32("CAMERAID"); ok {
		var grid *struct {
			ci, cf, cp, cs int // column: initial, final, pitch, size
			ri, rf, rp, rs int // row: initial, final, pitch, size
		}

		if cameraID == CameraIDSDQ { // X3F_CAMERAID_SDQ
			if colors == 1 {
				grid = &struct{ ci, cf, cp, cs, ri, rf, rp, rs int }{217, 5641, 16, 1, 464, 3312, 32, 2}
			} else {
				grid = &struct{ ci, cf, cp, cs, ri, rf, rp, rs int }{108, 2820, 8, 1, 232, 1656, 16, 1}
			}
		} else if cameraID == CameraIDSDQH { // X3F_CAMERAID_SDQH
			if colors == 1 {
				grid = &struct{ ci, cf, cp, cs, ri, rf, rp, rs int }{233, 6425, 16, 1, 592, 3888, 32, 2}
			} else {
				grid = &struct{ ci, cf, cp, cs, ri, rf, rp, rs int }{116, 2820, 8, 1, 296, 1944, 16, 1}
			}
		}

		if grid != nil {
			for row := grid.ri; row <= grid.rf; row += grid.rp {
				for col := grid.ci; col <= grid.cf; col += grid.cp {
					for r := 0; r < grid.rs; r++ {
						for c := 0; c < grid.cs; c++ {
							addBadPixel(col+c, row+r, "AFGrid")
						}
					}
				}
			}
		}
	}

	// 输出统计信息
	// DEBUG=1 时显示详细信息，否则不显示
	if len(badPixels) > 0 && debugEnabled {
		fmt.Printf("坏点统计 (总数:%d, 图像尺寸:%dx%d=%.2f%%):\n",
			len(badPixels), imageWidth, imageHeight,
			float64(len(badPixels))*100.0/float64(imageWidth*imageHeight))
		for source, count := range stats {
			extra := ""
			if dup, ok := duplicates[source]; ok && dup > 0 {
				extra += fmt.Sprintf(" (重复:%d)", dup)
			}
			if oob, ok := outOfBounds[source]; ok && oob > 0 {
				extra += fmt.Sprintf(" (越界:%d)", oob)
			}
			fmt.Printf("  %-20s: %d%s\n", source, count, extra)
		}
	}

	return badPixels
}

// InpaintBadPixelsWithOpenCV 使用 OpenCV inpaint 算法修复坏点
func InpaintBadPixelsWithOpenCV(imageData []uint16, imageWidth, imageHeight, channels uint32, badPixels []BadPixel, method InpaintMethod) {
	if len(badPixels) == 0 {
		return
	}

	width := int(imageWidth)
	height := int(imageHeight)
	chans := int(channels)

	// 创建坏点掩码（uint8，非零处表示坏点）
	mask := make([]uint8, width*height)
	for _, bp := range badPixels {
		if bp.Col >= 0 && bp.Col < width && bp.Row >= 0 && bp.Row < height {
			mask[bp.Row*width+bp.Col] = 255
		}
	}

	// 调用 OpenCV inpaint
	// radius=3: 修复半径，通常 3-5 像素足够
	rowStride := width * chans
	InpaintBadPixelsOpenCV(imageData, height, width, chans, rowStride, mask, width, 3, method)
}
