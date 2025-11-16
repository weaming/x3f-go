package x3f

import (
	"fmt"
)

// ProcessedImage 处理后的图像
type ProcessedImage struct {
	Width    uint32
	Height   uint32
	Channels uint32
	Data     []float64 // RGB 浮点数据 [0, 1]
}

// 转换为 16-bit 图像
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

// 转换为 8-bit 图像
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

// LoadRawImageSection 查找并加载 RAW 图像段
func (f *File) LoadRawImageSection(logger *Logger) (*ImageSection, error) {
	logger.Step("加载图像段")

	for _, entry := range f.Directory.Entries {
		isImageSection := entry.Type == SECi ||
			entry.Type == IMA2 ||
			entry.Type == IMAG

		if isImageSection {
			if err := f.LoadImageSection(&entry); err != nil {
				continue
			}
		}
	}

	if len(f.ImageData) == 0 {
		return nil, fmt.Errorf("未找到图像数据")
	}

	rawSection := f.ImageData[len(f.ImageData)-1]
	logger.Done("完成")

	return rawSection, nil
}
