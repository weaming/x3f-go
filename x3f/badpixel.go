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
					i += 2
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

// InpaintBadPixels 使用纯 Go 实现的等价 C 版本插值算法来修复坏点
func InpaintBadPixels(imageData []uint16, imageWidth, imageHeight, channels uint32, badPixels []BadPixel) {
	if len(badPixels) == 0 {
		return
	}

	width := int(imageWidth)
	height := int(imageHeight)
	chans := int(channels)
	rowStride := width * chans

	// 创建一个布尔掩码用于快速查找坏点，true 表示是坏点
	isBadPixel := make([]bool, width*height)
	for _, bp := range badPixels {
		if bp.Col >= 0 && bp.Col < width && bp.Row >= 0 && bp.Row < height {
			isBadPixel[bp.Row*width+bp.Col] = true
		}
	}

	// 待处理的坏点列表
	remainingBadPixels := badPixels
	fixCorner := false // 是否允许使用对角像素进行修复
	pass := 0

	for len(remainingBadPixels) > 0 {
		var stillBadPixels []BadPixel
		fixedInPass := 0

		// 遍历当前轮次中所有待处理的坏点
		for _, bp := range remainingBadPixels {
			c, r := bp.Col, bp.Row

			// 检查四个邻居的状态
			neighbors := [4][]uint16{}
			validNeighbors := 0
			// Left
			if c > 0 && !isBadPixel[r*width+(c-1)] {
				idx := r*rowStride + (c-1)*chans
				neighbors[0] = imageData[idx : idx+chans]
				validNeighbors++
			}
			// Right
			if c < width-1 && !isBadPixel[r*width+(c+1)] {
				idx := r*rowStride + (c+1)*chans
				neighbors[1] = imageData[idx : idx+chans]
				validNeighbors++
			}
			// Top
			if r > 0 && !isBadPixel[(r-1)*width+c] {
				idx := (r-1)*rowStride + c*chans
				neighbors[2] = imageData[idx : idx+chans]
				validNeighbors++
			}
			// Bottom
			if r < height-1 && !isBadPixel[(r+1)*width+c] {
				idx := (r+1)*rowStride + c*chans
				neighbors[3] = imageData[idx : idx+chans]
				validNeighbors++
			}

			canFix := false

			// 判断是否可以修复
			if neighbors[0] != nil && neighbors[1] != nil && neighbors[2] != nil && neighbors[3] != nil {
				// 四个邻居都可用
				canFix = true
			} else if neighbors[0] != nil && neighbors[1] != nil {
				// 左右邻居可用
				canFix = true
				neighbors[2], neighbors[3] = nil, nil // 只使用左右
			} else if neighbors[2] != nil && neighbors[3] != nil {
				// 上下邻居可用
				canFix = true
				neighbors[0], neighbors[1] = nil, nil // 只使用上下
			} else if fixCorner && validNeighbors >= 2 {
				// 如果是第二轮，允许使用任意两个邻居（包括对角）
				canFix = true
			}

			if canFix {
				// 执行插值
				outpIdx := r*rowStride + c*chans
				for color := 0; color < chans; color++ {
					sum := uint32(0)
					count := 0
					for i := 0; i < 4; i++ {
						if neighbors[i] != nil {
							sum += uint32(neighbors[i][color])
							count++
						}
					}
					// 使用 C 版本的带舍入的整数除法
					imageData[outpIdx+color] = uint16((sum + uint32(count)/2) / uint32(count))
				}
				// 标记为已修复
				isBadPixel[r*width+c] = false
				fixedInPass++
			} else {
				// 无法修复，留到下一轮
				stillBadPixels = append(stillBadPixels, bp)
			}
		}

		debug("坏点修复第 %d 轮: %d 个已修复, %d 个剩余", pass, fixedInPass, len(stillBadPixels))
		pass++

		if fixedInPass == 0 {
			if !fixCorner {
				// 如果第一轮没有修复任何像素，则在下一轮放宽条件
				fixCorner = true
			} else {
				// 如果放宽条件后仍然无法修复，则放弃以避免死循环
				if len(stillBadPixels) > 0 {
					fmt.Printf("无法修复 %d 个坏点\n", len(stillBadPixels))
				}
				break
			}
		}
		remainingBadPixels = stillBadPixels
	}
}
