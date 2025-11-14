package output

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"sort"
	"unsafe"

	"github.com/weaming/x3f-go/x3f"
)

var debug = x3f.Debug

// DNG 特定标签常量 (标准 TIFF 标签在 tiff.go 中定义)
const (
	TagImageDescription    = 270
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
	TagChromaBlurRadius    = 50737
	TagBlackLevel          = 50714
	TagWhiteLevel          = 50717
	TagCalibrationIllum1   = 50778
	TagCalibrationIllum2   = 50779
	TagActiveArea          = 50829
	TagForwardMatrix1      = 50964
	TagForwardMatrix2      = 50965
	TagAsShotProfileName   = 50934
	TagProfileName         = 50936
	TagExtraCameraProfiles = 50933
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

// Camera Profile 类型
type CameraProfile struct {
	Name          string
	GrayscaleMix  *x3f.Vector3 // nil 表示不是灰度模式
	UseSRGBMatrix bool         // true 使用 sRGB 标准矩阵，false 使用相机特定矩阵
}

// 预定义的 Camera Profiles（匹配 C 版本）
var DefaultCameraProfiles = []CameraProfile{
	{"Default", nil, false}, // 使用相机 CAMF ColorCorrections
	{"Grayscale", &x3f.Vector3{1.0 / 3.0, 1.0 / 3.0, 1.0 / 3.0}, false},
	{"Grayscale (red filter)", &x3f.Vector3{2.0, -1.0, 0.0}, false},
	{"Grayscale (blue filter)", &x3f.Vector3{0.0, -1.0, 2.0}, false},
	{"Unconverted", nil, true}, // 使用 sRGB 标准矩阵
	{"Linear sRGB", nil, true},
}

// DNGOptions DNG 输出选项
type DNGOptions struct {
	Camera x3f.CameraInfo
}

// writeCameraProfileIFD 为单个 camera profile 生成 Big Endian IFD 数据
// 返回完整的 TIFF IFD 结构（不包含 magic bytes）
func writeCameraProfileIFD(x3fFile *x3f.File, wb string, profile CameraProfile) ([]byte, error) {
	// 计算 ColorMatrix1 和 ForwardMatrix1
	colorMatrix1 := x3f.GetColorMatrix1()

	var forwardMatrix1 x3f.Matrix3x3

	if profile.GrayscaleMix != nil {
		// Grayscale profile: 使用 grayscale_mix 计算
		forwardMatrix1 = x3f.GetForwardMatrixGrayscale(*profile.GrayscaleMix)
	} else if profile.UseSRGBMatrix {
		// sRGB profile: 使用标准 sRGB 矩阵
		forwardMatrix1 = x3f.GetForwardMatrixWithSRGB()
	} else {
		// Default profile: 使用 CAMF ColorCorrections
		var ok bool
		forwardMatrix1, ok = x3fFile.GetForwardMatrix1(wb)
		if !ok {
			return nil, fmt.Errorf("无法获取 ForwardMatrix1: 白平衡 '%s' 的 ColorCorrections 数据读取失败", wb)
		}
	}

	// 创建一个内存缓冲区来写入 Big Endian TIFF 文件
	buf := &bytes.Buffer{}

	// 写入 TIFF magic (Big Endian) - 稍后会被 MMCR 替换
	binary.Write(buf, binary.BigEndian, uint16(0x4d4d)) // 'MM'
	binary.Write(buf, binary.BigEndian, uint16(42))     // TIFF magic
	binary.Write(buf, binary.BigEndian, uint32(8))      // IFD offset at 8

	// 准备 IFD entries
	type ifdEntry struct {
		tag           uint16
		typ           uint16
		count         uint32
		valueOrOffset uint32
	}

	entries := []ifdEntry{}
	extraData := &bytes.Buffer{}
	// 我们有 5 个 tags: Compression, ColorMatrix1, ForwardMatrix1, ProfileName, DefaultBlackRender
	// 计算初始偏移：magic(4) + offset(4) + count(2) + entries(12*5) + next(4)
	extraDataOffset := uint32(8 + 2 + 12*5 + 4)

	// Helper: 添加 Matrix3x3
	addMatrix := func(tag uint16, matrix x3f.Matrix3x3, signed bool) {
		count := uint32(9)
		offset := extraDataOffset

		const maxDenom = 67108864 // 2^26

		for i := 0; i < 9; i++ {
			num, denom := floatToRational(matrix[i], maxDenom)
			if signed {
				binary.Write(extraData, binary.BigEndian, int32(num))
				binary.Write(extraData, binary.BigEndian, int32(denom))
			} else {
				binary.Write(extraData, binary.BigEndian, uint32(num))
				binary.Write(extraData, binary.BigEndian, uint32(denom))
			}
		}

		typ := uint16(5) // RATIONAL
		if signed {
			typ = 10 // SRATIONAL
		}
		entries = append(entries, ifdEntry{tag, typ, count, offset})
		extraDataOffset += count * 8
	}

	// Helper: 添加 ASCII 字符串
	addASCII := func(tag uint16, value string) {
		data := []byte(value)
		data = append(data, 0) // null terminator
		count := uint32(len(data))

		if count <= 4 {
			// 可以内联
			var valueBytes [4]byte
			copy(valueBytes[:], data)
			entries = append(entries, ifdEntry{tag, 2, count, binary.BigEndian.Uint32(valueBytes[:])})
		} else {
			offset := extraDataOffset
			extraData.Write(data)
			// Pad to even boundary
			if len(data)%2 != 0 {
				extraData.WriteByte(0)
			}
			entries = append(entries, ifdEntry{tag, 2, count, offset})
			extraDataOffset += uint32(len(data))
			if len(data)%2 != 0 {
				extraDataOffset++
			}
		}
	}

	// Helper: 添加 LONG
	addLong := func(tag uint16, value uint32) {
		entries = append(entries, ifdEntry{tag, 4, 1, value}) // LONG = 4
	}

	// 添加 tags (按照 tag ID 排序)
	addLong(TagCompression, 1) // Uncompressed
	addMatrix(TagColorMatrix1, colorMatrix1, true)
	addMatrix(TagForwardMatrix1, forwardMatrix1, true)
	addASCII(TagProfileName, profile.Name)
	addLong(TagDefaultBlackRender, 1) // None

	// 按 tag ID 排序
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].tag < entries[j].tag
	})

	// 写入 IFD
	binary.Write(buf, binary.BigEndian, uint16(len(entries)))
	for _, entry := range entries {
		binary.Write(buf, binary.BigEndian, entry.tag)
		binary.Write(buf, binary.BigEndian, entry.typ)
		binary.Write(buf, binary.BigEndian, entry.count)
		binary.Write(buf, binary.BigEndian, entry.valueOrOffset)
	}
	binary.Write(buf, binary.BigEndian, uint32(0)) // Next IFD offset = 0

	// 写入额外数据
	buf.Write(extraData.Bytes())

	return buf.Bytes(), nil
}

// writeExtraCameraProfiles 写入额外的 camera profiles 到文件
// 返回所有额外 profile 的偏移量数组
func writeExtraCameraProfiles(file *os.File, x3fFile *x3f.File, wb string, profiles []CameraProfile) ([]uint32, error) {
	if len(profiles) <= 1 {
		return nil, nil
	}

	offsets := make([]uint32, 0, len(profiles)-1)

	for i := 1; i < len(profiles); i++ {
		// 获取当前文件位置
		currentPos, err := file.Seek(0, io.SeekCurrent)
		if err != nil {
			return nil, err
		}

		// 2 字节对齐
		if currentPos%2 != 0 {
			file.Write([]byte{0})
			currentPos++
		}

		// 记录偏移量
		offsets = append(offsets, uint32(currentPos))

		// 写入 DCP 魔法字节 (Big Endian)
		if _, err := file.Write([]byte{'M', 'M', 'C', 'R'}); err != nil {
			return nil, err
		}

		// 生成 profile IFD 数据
		ifdData, err := writeCameraProfileIFD(x3fFile, wb, profiles[i])
		if err != nil {
			return nil, fmt.Errorf("无法生成 profile '%s' 的 IFD: %v", profiles[i].Name, err)
		}

		// 写入 IFD 数据（跳过前 4 个字节，因为我们已经写了 MMCR）
		if _, err := file.Write(ifdData[4:]); err != nil {
			return nil, err
		}
	}

	return offsets, nil
}

// 使用连分数算法将浮点数转换为有理数
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

// 构建 Opcode List 2 数据（Spatial Gain Maps）
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

// 将 float32 转换为 bits 表示
func floatToBits32(f float32) uint32 {
	return *(*uint32)(unsafe.Pointer(&f))
}

// 将 float64 转换为 bits 表示
func floatToBits64(f float64) uint64 {
	return *(*uint64)(unsafe.Pointer(&f))
}

// imageDimensions 图像尺寸信息
type imageDimensions struct {
	decodedWidth  uint32
	decodedHeight uint32
	targetWidth   uint32
	targetHeight  uint32
	cropX         int32
	cropY         int32
	activeArea    [4]uint32 // [top, left, bottom, right]
}

// 导出未经色彩处理的线性 RAW DNG
func ExportRawDNG(c *CommonData, x3fFile *x3f.File, filename string, cameraInfo x3f.CameraInfo, logger *x3f.Logger) error {
	file := createDNGFile(filename)
	defer file.Close()

	wb := cameraInfo.WhiteBalance
	opts := DNGOptions{
		Camera: cameraInfo,
	}

	imageLevels := stdLevels
	opcodeData := prepareSpatialGain(x3fFile, wb, c.Dims)
	previewData, previewW, previewH := generatePreviewImage(c.ImgData, c.Dims.targetWidth, c.Dims.targetHeight, 300)

	writeTIFFHeader(file)
	writeIFD0(file, x3fFile, wb, opts, c.Dims, previewW, previewH, imageLevels)
	previewOffset := writePreviewData(file, previewData)
	subIFDOffset := writeSubIFDData(file, x3fFile, c.ImgData, c.Dims, imageLevels, opcodeData)

	return writeAndUpdateProfiles(file, x3fFile, wb, previewOffset, subIFDOffset)
}

// applyIntermediateToSRGB 直接从 intermediate 转换为 linear sRGB（一步完成，避免中间量化）
// 输入: imageData 包含 intermediate 数据 (uint16)
// 输出: 就地修改为 linear sRGB 数据 (uint16, 范围 0-65535)
func applyIntermediateToSRGB(imageData []byte, dims imageDimensions, x3fFile *x3f.File, wb string, preproc *x3f.PreprocessedData) {
	// 1. 获取 raw_to_xyz 矩阵（包含白平衡）
	rawToXYZ, ok := x3fFile.GetRawToXYZ(wb)
	if !ok {
		panic(fmt.Errorf("无法获取 raw_to_xyz 矩阵"))
	}

	// 2. 获取 xyz_to_srgb 矩阵
	xyzToSRGB := x3f.GetColorMatrix1()

	// 3. 组合矩阵: intermediate → XYZ → sRGB（一步完成）
	combinedMat := xyzToSRGB.Multiply(rawToXYZ)

	// 4. 从 preprocessed 获取 intermediate 参数
	intermediateBias := preproc.IntermediateBias
	maxIntermediate := preproc.MaxIntermediate

	// 5. 对每个像素应用转换
	maxOut := 65535.0
	pixelCount := dims.targetWidth * dims.targetHeight

	for i := uint32(0); i < pixelCount; i++ {
		offset := i * 6 // 16-bit RGB, 3 channels

		// 读取 intermediate 值
		r := float64(binary.LittleEndian.Uint16(imageData[offset:]))
		g := float64(binary.LittleEndian.Uint16(imageData[offset+2:]))
		b := float64(binary.LittleEndian.Uint16(imageData[offset+4:]))

		// 归一化到 [0, 1]（从 intermediate）
		input := x3f.Vector3{
			(r - intermediateBias) / (float64(maxIntermediate[0]) - intermediateBias),
			(g - intermediateBias) / (float64(maxIntermediate[1]) - intermediateBias),
			(b - intermediateBias) / (float64(maxIntermediate[2]) - intermediateBias),
		}

		// 应用组合矩阵（intermediate → sRGB，一步完成）
		output := combinedMat.Apply(input)

		// 转换回 16-bit，裁剪到 [0, 65535]
		for c := 0; c < 3; c++ {
			val := output[c] * maxOut
			if val < 0 {
				val = 0
			} else if val > maxOut {
				val = maxOut
			}
			binary.LittleEndian.PutUint16(imageData[offset+uint32(c)*2:], uint16(val))
		}
	}
}

// 写入 TIFF/DNG 文件头
func writeTIFFHeader(file *os.File) {
	file.Write([]byte{0x49, 0x49}) // Little Endian
	binary.Write(file, binary.LittleEndian, uint16(42))
	binary.Write(file, binary.LittleEndian, uint32(8)) // IFD0 偏移
}

// 创建 DNG 输出文件
func createDNGFile(filename string) *os.File {
	file, err := os.Create(filename)
	if err != nil {
		panic(err)
	}
	return file
}

// 计算图像尺寸和裁剪参数
func calculateDimensions(imageSection *x3f.ImageSection, x3fFile *x3f.File, preProc *x3f.PreprocessedData, crop bool) imageDimensions {
	var dims imageDimensions
	if preProc != nil {
		// 使用 preprocessed 数据的尺寸（已经 expand）
		dims = imageDimensions{
			decodedWidth:  preProc.Width,
			decodedHeight: preProc.Height,
		}
	} else {
		// 使用原始解码尺寸
		dims = getDecodedDimensions(imageSection)
	}
	applyDimensionOptions(&dims, x3fFile, crop)
	return dims
}

// 获取解码后的图像尺寸
func getDecodedDimensions(imageSection *x3f.ImageSection) imageDimensions {
	dims := imageDimensions{
		decodedWidth:  imageSection.Columns,
		decodedHeight: imageSection.Rows,
	}

	if imageSection.DecodedColumns > 0 {
		dims.decodedWidth = imageSection.DecodedColumns
	}
	if imageSection.DecodedRows > 0 {
		dims.decodedHeight = imageSection.DecodedRows
	}

	return dims
}

// 根据选项应用尺寸和裁剪设置
func applyDimensionOptions(dims *imageDimensions, x3fFile *x3f.File, crop bool) {
	if crop {
		applyCropMode(dims, x3fFile)
	} else {
		applyNoCropMode(dims)
	}
}

// 不裁剪模式：输出完整解码数据
func applyNoCropMode(dims *imageDimensions) {
	dims.targetWidth = dims.decodedWidth
	dims.targetHeight = dims.decodedHeight
	dims.cropX = 0
	dims.cropY = 0
	dims.activeArea = [4]uint32{0, 0, dims.targetHeight, dims.targetWidth}
}

// 默认裁剪模式：物理裁剪到 Active Area
func applyCropMode(dims *imageDimensions, x3fFile *x3f.File) {
	// 使用 GetCAMFRectScaled 以支持 Quattro 的缩放坐标
	x0, y0, x1, y1, ok := x3fFile.GetCAMFRectScaled("ActiveImageArea", dims.decodedWidth, dims.decodedHeight, true)
	if ok {
		// 物理裁剪到 Active Area
		dims.cropX = int32(x0)
		dims.cropY = int32(y0)
		dims.targetWidth = x1 - x0 + 1
		dims.targetHeight = y1 - y0 + 1
		// 裁剪后的 Active Area 就是完整图像
		dims.activeArea = [4]uint32{0, 0, dims.targetHeight, dims.targetWidth}
	} else {
		applyDefaultCrop(dims, x3fFile)
	}
}

// 使用文件头尺寸进行居中裁剪
func applyDefaultCrop(dims *imageDimensions, x3fFile *x3f.File) {
	dims.targetWidth = x3fFile.Header.Columns
	dims.targetHeight = x3fFile.Header.Rows
	if dims.targetWidth == 0 || dims.targetHeight == 0 {
		dims.targetWidth = dims.decodedWidth
		dims.targetHeight = dims.decodedHeight
	}
	dims.cropX = int32((dims.decodedWidth - dims.targetWidth) / 2)
	dims.cropY = int32((dims.decodedHeight - dims.targetHeight) / 2)
}

// 准备preprocessed 数据（已经 expand）
func preparePreprocessedImageData(preprocessedData []uint16, dims imageDimensions) []byte {
	// 验证 preprocessed 数据长度
	expectedLen := int(dims.decodedWidth * dims.decodedHeight * 3)
	actualLen := len(preprocessedData)
	if actualLen != expectedLen {
		panic(fmt.Errorf("PreprocessedData 长度不匹配: 期望 %d (width=%d, height=%d), 实际 %d",
			expectedLen, dims.decodedWidth, dims.decodedHeight, actualLen))
	}

	// 分配输出数据
	imageData := make([]byte, dims.targetWidth*dims.targetHeight*3*2)

	// 复制数据（处理裁剪）
	if dims.cropX == 0 && dims.cropY == 0 && dims.targetWidth == dims.decodedWidth && dims.targetHeight == dims.decodedHeight {
		// 完整复制（无裁剪）
		for i, val := range preprocessedData {
			binary.LittleEndian.PutUint16(imageData[i*2:], val)
		}
	} else {
		// 裁剪复制
		for outY := uint32(0); outY < dims.targetHeight; outY++ {
			for outX := uint32(0); outX < dims.targetWidth; outX++ {
				srcX := int32(outX) + dims.cropX
				srcY := int32(outY) + dims.cropY
				srcIdx := int(srcY)*int(dims.decodedWidth)*3 + int(srcX)*3
				dstIdx := int(outY)*int(dims.targetWidth)*3 + int(outX)*3

				for c := 0; c < 3; c++ {
					val := preprocessedData[srcIdx+c]
					binary.LittleEndian.PutUint16(imageData[(dstIdx+c)*2:], val)
				}
			}
		}
	}

	return imageData
}

// 准备 Spatial Gain 数据
func prepareSpatialGain(x3fFile *x3f.File, wb string, dims imageDimensions) []byte {
	spatialGains := x3fFile.GetSpatialGain(wb)
	if len(spatialGains) == 0 {
		return nil
	}

	x0, y0, x1, y1, ok := x3fFile.GetActiveImageArea()
	if !ok {
		return nil
	}

	return buildOpcodeList2(spatialGains, []uint32{y0, x0, y1 + 1, x1 + 1}, dims.targetHeight, dims.targetWidth)
}

// 写入 IFD0 标签
func writeIFD0(file *os.File, x3fFile *x3f.File, wb string, opts DNGOptions, dims imageDimensions, previewW, previewH uint32, imageLevels x3f.ImageLevels) {
	ifd0 := NewIFDWriter(file)

	addPreviewTags(ifd0, previewW, previewH)
	addDNGVersionTags(ifd0)
	addColorMatrixTags(ifd0, x3fFile, wb, opts, imageLevels)
	addProfileTags(ifd0, x3fFile, wb, opts)

	if _, err := ifd0.Write(); err != nil {
		panic(err)
	}
}

// 添加预览图相关标签
func addPreviewTags(ifd0 *IFDWriter, previewW, previewH uint32) {
	ifd0.AddLong(TagNewSubfileType, 1)
	ifd0.AddLong(TagImageWidth, previewW)
	ifd0.AddLong(TagImageLength, previewH)
	ifd0.AddShortArray(TagBitsPerSample, []uint16{8, 8, 8})
	ifd0.AddShort(TagCompression, 1)
	ifd0.AddShort(TagPhotometricInterpret, PhotometricRGB)
	ifd0.ReservePointer(TagStripOffsets)
	ifd0.AddShort(TagOrientation, 1)
	ifd0.AddShort(TagSamplesPerPixel, 3)
	ifd0.AddLong(TagRowsPerStrip, previewH)
	ifd0.AddLong(TagStripByteCounts, previewW*previewH*3)
	ifd0.AddShort(TagPlanarConfiguration, 1)
	ifd0.AddASCII(TagSoftware, "x3f-go "+x3f.Version, 32)
	ifd0.ReservePointer(TagSubIFDs)
}

// 添加 DNG 版本标签
func addDNGVersionTags(ifd0 *IFDWriter) {
	ifd0.AddByte(TagDNGVersion, uint32(1)|(uint32(4)<<8))
	ifd0.AddByte(TagDNGBackwardVersion, uint32(1)|(uint32(3)<<8))
}

// 添加色彩矩阵标签
func addColorMatrixTags(ifd0 *IFDWriter, x3fFile *x3f.File, wb string, opts DNGOptions, imageLevels x3f.ImageLevels) {
	gainD65, ok := x3fFile.GetWhiteBalanceGain("Overcast")
	if !ok {
		gainD65 = opts.Camera.WBGain
	}

	// Linear sRGB 模式: 使用标准 XYZ to sRGB 矩阵
	xyzToSRGB := x3f.GetColorMatrix1()
	ifd0.AddRationalArrayFromMatrix(TagColorMatrix1, xyzToSRGB, true)

	// Camera Calibration: 使用白平衡校正
	cameraCalibration := x3f.GetCameraCalibration1(gainD65)
	ifd0.AddRationalArrayFromMatrix(TagCameraCalibration1, cameraCalibration, true)

	// AsShotNeutral: 白平衡已应用，使用中性值
	asShotNeutral := x3f.Vector3{1.0, 1.0, 1.0}
	ifd0.AddRationalArrayFromVector3(TagAsShotNeutral, asShotNeutral, false)

	ifd0.AddSRational(TagBaselineExposure, 0, 1)
}

// 添加 Profile 相关标签
func addProfileTags(ifd0 *IFDWriter, x3fFile *x3f.File, wb string, opts DNGOptions) {
	// Linear sRGB 模式: 使用 Linear sRGB profile
	ifd0.AddASCII(TagImageDescription, "Preprocessed linear sRGB with white balance applied. Camera Calibration matrix is for reference only.", 128)

	profileName := "Linear sRGB"
	ifd0.AddASCII(TagAsShotProfileName, profileName, 32)
	ifd0.AddASCII(TagProfileName, profileName, 32)

	// ForwardMatrix: sRGB to XYZ
	forwardMatrix := x3f.GetForwardMatrixWithSRGB()
	ifd0.AddRationalArrayFromMatrix(TagForwardMatrix1, forwardMatrix, true)

	ifd0.AddLong(TagDefaultBlackRender, 1)
}

// 写入预览图数据
func writePreviewData(file *os.File, previewData []byte) int64 {
	offset, _ := file.Seek(0, io.SeekCurrent)
	if _, err := file.Write(previewData); err != nil {
		panic(err)
	}
	return offset
}

// 写入 SubIFD 数据
func writeSubIFDData(file *os.File, x3fFile *x3f.File, imageData []byte, dims imageDimensions, imageLevels x3f.ImageLevels, opcodeData []byte) int64 {
	offset, err := writeSubIFD(file, x3fFile, imageData,
		dims.targetWidth, dims.targetHeight,
		dims.activeArea[0], dims.activeArea[1], dims.activeArea[2], dims.activeArea[3],
		imageLevels, opcodeData)
	if err != nil {
		panic(err)
	}
	return int64(offset)
}

// 写入额外 Profiles 并更新所有偏移量
func writeAndUpdateProfiles(file *os.File, x3fFile *x3f.File, wb string, previewOffset, subIFDOffset int64) error {
	var profileOffsets []uint32
	if len(DefaultCameraProfiles) > 1 {
		file.Seek(0, io.SeekEnd)
		offsets, err := writeExtraCameraProfiles(file, x3fFile, wb, DefaultCameraProfiles)
		if err != nil {
			return fmt.Errorf("写入额外 Camera Profiles 失败: %v", err)
		}
		profileOffsets = offsets
	}

	updateIFD0Offsets(file, previewOffset, subIFDOffset, profileOffsets)
	return nil
}

// 更新 IFD0 中的偏移量
func updateIFD0Offsets(file *os.File, previewOffset, subIFDOffset int64, profileOffsets []uint32) {
	file.Seek(8, io.SeekStart)

	var numEntries uint16
	binary.Read(file, binary.LittleEndian, &numEntries)

	for i := uint16(0); i < numEntries; i++ {
		var tag, typ uint16
		var count, value uint32

		binary.Read(file, binary.LittleEndian, &tag)
		binary.Read(file, binary.LittleEndian, &typ)
		binary.Read(file, binary.LittleEndian, &count)

		if tag == TagStripOffsets {
			binary.Write(file, binary.LittleEndian, uint32(previewOffset))
		} else if tag == TagSubIFDs {
			binary.Write(file, binary.LittleEndian, uint32(subIFDOffset))
		} else if tag == TagExtraCameraProfiles && len(profileOffsets) > 0 {
			updateProfileOffsets(file, profileOffsets)
		} else {
			binary.Read(file, binary.LittleEndian, &value)
		}
	}
}

// 更新 Profile 偏移量数组
func updateProfileOffsets(file *os.File, profileOffsets []uint32) {
	var offsetArrayPos uint32
	binary.Read(file, binary.LittleEndian, &offsetArrayPos)

	currentPos, _ := file.Seek(0, io.SeekCurrent)
	file.Seek(int64(offsetArrayPos), io.SeekStart)

	for _, offset := range profileOffsets {
		binary.Write(file, binary.LittleEndian, offset)
	}

	file.Seek(currentPos, io.SeekStart)
}
