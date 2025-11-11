package output

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"unsafe"

	"github.com/weaming/x3f-go/matrix"
	"github.com/weaming/x3f-go/x3f"
)

// DNG 特定标签常量 (标准 TIFF 标签在 tiff.go 中定义)
const (
	TagNewSubfileType      = 254
	TagOrientation         = 274
	TagSubIFDs             = 330 // SubIFD 偏移量
	TagDNGVersion          = 50706
	TagDNGBackwardVersion  = 50707
	TagUniqueCameraModel   = 50708
	TagColorMatrix1        = 50721
	TagColorMatrix2        = 50722
	TagCameraCalibration1  = 50723
	TagCameraCalibration2  = 50724
	TagAsShotNeutral       = 50728
	TagBaselineExposure    = 50730
	TagBaselineNoise       = 50731
	TagBaselineSharpness   = 50732
	TagLinearResponseLimit = 50734
	TagCameraSerialNumber  = 50735
	TagChromaBlurRadius    = 50703
	TagBlackLevel          = 50714
	TagWhiteLevel          = 50717
	TagCalibrationIllum1   = 50778
	TagCalibrationIllum2   = 50779
	TagActiveArea          = 50829
	TagForwardMatrix1      = 50964
	TagForwardMatrix2      = 50965
	TagAsShotProfileName   = 50934
	TagProfileName         = 50936
	TagDefaultBlackRender  = 51110
	TagOpcodeList2         = 51009
)

// Photometric Interpretation 值
const (
	PhotometricRGB       = 2
	PhotometricLinearRaw = 34892
)

// DNG Opcode 相关常量
const (
	OpcodeGainMapID      = 9          // DNG Opcode ID for GainMap
	OpcodeGainMapVersion = 0x01030000 // DNG Opcode version 1.3.0.0
)

// DNG 光源类型
const (
	LightSourceUnknown       = 0
	LightSourceDaylight      = 1
	LightSourceFluorescent   = 2
	LightSourceTungsten      = 3
	LightSourceFlash         = 4
	LightSourceFineWeather   = 9
	LightSourceCloudyWeather = 10
	LightSourceShade         = 11
	LightSourceD65           = 21
	LightSourceD50           = 23
	LightSourceStandardA     = 17
	LightSourceStandardB     = 18
	LightSourceStandardC     = 19
)

// DNGOptions DNG 输出选项
type DNGOptions struct {
	CameraModel     string
	CameraSerial    string
	ColorMatrix     []float64 // 3x3 矩阵
	WhiteBalance    [3]float64
	BaselineExpose  float64
	LinearOutput    bool
	NoCrop          bool
	CompatibleWithC bool // 生成与 C 版本完全相同的输出（不裁剪，输出完整图像）
}

// floatToRational 使用连分数算法将浮点数转换为有理数
func floatToRational(value float64, maxDenom int64) (num int64, denom int64) {
	if value == 0 {
		return 0, 1
	}

	sign := int64(1)
	if value < 0 {
		sign = -1
		value = -value
	}

	z := value
	n0, d0 := int64(0), int64(1)
	n1, d1 := int64(1), int64(0)

	for i := 0; i < 50; i++ {
		a := int64(z)
		n2 := n1*a + n0
		d2 := d1*a + d0

		if d2 > maxDenom {
			break
		}

		n0, d0 = n1, d1
		n1, d1 = n2, d2

		if z == float64(a) {
			break
		}
		z = 1.0 / (z - float64(a))
	}

	return sign * n1, d1
}

// buildOpcodeList2 构建 Opcode List 2 数据（Spatial Gain Maps）
func buildOpcodeList2(spatialGains []x3f.SpatialGainCorr, activeArea []uint32, imageRows, imageCols uint32) []byte {
	if len(spatialGains) == 0 || len(activeArea) != 4 {
		return nil
	}

	// 计算变换参数（Spatial Gain 相对于整个图像，但 OpcodeList2 应用于裁剪后的 ActiveArea）
	originV := -float64(activeArea[0]) / float64(activeArea[2]-activeArea[0])
	originH := -float64(activeArea[1]) / float64(activeArea[3]-activeArea[1])
	scaleV := float64(imageRows) / float64(activeArea[2]-activeArea[0])
	scaleH := float64(imageCols) / float64(activeArea[3]-activeArea[1])

	// 计算总大小
	totalSize := 4 // OpcodeList header (count)
	for _, sg := range spatialGains {
		opcodeSize := 16 + // Opcode header (id, ver, flags, parsize)
			16 + // Top, Left, Bottom, Right (4 × uint32)
			12 + // Plane, Planes, RowPitch (3 × uint32)
			4 + // ColPitch (uint32)
			8 + // MapPointsV, MapPointsH (2 × uint32)
			16 + // MapSpacingV, MapSpacingH (2 × float64)
			16 + // MapOriginV, MapOriginH (2 × float64)
			4 + // MapPlanes (uint32)
			len(sg.Gain)*4 // Gain data (float32)
		totalSize += opcodeSize
	}

	// 分配缓冲区
	buf := make([]byte, totalSize)
	offset := 0

	// 写入 OpcodeList header
	binary.BigEndian.PutUint32(buf[offset:], uint32(len(spatialGains)))
	offset += 4

	// 写入每个 GainMap opcode
	for planeIdx, sg := range spatialGains {
		opcodeParamSize := uint32(76 + len(sg.Gain)*4) // 参数大小（不包括 opcode header）

		// Opcode header
		binary.BigEndian.PutUint32(buf[offset:], OpcodeGainMapID)        // ID = 9
		binary.BigEndian.PutUint32(buf[offset+4:], OpcodeGainMapVersion) // Version = 1.3.0.0
		binary.BigEndian.PutUint32(buf[offset+8:], 0)                    // Flags = 0
		binary.BigEndian.PutUint32(buf[offset+12:], opcodeParamSize)     // ParamSize
		offset += 16

		// GainMap 参数
		binary.BigEndian.PutUint32(buf[offset:], uint32(sg.RowOff))              // Top
		binary.BigEndian.PutUint32(buf[offset+4:], uint32(sg.ColOff))            // Left
		binary.BigEndian.PutUint32(buf[offset+8:], activeArea[2]-activeArea[0])  // Bottom (active height)
		binary.BigEndian.PutUint32(buf[offset+12:], activeArea[3]-activeArea[1]) // Right (active width)
		offset += 16

		binary.BigEndian.PutUint32(buf[offset:], uint32(planeIdx))      // Plane (0=R, 1=G, 2=B)
		binary.BigEndian.PutUint32(buf[offset+4:], uint32(sg.Channels)) // Planes
		binary.BigEndian.PutUint32(buf[offset+8:], 1)                   // RowPitch = 1
		binary.BigEndian.PutUint32(buf[offset+12:], 1)                  // ColPitch = 1
		offset += 16

		binary.BigEndian.PutUint32(buf[offset:], uint32(sg.Rows))   // MapPointsV
		binary.BigEndian.PutUint32(buf[offset+4:], uint32(sg.Cols)) // MapPointsH
		offset += 8

		// MapSpacing (使用 float64, Big Endian)
		// MapSpacing 表示每个 map 点之间的间距，所以需要除以 (points-1)
		mapSpacingV := scaleV / float64(sg.Rows-1)
		mapSpacingH := scaleH / float64(sg.Cols-1)
		binary.BigEndian.PutUint64(buf[offset:], floatToBits64(mapSpacingV))
		binary.BigEndian.PutUint64(buf[offset+8:], floatToBits64(mapSpacingH))
		offset += 16

		// MapOrigin
		binary.BigEndian.PutUint64(buf[offset:], floatToBits64(originV))
		binary.BigEndian.PutUint64(buf[offset+8:], floatToBits64(originH))
		offset += 16

		binary.BigEndian.PutUint32(buf[offset:], uint32(sg.Channels)) // MapPlanes
		offset += 4

		// Gain data (float32, Big Endian)
		for _, g := range sg.Gain {
			binary.BigEndian.PutUint32(buf[offset:], floatToBits32(g))
			offset += 4
		}
	}

	return buf
}

// floatToBits32 将 float32 转换为 bits 表示
func floatToBits32(f float32) uint32 {
	return *(*uint32)(unsafe.Pointer(&f))
}

// floatToBits64 将 float64 转换为 bits 表示
func floatToBits64(f float64) uint64 {
	return *(*uint64)(unsafe.Pointer(&f))
}

// writeRational 写入有理数（使用连分数算法提高精度）
func writeRational(file *os.File, value float64, signed bool) {
	num, denom := floatToRational(value, 1000000000)

	if signed {
		binary.Write(file, binary.LittleEndian, int32(num))
	} else {
		binary.Write(file, binary.LittleEndian, uint32(num))
	}
	binary.Write(file, binary.LittleEndian, uint32(denom))
}

// ExportRawDNG 导出未经色彩处理的线性 RAW DNG
func ExportRawDNG(x3fFile *x3f.File, imageSection *x3f.ImageSection, filename string, opts DNGOptions) error {
	// 检查是否为 Quattro 格式 (版本 >= 4.0)
	if x3fFile.Header.Version >= 0x00040000 {
		return fmt.Errorf("Quattro 格式目前不支持 DNG 导出\n" +
			"建议使用 C 版本工具: ./bin/osx-arm64/x3f_extract -dng <文件>")
	}

	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	// 确定输出尺寸
	decodedWidth := imageSection.Columns
	decodedHeight := imageSection.Rows
	if imageSection.DecodedColumns > 0 {
		decodedWidth = imageSection.DecodedColumns
	}
	if imageSection.DecodedRows > 0 {
		decodedHeight = imageSection.DecodedRows
	}

	var targetWidth, targetHeight uint32
	var cropX, cropY int32
	var activeAreaTop, activeAreaLeft, activeAreaBottom, activeAreaRight uint32

	// 根据选项决定是否裁剪
	if opts.CompatibleWithC {
		// C 兼容模式：输出完整图像，在 Active Area 中指定裁剪区域
		targetWidth = decodedWidth
		targetHeight = decodedHeight
		cropX = 0
		cropY = 0

		// 获取 Active Area（用于 DNG 标签）
		x0, y0, x1, y1, ok := x3fFile.GetActiveImageArea()
		if ok {
			// DNG Active Area 格式: [top, left, bottom, right]
			// X3F CAMF 格式: [x0, y0, x1, y1] (inclusive)
			activeAreaTop = y0
			activeAreaLeft = x0
			activeAreaBottom = y1 + 1
			activeAreaRight = x1 + 1
		} else {
			// 没有 Active Area 信息，使用完整图像
			activeAreaTop = 0
			activeAreaLeft = 0
			activeAreaBottom = targetHeight
			activeAreaRight = targetWidth
		}
	} else if opts.NoCrop {
		// 不裁剪模式：输出完整解码数据
		targetWidth = decodedWidth
		targetHeight = decodedHeight
		cropX = 0
		cropY = 0
		activeAreaTop = 0
		activeAreaLeft = 0
		activeAreaBottom = targetHeight
		activeAreaRight = targetWidth
	} else {
		// 默认裁剪模式：裁剪到 Active Area
		x0, y0, x1, y1, ok := x3fFile.GetActiveImageArea()
		if ok {
			cropX = int32(x0)
			cropY = int32(y0)
			targetWidth = x1 - x0 + 1
			targetHeight = y1 - y0 + 1
		} else {
			// 使用文件头尺寸
			targetWidth = x3fFile.Header.Columns
			targetHeight = x3fFile.Header.Rows
			if targetWidth == 0 || targetHeight == 0 {
				targetWidth = decodedWidth
				targetHeight = decodedHeight
			}
			// 居中裁剪
			cropX = int32((decodedWidth - targetWidth) / 2)
			cropY = int32((decodedHeight - targetHeight) / 2)
		}
		// 裁剪模式下，Active Area 就是整个输出图像
		activeAreaTop = 0
		activeAreaLeft = 0
		activeAreaBottom = targetHeight
		activeAreaRight = targetWidth
	}

	// 检查 DecodedData 长度
	expectedLen := int(decodedWidth * decodedHeight * 3)
	actualLen := len(imageSection.DecodedData)
	if actualLen != expectedLen {
		return fmt.Errorf("DecodedData 长度不匹配: 期望 %d (width=%d, height=%d), 实际 %d",
			expectedLen, decodedWidth, decodedHeight, actualLen)
	}

	// 准备图像数据
	imageData := make([]byte, targetWidth*targetHeight*3*2) // 16-bit RGB
	if cropX == 0 && cropY == 0 && targetWidth == decodedWidth && targetHeight == decodedHeight {
		// 无裁剪：直接复制完整数据
		for y := uint32(0); y < targetHeight; y++ {
			for x := uint32(0); x < targetWidth; x++ {
				srcIdx := (int(y)*int(decodedWidth) + int(x)) * 3
				outIdx := (int(y)*int(targetWidth) + int(x)) * 6

				binary.LittleEndian.PutUint16(imageData[outIdx:], imageSection.DecodedData[srcIdx])
				binary.LittleEndian.PutUint16(imageData[outIdx+2:], imageSection.DecodedData[srcIdx+1])
				binary.LittleEndian.PutUint16(imageData[outIdx+4:], imageSection.DecodedData[srcIdx+2])
			}
		}
	} else {
		// 裁剪：从源图像中提取裁剪区域
		for outY := uint32(0); outY < targetHeight; outY++ {
			for outX := uint32(0); outX < targetWidth; outX++ {
				srcX := int32(outX) + cropX
				srcY := int32(outY) + cropY
				srcIdx := int(srcY)*int(decodedWidth) + int(srcX)
				outIdx := (int(outY)*int(targetWidth) + int(outX)) * 6

				binary.LittleEndian.PutUint16(imageData[outIdx:], imageSection.DecodedData[srcIdx*3])
				binary.LittleEndian.PutUint16(imageData[outIdx+2:], imageSection.DecodedData[srcIdx*3+1])
				binary.LittleEndian.PutUint16(imageData[outIdx+4:], imageSection.DecodedData[srcIdx*3+2])
			}
		}
	}

	// TIFF/DNG 文件头
	file.Write([]byte{0x49, 0x49}) // Little Endian
	binary.Write(file, binary.LittleEndian, uint16(42))
	binary.Write(file, binary.LittleEndian, uint32(8)) // IFD0 偏移

	// 获取白平衡
	wb := x3fFile.GetWhiteBalance()

	// 获取 Spatial Gain 数据
	spatialGains := x3fFile.GetSpatialGain(wb)
	var opcodeList2Data []byte
	if len(spatialGains) > 0 {
		x0, y0, x1, y1, ok := x3fFile.GetActiveImageArea()
		if ok {
			opcodeList2Data = buildOpcodeList2(spatialGains, []uint32{
				y0, x0, y1 + 1, x1 + 1,
			}, targetHeight, targetWidth)
		}
	}

	// 使用 IFDWriter 自动管理偏移
	ifd0 := NewIFDWriter(file)

	// Software (保留,用于标识生成工具)
	software := "x3f-go " + Version
	ifd0.AddASCII(TagSoftware, software, 32)

	// Orientation
	ifd0.AddShort(TagOrientation, 1) // 1 = Horizontal (normal)

	// SubIFDs - 预留指针,稍后更新
	_ = ifd0.ReservePointer(TagSubIFDs)

	// DNG Version
	dngVersionValue := uint32(1) | (uint32(4) << 8)
	ifd0.AddByte(TagDNGVersion, dngVersionValue)

	// DNG Backward Version
	dngBackwardVersionValue := uint32(1) | (uint32(3) << 8)
	ifd0.AddByte(TagDNGBackwardVersion, dngBackwardVersionValue)

	// ColorMatrix1 (9个有理数) - XYZ to sRGB (固定标准矩阵)
	colorMatrix1 := x3f.GetColorMatrix1ForDNG()
	ifd0.AddRationalArrayFromFloats(TagColorMatrix1, colorMatrix1, true)

	// CameraCalibration1 (9个有理数) - 使用 D65 (Overcast) 白平衡
	// C 代码: #define WB_D65 "Overcast"
	wbD65 := "Overcast"
	gainD65, ok := x3fFile.GetWhiteBalanceGain(wbD65)
	if !ok {
		// 如果无法获取 Overcast，使用当前白平衡
		gainD65 = opts.WhiteBalance
	}
	cameraCalibration := x3f.GetCameraCalibration1ForDNG(gainD65)
	ifd0.AddRationalArrayFromFloats(TagCameraCalibration1, cameraCalibration, true)

	// AsShotNeutral (白平衡的倒数)
	asShotNeutral := make([]float64, 3)
	for i := 0; i < 3; i++ {
		if opts.WhiteBalance[i] > 0 {
			asShotNeutral[i] = 1.0 / opts.WhiteBalance[i]
		} else {
			asShotNeutral[i] = 1.0
		}
	}
	ifd0.AddRationalArrayFromFloats(TagAsShotNeutral, asShotNeutral, false)

	// Baseline Exposure
	ifd0.AddSRational(TagBaselineExposure, 0, 1) // 0.0

	// As Shot Profile Name
	profileName := "Default"
	ifd0.AddASCII(TagAsShotProfileName, profileName, 32)

	// Profile Name
	ifd0.AddASCII(TagProfileName, profileName, 32)

	// Forward Matrix 1 (Camera RGB to XYZ)
	// ForwardMatrix1 = D65_to_D50 × bmt_to_xyz
	forwardMatrix1, ok := x3fFile.GetForwardMatrix1ForDNG(wb)
	if !ok {
		return fmt.Errorf("无法获取 ForwardMatrix1: 白平衡 '%s' 的 ColorCorrections 数据读取失败", wb)
	}
	ifd0.AddRationalArrayFromFloats(TagForwardMatrix1, forwardMatrix1, true)

	// Default Black Render (1 = None, 不是 0)
	ifd0.AddLong(TagDefaultBlackRender, 1) // 1 = None

	// 写入 IFD0
	if _, err := ifd0.Write(); err != nil {
		return err
	}

	// 写入 SubIFD
	subIFDStartPos, _ := file.Seek(0, io.SeekCurrent)
	if _, err := writeSubIFD(file, x3fFile, imageData,
		targetWidth, targetHeight,
		activeAreaTop, activeAreaLeft, activeAreaBottom, activeAreaRight,
		opts.WhiteBalance, opcodeList2Data); err != nil {
		return err
	}

	// 回写 SubIFD 偏移到 IFD0 的 SubIFDs 标签
	// 需要重新定位到 IFD0 entry 并更新
	file.Seek(8, io.SeekStart) // IFD0 开始位置

	// 读取 entry 数量
	var numEntries uint16
	binary.Read(file, binary.LittleEndian, &numEntries)

	// 遍历找到 SubIFDs tag (330)
	for i := uint16(0); i < numEntries; i++ {
		var tag, typ uint16
		var count, value uint32
		binary.Read(file, binary.LittleEndian, &tag)
		binary.Read(file, binary.LittleEndian, &typ)
		binary.Read(file, binary.LittleEndian, &count)

		if tag == TagSubIFDs {
			// 找到了,更新 value 字段
			binary.Write(file, binary.LittleEndian, uint32(subIFDStartPos))
			break
		} else {
			// 跳过 value 字段
			binary.Read(file, binary.LittleEndian, &value)
		}
	}

	return nil
}

// invertMatrix3x3 计算 3x3 矩阵的逆矩阵
func invertMatrix3x3(m []float64) []float64 {
	if len(m) != 9 {
		// 返回单位矩阵
		return []float64{1, 0, 0, 0, 1, 0, 0, 0, 1}
	}

	// 转换为 matrix 包的类型
	var mat matrix.Matrix3x3
	copy(mat[:], m)

	// 使用 matrix 包的 Inverse3x3 函数
	invMat := matrix.Inverse3x3(mat)

	// 转换回 []float64
	result := make([]float64, 9)
	copy(result, invMat[:])
	return result
}

// multiplyMatrix3x3 计算两个 3x3 矩阵相乘
func multiplyMatrix3x3(a, b []float64) []float64 {
	if len(a) != 9 || len(b) != 9 {
		// 返回单位矩阵
		return []float64{1, 0, 0, 0, 1, 0, 0, 0, 1}
	}

	// 转换为 matrix 包的类型
	var matA, matB matrix.Matrix3x3
	copy(matA[:], a)
	copy(matB[:], b)

	// 使用 matrix 包的 Multiply3x3 函数
	resultMat := matrix.Multiply3x3(matA, matB)

	// 转换回 []float64
	result := make([]float64, 9)
	copy(result, resultMat[:])
	return result
}
