package output

import (
	"encoding/binary"
	"fmt"

	"github.com/weaming/x3f-go/x3f"
)

type Config struct {
	Input           string
	Output          string
	ColorSpace      string
	WhiteBalance    string
	ToneMapping     string
	NoCrop          bool
	CompatibleWithC bool
	DumpMeta        bool
	Unprocessed     bool
	Qtop            bool
	Quality         int
	NoDenoise       bool    // 是否禁用降噪（默认启用）
	ExposureValue   float64 // 曝光补偿（EV 值）
}

type FinalData struct {
	ImgData []byte
	Dims    imageDimensions
}

func ProcessAll(x3fFile *x3f.File, config Config, logger *x3f.Logger) (*FinalData, error) {
	// 加载 RAW 图像段（用于 DNG 元数据）
	rawSection, err := x3fFile.LoadRawImageSection(logger)
	if err != nil {
		return nil, err
	}

	// 使用 ProcessImage 进行预处理（返回 intermediate 数据）
	opts := x3f.PreProcessOptions{WhiteBalance: config.WhiteBalance, Denoise: !config.NoDenoise}
	preData, err := x3f.PreProcessImage(x3fFile, rawSection, opts, logger)
	if err != nil {
		return nil, err
	}

	// intermediate 数据（uint16）
	if preData.DataUint16 == nil {
		panic(fmt.Errorf("intermediate 数据为空"))
	}

	dims := calculateDimensions(rawSection, x3fFile, preData, !config.NoCrop)

	// 从 intermediate 数据准备图像数据（处理裁剪）
	imageData := preparePreprocessedImageData(preData.DataUint16, dims)

	// 应用色彩转换：intermediate → linear sRGB
	applyIntermediateToSRGB(imageData, dims, x3fFile, config.WhiteBalance, preData)

	return &FinalData{
		ImgData: imageData,
		Dims:    dims,
	}, nil
}

// applyPostProcessing 从 linear sRGB (byte array, uint16) 应用后处理
// 返回 ProcessedImage（float64，范围 [0, 1]）
func applyPostProcessing(imageData []byte, dims imageDimensions, config Config) *x3f.ProcessedImage {
	width := dims.targetWidth
	height := dims.targetHeight
	totalPixels := int(width * height)

	processed := &x3f.ProcessedImage{
		Width:    width,
		Height:   height,
		Channels: 3,
		Data:     make([]float64, totalPixels*3),
	}

	// Convert byte slice to uint16 slice for CGo
	if len(imageData)%2 != 0 {
		panic("imageData length must be a multiple of 2 to be converted to uint16")
	}
	inputData := make([]uint16, len(imageData)/2)
	for i := 0; i < len(inputData); i++ {
		inputData[i] = binary.LittleEndian.Uint16(imageData[i*2:])
	}

	toneMappingMethod := getToneMappingMethod(config.ToneMapping)
	colorSpace := getColorSpace(config.ColorSpace)
	gamma := x3f.GetGamma(colorSpace)

	// Use OpenCV accelerated function
	x3f.ApplyPostProcessingOpenCV(
		inputData,
		processed.Data,
		int(width),
		int(height),
		config.ExposureValue,
		toneMappingMethod,
		gamma,
	)

	return processed
}

func getColorSpace(name string) x3f.ColorSpace {
	switch name {
	case "none", "None":
		return x3f.ColorSpaceNone
	case "srgb", "sRGB":
		return x3f.ColorSpaceSRGB
	case "adobergb", "AdobeRGB":
		return x3f.ColorSpaceAdobeRGB
	case "prophoto", "prophotorgb", "ProPhotoRGB":
		return x3f.ColorSpaceProPhotoRGB
	default:
		return x3f.ColorSpaceSRGB
	}
}

func getToneMappingMethod(name string) x3f.ToneMappingMethod {
	switch name {
	case "aces", "ACES":
		return x3f.ToneMappingACES
	case "agx", "AgX":
		return x3f.ToneMappingAgX
	case "none", "None":
		return x3f.ToneMappingNone
	default:
		return x3f.ToneMappingAgX
	}
}
