package main

import (
	"fmt"
	"os"

	"github.com/weaming/x3f-go/x3f"
)

func dumpMetadata(x3fFile *x3f.File, config *Config) error {
	outputPath := config.Input + ".meta"

	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("无法创建元数据文件: %w", err)
	}
	defer f.Close()

	fmt.Fprintf(f, "BEGIN: file header meta data\n\n")
	dumpFileHeader(f, x3fFile)
	fmt.Fprintf(f, "END: file header meta data\n\n")

	if x3fFile.CAMFSection != nil {
		fmt.Fprintf(f, "BEGIN: CAMF meta data\n\n")
		dumpCAMFMetadata(f, x3fFile)
		fmt.Fprintf(f, "END: CAMF meta data\n\n")
	}

	if x3fFile.Properties != nil && len(x3fFile.Properties.Properties) > 0 {
		fmt.Fprintf(f, "BEGIN: PROP meta data\n\n")
		dumpProperties(f, x3fFile)
		fmt.Fprintf(f, "END: PROP meta data\n\n")
	} else {
		fmt.Fprintf(f, "INFO: No PROP meta data found\n\n")
	}

	fmt.Printf("   : READ THE X3F FILE %s\n", config.Input)
	fmt.Printf("   : Dump META DATA to %s\n", outputPath)
	fmt.Printf("   : Files processed: 1\terrors: 0\n")

	return nil
}

func dumpFileHeader(f *os.File, x3fFile *x3f.File) {
	h := x3fFile.Header

	// 反转 identifier 字节序以匹配 C 版本的输出
	identifier := uint32(h.Identifier[3])<<24 | uint32(h.Identifier[2])<<16 |
		uint32(h.Identifier[1])<<8 | uint32(h.Identifier[0])

	fmt.Fprintf(f, "header.\n")
	fmt.Fprintf(f, "  identifier        = %08x (FOVb)\n", identifier)
	fmt.Fprintf(f, "  version           = %08x\n", h.Version)

	// version < 4.0 才输出其他字段（Quattro 不输出）
	if h.Version < x3f.Version40 {
		// 移除末尾的 null 字节和空格
		wb := string(h.WhiteBalance[:])
		wb = trimNullAndSpace(wb)
		cm := string(h.ColorMode[:])
		cm = trimNullAndSpace(cm)

		fmt.Fprintf(f, "  unique_identifier = 30...\n")
		fmt.Fprintf(f, "  mark_bits         = %08x\n", h.MarkBits)
		fmt.Fprintf(f, "  columns           = %08x (%d)\n", h.Columns, h.Columns)
		fmt.Fprintf(f, "  rows              = %08x (%d)\n", h.Rows, h.Rows)
		fmt.Fprintf(f, "  rotation          = %08x (%d)\n", h.Rotation, h.Rotation)
		fmt.Fprintf(f, "  white_balance     = %s\n", wb)
		fmt.Fprintf(f, "  color_mode        = %s\n", cm)

		fmt.Fprintf(f, "  extended_types\n")
		for i := 0; i < 32; i++ {
			fmt.Fprintf(f, "    %2d: %3d = %9f\n", i, h.ExtendedDataTypes[i], h.ExtendedData[i])
		}
	}
}

func trimNullAndSpace(s string) string {
	// 移除 null 字节
	for i, c := range s {
		if c == 0 {
			s = s[:i]
			break
		}
	}
	// 移除末尾空格
	for len(s) > 0 && s[len(s)-1] == ' ' {
		s = s[:len(s)-1]
	}
	return s
}

func dumpCAMFMetadata(f *os.File, x3fFile *x3f.File) {
	if x3fFile.CAMFSection == nil {
		return
	}

	for _, entry := range x3fFile.CAMFSection.Entries {
		switch entry.ID {
		case x3f.CMbM: // Matrix
			dumpCAMFMatrix(f, entry)
		case x3f.CMbT: // Text
			dumpCAMFText(f, entry)
		case x3f.CMbP: // Property list
			dumpCAMFPropertyList(f, entry)
		}
	}
}

func dumpCAMFMatrix(f *os.File, entry *x3f.CAMFEntry) {
	fmt.Fprintf(f, "BEGIN: CAMF matrix meta data (%s)\n", entry.Name)

	// 确定类型名称
	var typeName string
	switch entry.MatrixType {
	case 0:
		typeName = "integer"
	case 1, 2:
		typeName = "unsigned integer"
	case 3:
		typeName = "float"
	case 5:
		typeName = "unsigned integer"
	case 6:
		typeName = "unsigned integer"
	default:
		typeName = "unknown"
	}

	// 打印维度 - 匹配C版本格式
	fmt.Fprintf(f, "%s ", typeName)
	for i := range entry.MatrixDims {
		fmt.Fprintf(f, "[%d]", entry.MatrixDims[i].Size)
	}
	fmt.Fprintf(f, "\n")

	// 打印维度名称 - 匹配C版本的顺序
	dim := len(entry.MatrixDims)
	if dim == 1 {
		fmt.Fprintf(f, "x: %s\n", entry.MatrixDims[0].Name)
	} else if dim == 2 {
		// x 对应最后一个维度, y 对应第一个维度
		fmt.Fprintf(f, "x: %s\n", entry.MatrixDims[1].Name)
		fmt.Fprintf(f, "y: %s\n", entry.MatrixDims[0].Name)
	} else if dim >= 3 {
		fmt.Fprintf(f, "x: %s\n", entry.MatrixDims[2].Name)
		fmt.Fprintf(f, "y: %s\n", entry.MatrixDims[1].Name)
		fmt.Fprintf(f, "z: %s (i.e. group)\n", entry.MatrixDims[0].Name)
	}

	// 打印矩阵数据
	if entry.MatrixDecoded != nil {
		// 计算 blocksize - 用于 3D 矩阵的额外换行
		blocksize := uint32(0xFFFFFFFF) // -1 as uint32
		if dim >= 3 {
			linesize := entry.MatrixDims[dim-1].Size
			blocksize = linesize * entry.MatrixDims[dim-2].Size
		}

		switch data := entry.MatrixDecoded.(type) {
		case []float64:
			printMatrixFloat(f, data, entry.MatrixDims, blocksize)
		case []uint32:
			printMatrixUint(f, data, entry.MatrixDims, blocksize)
		case []int32:
			printMatrixInt(f, data, entry.MatrixDims, blocksize)
		}
	}

	fmt.Fprintf(f, "END: CAMF matrix meta data\n\n")
}

func printMatrixFloat(f *os.File, data []float64, dims []x3f.CAMFDimEntry, blocksize uint32) {
	if len(data) == 0 {
		return
	}

	const maxPrintedElements = 100
	totalSize := len(data)
	linesize := int(dims[len(dims)-1].Size)

	for i, val := range data {
		if i >= maxPrintedElements {
			fmt.Fprintf(f, "\n... (%d skipped) ...\n", totalSize-i)
			break
		}
		fmt.Fprintf(f, "%12.6g ", val)
		if (i+1)%linesize == 0 {
			fmt.Fprintf(f, "\n")
		}
		if (i+1)%int(blocksize) == 0 {
			fmt.Fprintf(f, "\n")
		}
	}
}

func printMatrixUint(f *os.File, data []uint32, dims []x3f.CAMFDimEntry, blocksize uint32) {
	if len(data) == 0 {
		return
	}

	const maxPrintedElements = 100
	totalSize := len(data)
	linesize := int(dims[len(dims)-1].Size)

	for i, val := range data {
		if i >= maxPrintedElements {
			fmt.Fprintf(f, "\n... (%d skipped) ...\n", totalSize-i)
			break
		}
		fmt.Fprintf(f, "%12d ", val)
		if (i+1)%linesize == 0 {
			fmt.Fprintf(f, "\n")
		}
		if (i+1)%int(blocksize) == 0 {
			fmt.Fprintf(f, "\n")
		}
	}
}

func printMatrixInt(f *os.File, data []int32, dims []x3f.CAMFDimEntry, blocksize uint32) {
	if len(data) == 0 {
		return
	}

	const maxPrintedElements = 100
	totalSize := len(data)
	linesize := int(dims[len(dims)-1].Size)

	for i, val := range data {
		if i >= maxPrintedElements {
			fmt.Fprintf(f, "\n... (%d skipped) ...\n", totalSize-i)
			break
		}
		fmt.Fprintf(f, "%12d ", val)
		if (i+1)%linesize == 0 {
			fmt.Fprintf(f, "\n")
		}
		if (i+1)%int(blocksize) == 0 {
			fmt.Fprintf(f, "\n")
		}
	}
}

func dumpCAMFText(f *os.File, entry *x3f.CAMFEntry) {
	fmt.Fprintf(f, "BEGIN: CAMF text meta data (%s)\n", entry.Name)
	fmt.Fprintf(f, "\"%s\"\n", entry.Text)
	fmt.Fprintf(f, "END: CAMF text meta data\n\n")
}

func dumpCAMFPropertyList(f *os.File, entry *x3f.CAMFEntry) {
	fmt.Fprintf(f, "BEGIN: CAMF property meta data (%s)\n", entry.Name)

	for i := 0; i < len(entry.PropertyNames); i++ {
		name := entry.PropertyNames[i]
		value := entry.PropertyValues[i]
		fmt.Fprintf(f, "              \"%s\" = \"%s\"\n", name, value)
	}

	fmt.Fprintf(f, "END: CAMF property meta data\n\n")
}

func dumpProperties(f *os.File, x3fFile *x3f.File) {
	for i, prop := range x3fFile.Properties.Properties {
		fmt.Fprintf(f, "          [%d] \"%s\" = \"%s\"\n", i, prop.Name, prop.Value)
	}
}
