package processor

import (
	"fmt"
	"os"

	"github.com/weaming/x3f-go/colorspace"
	"github.com/weaming/x3f-go/x3f"
)

var debugEnabled = os.Getenv("DEBUG") != ""

func debug(format string, args ...interface{}) {
	if debugEnabled {
		fmt.Printf(format+"\n", args...)
	}
}

// ProcessOptions 图像处理选项
type ProcessOptions struct {
	WhiteBalance  string
	ColorSpace    colorspace.ColorSpace
	ApplyGamma    bool
	ToneMapping   bool
	LinearOutput  bool
	AutoExposure  bool
	ExposureValue float64
	NoCrop        bool
}

// ProcessedImage 处理后的图像
type ProcessedImage struct {
	Width    uint32
	Height   uint32
	Channels uint32
	Data     []float64 // RGB 浮点数据 [0, 1]
}

// ProcessRAW 处理 RAW 图像
func ProcessRAW(file *x3f.File, imageSection *x3f.ImageSection, opts ProcessOptions) (*ProcessedImage, error) {
	// 1. 解码图像
	if imageSection.DecodedData == nil {
		err := imageSection.DecodeImage()
		if err != nil {
			return nil, fmt.Errorf("解码图像失败: %w", err)
		}
	}

	// 2. 计算黑电平
	blackLevelInfo, err := CalculateBlackLevel(file, imageSection)
	if err != nil {
		// 使用默认黑电平
		blackLevelInfo = BlackLevelInfo{
			Level: [3]float64{0, 0, 0},
			Dev:   [3]float64{0, 0, 0},
		}
	}
	blackLevel := colorspace.Vector3{blackLevelInfo.Level[0], blackLevelInfo.Level[1], blackLevelInfo.Level[2]}

	// 3. 获取最大 RAW 值
	maxRaw, ok := file.GetMaxRAW()
	debug("ProcessRAW: GetMaxRAW returned ok=%v, maxRaw=%v", ok, maxRaw)
	if !ok {
		// TODO: 应该从 ImageDepth 计算,暂时使用 12-bit 的默认值
		maxRaw = [3]uint32{4095, 4095, 4095}
		debug("ProcessRAW: Using default maxRaw=%v", maxRaw)
	}

	// 4. 获取白平衡增益
	wb := opts.WhiteBalance
	if wb == "" {
		wb = file.GetWhiteBalance()
	}

	gain, ok := file.GetWhiteBalanceGain(wb)
	if !ok {
		gain = [3]float64{1.0, 1.0, 1.0}
	}

	// 5. 获取色彩矩阵 (RAW → XYZ)
	rawToXYZ, ok := file.GetColorMatrix(wb)
	if !ok {
		// 使用单位矩阵
		rawToXYZ = []float64{
			1, 0, 0,
			0, 1, 0,
			0, 0, 1,
		}
	}

	// 6. 获取 XYZ → RGB 转换矩阵
	xyzToRGB := colorspace.Identity3x3()
	if opts.ColorSpace != colorspace.ColorSpaceNone {
		xyzToRGB = colorspace.GetXYZToRGBMatrix(opts.ColorSpace)
	}

	// 7. 确定输出尺寸
	// 解码尺寸（实际解码出来的图像大小）
	decodedWidth := imageSection.Columns
	decodedHeight := imageSection.Rows
	if imageSection.DecodedColumns > 0 {
		decodedWidth = imageSection.DecodedColumns
	}
	if imageSection.DecodedRows > 0 {
		decodedHeight = imageSection.DecodedRows
	}
	debug("ProcessRAW: imageSection.Columns=%d, Rows=%d", imageSection.Columns, imageSection.Rows)
	debug("ProcessRAW: DecodedColumns=%d, DecodedRows=%d", imageSection.DecodedColumns, imageSection.DecodedRows)
	debug("ProcessRAW: decodedWidth=%d, decodedHeight=%d", decodedWidth, decodedHeight)

	// 目标输出尺寸和裁剪偏移
	var targetWidth, targetHeight uint32
	var cropX, cropY int32

	if opts.NoCrop {
		// 不裁剪，输出完整解码数据
		targetWidth = decodedWidth
		targetHeight = decodedHeight
		cropX = 0
		cropY = 0
		debug("ProcessRAW: NoCrop mode, using full decoded size")
	} else {
		// 使用 ActiveImageArea 裁剪
		x0, y0, x1, y1, ok := file.GetActiveImageArea()
		if ok {
			// ActiveImageArea 给出的是 [x0, y0, x1, y1]，其中 x1, y1 是 inclusive 的最大坐标
			cropX = int32(x0)
			cropY = int32(y0)
			targetWidth = x1 - x0 + 1
			targetHeight = y1 - y0 + 1
			debug("ProcessRAW: ActiveImageArea=[%d, %d, %d, %d]", x0, y0, x1, y1)
		} else {
			// 没有 ActiveImageArea，使用文件头尺寸
			targetWidth = file.Header.Columns
			targetHeight = file.Header.Rows
			if targetWidth == 0 || targetHeight == 0 {
				targetWidth = decodedWidth
				targetHeight = decodedHeight
			}
			// 居中裁剪
			cropX = int32((decodedWidth - targetWidth) / 2)
			cropY = int32((decodedHeight - targetHeight) / 2)
		}
	}
	debug("ProcessRAW: targetWidth=%d, targetHeight=%d", targetWidth, targetHeight)
	debug("ProcessRAW: cropX=%d, cropY=%d", cropX, cropY)

	totalPixels := int(targetWidth * targetHeight)

	processed := &ProcessedImage{
		Width:    targetWidth,
		Height:   targetHeight,
		Channels: 3,
		Data:     make([]float64, totalPixels*3),
	}

	rawToXYZMat := colorspace.Matrix3x3{}
	copy(rawToXYZMat[:], rawToXYZ)

	// 调试输出
	if len(imageSection.DecodedData) > 0 {
		debug("ProcessRAW: First RAW pixel (0,0) = (%d, %d, %d)",
			imageSection.DecodedData[0], imageSection.DecodedData[1], imageSection.DecodedData[2])
	}
	debug("ProcessRAW: blackLevel = (%.2f, %.2f, %.2f)", blackLevel[0], blackLevel[1], blackLevel[2])
	debug("ProcessRAW: maxRaw = (%d, %d, %d)", maxRaw[0], maxRaw[1], maxRaw[2])
	debug("ProcessRAW: gain = (%.4f, %.4f, %.4f)", gain[0], gain[1], gain[2])

	for outY := uint32(0); outY < targetHeight; outY++ {
		for outX := uint32(0); outX < targetWidth; outX++ {
			// 计算在解码图像中的坐标（应用裁剪偏移）
			srcX := int32(outX) + cropX
			srcY := int32(outY) + cropY

			// 读取 RAW 像素值
			srcIdx := int(srcY)*int(decodedWidth) + int(srcX)
			rawPixel := colorspace.Vector3{
				float64(imageSection.DecodedData[srcIdx*3]),
				float64(imageSection.DecodedData[srcIdx*3+1]),
				float64(imageSection.DecodedData[srcIdx*3+2]),
			}

			// 应用黑电平校正
			rawPixel[0] -= blackLevel[0]
			rawPixel[1] -= blackLevel[1]
			rawPixel[2] -= blackLevel[2]

			// 归一化到 [0, 1]
			normalized := colorspace.Vector3{
				colorspace.NormalizeToRange(rawPixel[0], float64(maxRaw[0])),
				colorspace.NormalizeToRange(rawPixel[1], float64(maxRaw[1])),
				colorspace.NormalizeToRange(rawPixel[2], float64(maxRaw[2])),
			}

			// 应用白平衡增益
			normalized = normalized.ComponentMul(gain)

			// 转换到 XYZ
			xyz := rawToXYZMat.Apply(normalized)

			// 转换到目标 RGB 色彩空间
			rgb := xyzToRGB.Apply(xyz)

			// 限制到 [0, 1]
			rgb = rgb.Clamp(0, 1)

			// 应用曝光调整
			if opts.AutoExposure {
				rgb, _ = colorspace.AutoExposure(rgb, 0.18)
			} else if opts.ExposureValue != 0 {
				rgb = colorspace.SimpleExposure(rgb, opts.ExposureValue)
			}

			// 色调映射
			if opts.ToneMapping && !opts.LinearOutput {
				rgb = colorspace.ACESToneMapping(rgb)
			}

			// Gamma 校正
			if opts.ApplyGamma && !opts.LinearOutput {
				gamma := colorspace.GetGamma(opts.ColorSpace)
				if gamma > 0 {
					rgb = colorspace.ApplyGammaToRGB(rgb, gamma)
				}
			}

			// 存储结果
			outIdx := int(outY)*int(targetWidth) + int(outX)
			processed.Data[outIdx*3] = rgb[0]
			processed.Data[outIdx*3+1] = rgb[1]
			processed.Data[outIdx*3+2] = rgb[2]
		}
	}

	return processed, nil
}

// ToUint16 转换为 16-bit 图像
func (img *ProcessedImage) ToUint16() []uint16 {
	result := make([]uint16, len(img.Data))
	for i, v := range img.Data {
		result[i] = uint16(v*65535 + 0.5)
		if result[i] > 65535 {
			result[i] = 65535
		}
	}
	return result
}

// ToUint8 转换为 8-bit 图像
func (img *ProcessedImage) ToUint8() []uint8 {
	result := make([]uint8, len(img.Data))
	for i, v := range img.Data {
		result[i] = uint8(v*255 + 0.5)
		if result[i] > 255 {
			result[i] = 255
		}
	}
	return result
}
