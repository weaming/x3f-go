package output

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/weaming/x3f-go/x3f"
)

// LinearACEScgData 是已经烘焙相机校正的场景线性 ACEScg 数据。
type LinearACEScgData struct {
	ImageData []byte
	Pixels    []float32
	Dims      imageDimensions
}

// ProcessLinearACEScg 将 X3F 解码为 float32 场景线性 ACEScg。
func ProcessLinearACEScg(x3fFile *x3f.File, config Config, logger *x3f.Logger) (*LinearACEScgData, error) {
	rawSection, err := x3fFile.LoadRawImageSection(logger)
	if err != nil {
		return nil, err
	}

	preprocessed, err := x3f.PreProcessImage(
		x3fFile,
		rawSection,
		x3f.PreProcessOptions{WhiteBalance: config.WhiteBalance, Denoise: !config.NoDenoise},
		logger,
	)
	if err != nil {
		return nil, err
	}
	if preprocessed.DataUint16 == nil {
		return nil, fmt.Errorf("intermediate 数据为空")
	}

	dims := calculateDimensions(rawSection, x3fFile, preprocessed, !config.NoCrop)
	pixels, err := convertIntermediateToACEScg(x3fFile, config.WhiteBalance, preprocessed, dims)
	if err != nil {
		return nil, err
	}

	return &LinearACEScgData{
		ImageData: float32PixelsToBytes(pixels),
		Pixels:    pixels,
		Dims:      dims,
	}, nil
}

func convertIntermediateToACEScg(
	x3fFile *x3f.File,
	wb string,
	preprocessed *x3f.PreprocessedData,
	dims imageDimensions,
) ([]float32, error) {
	rawToXYZ, ok := x3fFile.GetRawToXYZ(wb)
	if !ok {
		return nil, fmt.Errorf("无法获取 raw_to_xyz 矩阵")
	}

	conversionMatrix := x3f.GetXYZD65ToACEScg().Multiply(rawToXYZ)
	pixels := make([]float32, dims.targetWidth*dims.targetHeight*3)
	var spatialGains []x3f.SpatialGainCorr
	if x3fFile.ShouldApplySpatialGain() {
		spatialGains = x3fFile.GetSpatialGain(wb)
	}

	for outputY := uint32(0); outputY < dims.targetHeight; outputY++ {
		for outputX := uint32(0); outputX < dims.targetWidth; outputX++ {
			sourceX := int32(outputX) + dims.cropX
			sourceY := int32(outputY) + dims.cropY
			sourceIndex := (int(sourceY)*int(dims.decodedWidth) + int(sourceX)) * 3
			outputIndex := (int(outputY)*int(dims.targetWidth) + int(outputX)) * 3

			input := normalizedIntermediatePixel(preprocessed, sourceIndex)
			for channel := 0; channel < 3; channel++ {
				input[channel] *= x3f.CalcSpatialGain(
					spatialGains,
					int(outputY),
					int(outputX),
					channel,
					int(dims.targetHeight),
					int(dims.targetWidth),
				)
			}

			output := conversionMatrix.Apply(input)
			pixels[outputIndex] = float32(output[0])
			pixels[outputIndex+1] = float32(output[1])
			pixels[outputIndex+2] = float32(output[2])
		}
	}

	return pixels, nil
}

func normalizedIntermediatePixel(preprocessed *x3f.PreprocessedData, index int) x3f.Vector3 {
	return x3f.Vector3{
		(float64(preprocessed.DataUint16[index]) - preprocessed.IntermediateBias) / (float64(preprocessed.MaxIntermediate[0]) - preprocessed.IntermediateBias),
		(float64(preprocessed.DataUint16[index+1]) - preprocessed.IntermediateBias) / (float64(preprocessed.MaxIntermediate[1]) - preprocessed.IntermediateBias),
		(float64(preprocessed.DataUint16[index+2]) - preprocessed.IntermediateBias) / (float64(preprocessed.MaxIntermediate[2]) - preprocessed.IntermediateBias),
	}
}

func float32PixelsToBytes(pixels []float32) []byte {
	data := make([]byte, len(pixels)*4)
	for i, pixel := range pixels {
		binary.LittleEndian.PutUint32(data[i*4:], math.Float32bits(pixel))
	}

	return data
}
