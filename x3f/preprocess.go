package x3f

import "fmt"

// PreprocessedData 包含预处理后的数据和相关参数
type PreprocessedData struct {
	// 处理后的数据
	Data []uint16
	// 数据尺寸
	Width  uint32
	Height uint32
	// 是否是 Quattro expanded 数据
	IsExpanded bool
	// Intermediate levels（用于色彩转换）
	IntermediateBias float64
	MaxIntermediate  [3]uint32
	BlackLevel       BlackLevelInfo
}

// PreprocessOptions 预处理选项
type PreprocessOptions struct {
	WhiteBalance string
	DoExpand     bool // 是否执行 Quattro expand
	Verbose      bool
}

// PreprocessImage 对图像进行预处理，包括 Quattro expand（如果适用）
// 这是 DNG 和 PPM 输出的共同前置处理流程
func PreprocessImage(file *File, imageSection *ImageSection, opts PreprocessOptions, logger *Logger) (*PreprocessedData, error) {
	// ==================== 阶段1: 解码图像 ====================

	// 1.1 解码图像（如果还没解码）
	if imageSection.DecodedData == nil {
		logger.Step("1️⃣  解码 RAW", fmt.Sprintf("%dx%d", imageSection.Columns, imageSection.Rows))
		if err := imageSection.DecodeImage(); err != nil {
			return nil, fmt.Errorf("解码失败: %w", err)
		}
		logger.Done(fmt.Sprintf("%d 像素", len(imageSection.DecodedData)/3))
	}

	// ==================== 阶段2: 准备预处理参数 ====================

	// 2.1 获取白平衡
	wb := opts.WhiteBalance
	if wb == "" {
		wb = file.GetWhiteBalance()
	}

	// 2.2 计算黑电平
	logger.Step("2️⃣  计算黑电平")
	blackLevel, err := CalculateBlackLevel(file, imageSection)
	if err != nil {
		return nil, fmt.Errorf("计算黑电平失败: %w", err)
	}
	logger.Done(fmt.Sprintf("R:%.0f G:%.0f B:%.0f",
		blackLevel.Level[0], blackLevel.Level[1], blackLevel.Level[2]))

	// 2.3 获取 intermediate bias
	logger.Step("3️⃣  Intermediate 参数")
	intermediateBias, ok := GetIntermediateBias(file, wb, blackLevel)
	if !ok {
		return nil, fmt.Errorf("获取 intermediate bias 失败")
	}

	// 2.4 获取 max intermediate
	maxIntermediate, ok := GetMaxIntermediate(file, wb, intermediateBias)
	if !ok {
		return nil, fmt.Errorf("获取 max intermediate 失败")
	}
	logger.Done(fmt.Sprintf("Bias:%.0f Max R:%d G:%d B:%d",
		intermediateBias, maxIntermediate[0], maxIntermediate[1], maxIntermediate[2]))

	// ==================== 阶段3: 应用预处理转换 ====================

	// 3.1 应用预处理（黑电平校正、intermediate bias、scale 转换）
	logger.Step("4️⃣  应用预处理转换")
	if err := PreprocessData(file, imageSection, wb); err != nil {
		return nil, fmt.Errorf("预处理失败: %w", err)
	}
	logger.Done("完成")

	// ==================== 阶段4: Quattro Expand（如需要） ====================

	// 4.1 检查是否需要 expand Quattro
	isQuattro := imageSection.QuattroTopData != nil && len(imageSection.QuattroTopData) > 0
	var expandedData []uint16
	var expandedWidth, expandedHeight int
	isExpanded := false

	if isQuattro && opts.DoExpand {
		logger.Step("5️⃣  Quattro Expand",
			fmt.Sprintf("%dx%d → %dx%d",
				imageSection.DecodedColumns, imageSection.DecodedRows,
				imageSection.DecodedColumns*2, imageSection.DecodedRows*2))

		// 对 Quattro top 层也应用预处理
		if err := PreprocessQuattroTop(file, imageSection, wb); err != nil {
			return nil, fmt.Errorf("top 层预处理失败: %w", err)
		}

		// 执行 Quattro expand
		expandedData = ExpandQuattro(
			imageSection.DecodedData,
			int(imageSection.DecodedColumns),
			int(imageSection.DecodedRows),
			imageSection.QuattroTopData,
			imageSection.QuattroTopCols,
			imageSection.QuattroTopRows,
		)
		expandedWidth = int(imageSection.DecodedColumns) * 2
		expandedHeight = int(imageSection.DecodedRows) * 2
		isExpanded = true

		logger.Done("完成")
	}

	// ==================== 阶段5: 确定输出数据 ====================

	// 5.1 确定输出数据
	var dataToUse []uint16
	var width, height uint32

	if isExpanded && expandedData != nil {
		// 使用 expanded 数据
		dataToUse = expandedData
		width = uint32(expandedWidth)
		height = uint32(expandedHeight)
	} else {
		// 使用原始数据
		dataToUse = imageSection.DecodedData
		width = imageSection.Columns
		height = imageSection.Rows
		if imageSection.DecodedColumns > 0 {
			width = imageSection.DecodedColumns
		}
		if imageSection.DecodedRows > 0 {
			height = imageSection.DecodedRows
		}
	}

	return &PreprocessedData{
		Data:             dataToUse,
		Width:            width,
		Height:           height,
		IsExpanded:       isExpanded,
		IntermediateBias: intermediateBias,
		MaxIntermediate:  maxIntermediate,
		BlackLevel:       blackLevel,
	}, nil
}
