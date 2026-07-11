package output

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
)

const (
	openEXRMagic         = 0x01312f76
	openEXRVersion       = 2
	openEXRFloatPixel    = 2
	openEXRNoCompression = 0
)

// ExportACEScgEXR 导出无压缩 float32 场景线性 ACEScg OpenEXR。
func ExportACEScgEXR(data *LinearACEScgData, filename string) error {
	file, err := createEXRFile(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	width := data.Dims.targetWidth
	height := data.Dims.targetHeight
	header := buildOpenEXRHeader(width, height)
	scanlineBytes := uint64(width) * 3 * 4
	chunkBytes := uint64(8) + scanlineBytes
	firstChunkOffset := uint64(8+len(header)) + uint64(height)*8

	if err := binary.Write(file, binary.LittleEndian, uint32(openEXRMagic)); err != nil {
		return fmt.Errorf("写入 OpenEXR magic 失败: %w", err)
	}
	if err := binary.Write(file, binary.LittleEndian, uint32(openEXRVersion)); err != nil {
		return fmt.Errorf("写入 OpenEXR version 失败: %w", err)
	}
	if _, err := file.Write(header); err != nil {
		return fmt.Errorf("写入 OpenEXR header 失败: %w", err)
	}

	for row := uint32(0); row < height; row++ {
		offset := firstChunkOffset + uint64(row)*chunkBytes
		if err := binary.Write(file, binary.LittleEndian, offset); err != nil {
			return fmt.Errorf("写入 OpenEXR offset table 失败: %w", err)
		}
	}

	scanline := make([]byte, scanlineBytes)
	for row := uint32(0); row < height; row++ {
		if err := binary.Write(file, binary.LittleEndian, int32(row)); err != nil {
			return fmt.Errorf("写入 OpenEXR scanline 坐标失败: %w", err)
		}
		if err := binary.Write(file, binary.LittleEndian, uint32(scanlineBytes)); err != nil {
			return fmt.Errorf("写入 OpenEXR scanline 长度失败: %w", err)
		}

		writeOpenEXRScanline(scanline, data.Pixels, width, row)
		if _, err := file.Write(scanline); err != nil {
			return fmt.Errorf("写入 OpenEXR scanline 数据失败: %w", err)
		}
	}

	return nil
}

func createEXRFile(filename string) (*os.File, error) {
	outputDir := filepath.Dir(filename)
	if outputDir != "." {
		if err := os.MkdirAll(outputDir, 0o755); err != nil {
			return nil, fmt.Errorf("创建 EXR 输出目录失败: %w", err)
		}
	}

	file, err := os.Create(filename)
	if err != nil {
		return nil, fmt.Errorf("创建 EXR 文件失败: %w", err)
	}

	return file, nil
}

func buildOpenEXRHeader(width, height uint32) []byte {
	header := &bytes.Buffer{}
	writeOpenEXRAttribute(header, "channels", "chlist", buildOpenEXRChannels())
	writeOpenEXRAttribute(header, "compression", "compression", []byte{openEXRNoCompression})
	writeOpenEXRAttribute(header, "dataWindow", "box2i", buildOpenEXRBox(width, height))
	writeOpenEXRAttribute(header, "displayWindow", "box2i", buildOpenEXRBox(width, height))
	writeOpenEXRAttribute(header, "lineOrder", "lineOrder", []byte{0})
	writeOpenEXRAttribute(header, "pixelAspectRatio", "float", openEXRFloat32(1))
	writeOpenEXRAttribute(header, "screenWindowCenter", "v2f", append(openEXRFloat32(0), openEXRFloat32(0)...))
	writeOpenEXRAttribute(header, "screenWindowWidth", "float", openEXRFloat32(1))
	writeOpenEXRAttribute(header, "chromaticities", "chromaticities", buildACEScgChromaticities())
	writeOpenEXRAttribute(header, "adoptedNeutral", "v2f", append(openEXRFloat32(0.32168), openEXRFloat32(0.33767)...))
	writeOpenEXRAttribute(header, "acesImageContainerFlag", "int", openEXRInt32(1))
	writeOpenEXRAttribute(header, "comments", "string", []byte("scene-linear ACEScg (AP1)"))
	header.WriteByte(0)

	return header.Bytes()
}

func buildOpenEXRChannels() []byte {
	channels := &bytes.Buffer{}
	for _, name := range []string{"B", "G", "R"} {
		writeOpenEXRString(channels, name)
		channels.Write(openEXRInt32(openEXRFloatPixel))
		channels.WriteByte(0)
		channels.Write([]byte{0, 0, 0})
		channels.Write(openEXRInt32(1))
		channels.Write(openEXRInt32(1))
	}
	channels.WriteByte(0)

	return channels.Bytes()
}

func buildOpenEXRBox(width, height uint32) []byte {
	box := &bytes.Buffer{}
	box.Write(openEXRInt32(0))
	box.Write(openEXRInt32(0))
	box.Write(openEXRInt32(int32(width - 1)))
	box.Write(openEXRInt32(int32(height - 1)))

	return box.Bytes()
}

func buildACEScgChromaticities() []byte {
	values := []float32{
		0.713, 0.293,
		0.165, 0.830,
		0.128, 0.044,
		0.32168, 0.33767,
	}
	data := &bytes.Buffer{}
	for _, value := range values {
		data.Write(openEXRFloat32(value))
	}

	return data.Bytes()
}

func writeOpenEXRAttribute(buffer *bytes.Buffer, name, typ string, data []byte) {
	writeOpenEXRString(buffer, name)
	writeOpenEXRString(buffer, typ)
	buffer.Write(openEXRInt32(int32(len(data))))
	buffer.Write(data)
}

func writeOpenEXRString(buffer *bytes.Buffer, value string) {
	buffer.WriteString(value)
	buffer.WriteByte(0)
}

func openEXRInt32(value int32) []byte {
	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, uint32(value))
	return data
}

func openEXRFloat32(value float32) []byte {
	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, math.Float32bits(value))
	return data
}

func writeOpenEXRScanline(scanline []byte, pixels []float32, width, row uint32) {
	channelOffset := 0
	for _, channel := range []int{2, 1, 0} {
		for col := uint32(0); col < width; col++ {
			pixelIndex := (row*width+col)*3 + uint32(channel)
			binary.LittleEndian.PutUint32(scanline[channelOffset+int(col)*4:], math.Float32bits(pixels[pixelIndex]))
		}
		channelOffset += int(width) * 4
	}
}
