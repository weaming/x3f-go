package x3f

// BadPixel 坏点信息
type BadPixel struct {
	Col int
	Row int
}

// 收集所有坏点位置
func CollectBadPixels(file *File, imageWidth, imageHeight uint32, colors int) []BadPixel {
	var badPixels []BadPixel
	badPixelMap := make(map[int]bool) // 用于去重

	addBadPixel := func(col, row int) {
		if col >= 0 && col < int(imageWidth) && row >= 0 && row < int(imageHeight) {
			key := row*int(imageWidth) + col
			if !badPixelMap[key] {
				badPixelMap[key] = true
				badPixels = append(badPixels, BadPixel{Col: col, Row: row})
			}
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
					addBadPixel(col, row)
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
					addBadPixel(col, row)
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
					addBadPixel(col, row)
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
					addBadPixel(col, row)
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
			currentRow := -1
			for i := 0; i < len(matrix); {
				if currentRow == -1 {
					currentRow = int(matrix[i])
					i++
				} else if matrix[i] == 0 {
					currentRow = -1
					i++
				} else {
					col := int(matrix[i])
					addBadPixel(col, currentRow)
					i++
				}
			}
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
			debug("Create AF grid for removing bad pixels")
			for row := grid.ri; row <= grid.rf; row += grid.rp {
				for col := grid.ci; col <= grid.cf; col += grid.cp {
					for r := 0; r < grid.rs; r++ {
						for c := 0; c < grid.cs; c++ {
							addBadPixel(col+c, row+r)
						}
					}
				}
			}
		}
	}

	debug("Collected %d bad pixels", len(badPixels))
	return badPixels
}

// 插值修复坏点
func InterpolateBadPixels(imageData []uint16, imageWidth, imageHeight, channels uint32, badPixels []BadPixel) {
	if len(badPixels) == 0 {
		return
	}

	debug("There are bad pixels to fix")

	width := int(imageWidth)
	height := int(imageHeight)
	chans := int(channels)
	rowStride := width * chans

	// 创建坏点标记位图
	badPixelMap := make(map[int]bool)
	for _, bp := range badPixels {
		key := bp.Row*width + bp.Col
		badPixelMap[key] = true
	}

	isBadPixel := func(col, row int) bool {
		if col < 0 || col >= width || row < 0 || row >= height {
			return true // 越界视为坏点
		}
		key := row*width + col
		return badPixelMap[key]
	}

	fixCorner := false
	passNum := 0

	for len(badPixelMap) > 0 {
		var fixed []BadPixel
		statsAllFour := 0
		statsTwoLinear := 0
		statsTwoCorner := 0
		statsLeft := 0

		// 遍历所有剩余的坏点
		for key := range badPixelMap {
			row := key / width
			col := key % width

			pixelIdx := row*rowStride + col*chans

			// 检查四个邻居，保存像素起始索引
			var neighborIndices [4]int
			var neighborValid [4]bool
			neighborCount := 0

			if !isBadPixel(col-1, row) {
				neighborIndices[0] = row*rowStride + (col-1)*chans
				neighborValid[0] = true
				neighborCount++
			}
			if !isBadPixel(col+1, row) {
				neighborIndices[1] = row*rowStride + (col+1)*chans
				neighborValid[1] = true
				neighborCount++
			}
			if !isBadPixel(col, row-1) {
				neighborIndices[2] = (row-1)*rowStride + col*chans
				neighborValid[2] = true
				neighborCount++
			}
			if !isBadPixel(col, row+1) {
				neighborIndices[3] = (row+1)*rowStride + col*chans
				neighborValid[3] = true
				neighborCount++
			}

			// 决定插值策略
			canInterpolate := false
			var useNeighbors [4]bool
			validCount := 0

			if neighborValid[0] && neighborValid[1] && neighborValid[2] && neighborValid[3] {
				// 四个邻居都OK
				useNeighbors = neighborValid
				validCount = 4
				canInterpolate = true
				statsAllFour++
			} else if neighborValid[0] && neighborValid[1] {
				// 左右OK
				useNeighbors[0] = true
				useNeighbors[1] = true
				validCount = 2
				canInterpolate = true
				statsTwoLinear++
			} else if neighborValid[2] && neighborValid[3] {
				// 上下OK
				useNeighbors[2] = true
				useNeighbors[3] = true
				validCount = 2
				canInterpolate = true
				statsTwoLinear++
			} else if fixCorner && neighborCount == 2 {
				// 对角OK（仅在最后阶段）
				useNeighbors = neighborValid
				validCount = 2
				canInterpolate = true
				statsTwoCorner++
			} else {
				// 无法插值
				statsLeft++
			}

			if canInterpolate {
				// 执行插值
				for c := 0; c < chans; c++ {
					sum := uint32(0)
					for i := 0; i < 4; i++ {
						if useNeighbors[i] {
							sum += uint32(imageData[neighborIndices[i]+c])
						}
					}
					imageData[pixelIdx+c] = uint16((sum + uint32(validCount)/2) / uint32(validCount))
				}

				// 标记为已修复
				fixed = append(fixed, BadPixel{Col: col, Row: row})
			}
		}

		debug("Bad pixels pass %d: %d fixed (%d all_four, %d linear, %d corner), %d left",
			passNum,
			statsAllFour+statsTwoLinear+statsTwoCorner,
			statsAllFour,
			statsTwoLinear,
			statsTwoCorner,
			statsLeft)

		if len(fixed) == 0 {
			// 没有修复任何像素
			if !fixCorner {
				fixCorner = true // 下一轮接受对角插值
			} else {
				debug("Failed to interpolate %d bad pixels", statsLeft)
				break
			}
		}

		// 从坏点列表中移除已修复的
		for _, bp := range fixed {
			key := bp.Row*width + bp.Col
			delete(badPixelMap, key)
		}

		passNum++
	}
}
