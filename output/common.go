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
	Verbose         bool
	NoCrop          bool
	CompatibleWithC bool
	DumpMeta        bool
	Unprocessed     bool
	Qtop            bool
	Quality         int
	NoDenoise       bool    // 是否禁用降噪（默认启用）
	ExposureValue   float64 // 曝光补偿（EV 值）
}

var stdLevels = x3f.ImageLevels{
	Black: x3f.Vector3{0.0, 0.0, 0.0},
	White: [3]uint32{65535, 65535, 65535},
}

type CommonData struct {
	PreData *x3f.PreprocessedData
	ImgData []byte
	Dims    imageDimensions
}

func ProcessAll(x3fFile *x3f.File, config Config, logger *x3f.Logger) (*CommonData, error) {
	wb := config.WhiteBalance
	opts := x3f.ProcessOptions{
		WhiteBalanceType: wb,
		Denoise:          !config.NoDenoise,
		NoCrop:           config.NoCrop,
	}

	// 使用 ProcessImage 进行预处理（返回 intermediate 数据）
	preData, err := x3f.ProcessImage(x3fFile, opts, logger)
	if err != nil {
		return nil, err
	}

	// intermediate 数据（uint16）
	if preData.DataUint16 == nil {
		panic(fmt.Errorf("intermediate 数据为空"))
	}

	// 加载 RAW 图像段（用于 DNG 元数据）
	rawSection, err := x3fFile.LoadRawImageSection(logger)
	if err != nil {
		return nil, err
	}

	dims := calculateDimensions(rawSection, x3fFile, preData, !config.NoCrop)

	// 从 intermediate 数据准备图像数据（处理裁剪）
	imageData := preparePreprocessedImageData(preData.DataUint16, dims)

	// 应用色彩转换：intermediate → linear sRGB
	applyIntermediateToSRGB(imageData, dims, x3fFile, config.WhiteBalance, preData)

	return &CommonData{
		PreData: preData,
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

	maxOut := 65535.0

	for i := 0; i < totalPixels; i++ {
		offset := i * 6 // 16-bit RGB, 3 channels

		// 读取 linear sRGB 值 (uint16)
		r := float64(binary.LittleEndian.Uint16(imageData[offset:]))
		g := float64(binary.LittleEndian.Uint16(imageData[offset+2:]))
		b := float64(binary.LittleEndian.Uint16(imageData[offset+4:]))

		// 归一化到 [0, 1]
		rgb := x3f.Vector3{r / maxOut, g / maxOut, b / maxOut}

		// 可选的曝光补偿
		if config.ExposureValue != 0 {
			rgb = x3f.SimpleExposure(rgb, config.ExposureValue)
		}

		// 应用色调映射
		toneMappingMethod := getToneMappingMethod(config.ToneMapping)
		if toneMappingMethod != x3f.ToneMappingNone && toneMappingMethod != "" {
			rgb = x3f.ApplyToneMapping(rgb, toneMappingMethod)
		}

		// 应用 gamma 校正
		colorSpace := getColorSpace(config.ColorSpace)
		gamma := x3f.GetGamma(colorSpace)
		if gamma > 0 {
			rgb = x3f.ApplyGammaToRGB(rgb, gamma)
		}

		// 存储结果
		processed.Data[i*3] = rgb[0]
		processed.Data[i*3+1] = rgb[1]
		processed.Data[i*3+2] = rgb[2]
	}

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
