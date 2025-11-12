package x3f

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
)

// CAMF 数据类型
const (
	CAMFType2 uint32 = 2
	CAMFType4 uint32 = 4
	CAMFType5 uint32 = 5
)

// CAMF Entry 类型
const (
	CMbP uint32 = 0x50624d43 // Property list
	CMbT uint32 = 0x54624d43 // Text
	CMbM uint32 = 0x4d624d43 // Matrix
)

// Matrix 数据类型
type MatrixType int

const (
	MatrixTypeFloat   MatrixType = iota // 8字节 float64
	MatrixTypeFloat32                   // 4字节 float32
	MatrixTypeUint32
	MatrixTypeInt32
	MatrixTypeUint16
	MatrixTypeInt16
	MatrixTypeUint8
)

// CAMFHeader CAMF 段头部
type CAMFHeader struct {
	Type uint32
	// Type 2
	Reserved        uint32
	InfoType        uint32
	InfoTypeVersion uint32
	CryptKey        uint32
	// Type 4
	DecodedDataSize uint32
	DecodeBias      uint32
	BlockSize       uint32
	BlockCount      uint32
}

// CAMFDimEntry 矩阵维度条目
type CAMFDimEntry struct {
	Size       uint32
	NameOffset uint32
	N          uint32
	Name       string // 维度名称（从 NameOffset 读取）
}

// CAMFEntry CAMF 条目
type CAMFEntry struct {
	// Entry header
	ID          uint32
	Version     uint32
	EntrySize   uint32
	NameOffset  uint32
	ValueOffset uint32

	// Computed values
	Name        string
	NameSize    uint32
	ValueSize   uint32
	EntryOffset uint32 // 在解码数据中的偏移

	// Type-specific data
	// For CMbT (Text)
	Text string

	// For CMbP (Property list)
	PropertyNames  []string
	PropertyValues []string

	// For CMbM (Matrix)
	MatrixDims        []CAMFDimEntry
	MatrixType        uint32
	MatrixElementSize uint32
	MatrixElements    uint32
	MatrixData        []byte
	MatrixDecoded     interface{} // []float64 or []uint32 or []int32
}

// CAMFData CAMF 数据段
type CAMFData struct {
	Header  CAMFHeader
	Entries []*CAMFEntry
}

// LoadCAMFSection 加载 CAMF 段
func (f *File) LoadCAMFSection(entry *DirectoryEntry) error {
	// 对于 version >= 4.0，entry.Type 直接是 CAMF
	// 对于 version < 4.0，entry.Type 是 SECc
	if entry.Type != SECc && entry.Type != CAMF {
		return fmt.Errorf("不是 CAMF 段")
	}

	data := make([]byte, entry.Length)
	_, err := f.reader.ReadAt(data, int64(entry.Offset))
	if err != nil {
		return fmt.Errorf("读取 CAMF 段失败: %w", err)
	}

	camf := &CAMFData{}

	// 检查数据是否以 SECc 标识符开头
	// SECc 格式：
	//   0-3: "SECc" 标识符
	//   4-7: Version
	//   8-11: CAMF type (!!!)  <-- SEC "type" 字段就是 CAMF type
	//   12-27: CAMF typeN 值 (4x4 = 16 字节) <-- SEC columns/rows/etc 就是 CAMF 参数
	//   28+: 编码的 CAMF 数据
	dataOffset := 0
	var camfType uint32

	if len(data) >= 12 {
		dataID := binary.LittleEndian.Uint32(data[0:4])
		debug("LoadCAMFSection: entry.Type=0x%08x, dataID=0x%08x, SECc=0x%08x", entry.Type, dataID, SECc)
		if dataID == SECc {
			// SECc 包装：跳过 28 字节的 SEC 头部
			debug("LoadCAMFSection: Data starts with SECc, skipping 28-byte SECc header")
			dataOffset = 28

			// CAMF type 在偏移 8
			camfType = binary.LittleEndian.Uint32(data[8:12])
			debug("LoadCAMFSection: CAMF type at offset 8: %d", camfType)

			// Type 4/5 的参数
			if camfType == 4 || camfType == 5 {
				decodedSize := binary.LittleEndian.Uint32(data[12:16])
				decodeBias := binary.LittleEndian.Uint32(data[16:20])
				blockSize := binary.LittleEndian.Uint32(data[20:24])
				blockCount := binary.LittleEndian.Uint32(data[24:28])
				debug("LoadCAMFSection: Type %d params: decodedSize=%d, decodeBias=%d, blockSize=%d, blockCount=%d",
					camfType, decodedSize, decodeBias, blockSize, blockCount)
			}
		}
	}

	// 解码 CAMF 数据
	encodedData := data[dataOffset:]
	var decodedData []byte
	var decodeErr error

	switch camfType {
	case 2:
		// Type 2: XOR 解密
		decodedData, decodeErr = f.decodeCAMFType2(encodedData, data)
	case 4:
		// Type 4: Huffman 解码
		decodedData, decodeErr = f.decodeCAMFType4(encodedData, data)
	case 5:
		// Type 5: Huffman 解码（简化版）
		decodedData, decodeErr = f.decodeCAMFType5(encodedData, data)
	default:
		return fmt.Errorf("unsupported CAMF type: %d", camfType)
	}

	if decodeErr != nil {
		return fmt.Errorf("failed to decode CAMF: %w", decodeErr)
	}

	debug("LoadCAMFSection: decoded %d bytes", len(decodedData))

	// 解析解码后的数据中的 entries
	offset := int64(0)
	for offset < int64(len(decodedData)) {
		camfEntry, bytesRead, err := f.parseCAMFEntry(decodedData[offset:], uint32(offset))
		if err != nil {
			debug("LoadCAMFSection: parse error at offset %d: %v", offset, err)
			break
		}
		debug("LoadCAMFSection: parsed entry '%s' (ID=0x%08x) at offset %d, size=%d",
			camfEntry.Name, camfEntry.ID, offset, bytesRead)
		camf.Entries = append(camf.Entries, camfEntry)
		offset += bytesRead
	}

	debug("LoadCAMFSection: loaded %d entries", len(camf.Entries))
	f.CAMFSection = camf
	return nil
}

// parseCAMFEntry 解析一个 CAMF 条目
func (f *File) parseCAMFEntry(data []byte, entryOffset uint32) (*CAMFEntry, int64, error) {
	if len(data) < 20 {
		return nil, 0, fmt.Errorf("数据太短")
	}

	entry := &CAMFEntry{
		EntryOffset: entryOffset,
	}
	buf := bytes.NewReader(data)

	// 读取头部
	if err := binary.Read(buf, binary.LittleEndian, &entry.ID); err != nil {
		return nil, 0, err
	}
	if err := binary.Read(buf, binary.LittleEndian, &entry.Version); err != nil {
		return nil, 0, err
	}
	if err := binary.Read(buf, binary.LittleEndian, &entry.EntrySize); err != nil {
		return nil, 0, err
	}
	if err := binary.Read(buf, binary.LittleEndian, &entry.NameOffset); err != nil {
		return nil, 0, err
	}
	if err := binary.Read(buf, binary.LittleEndian, &entry.ValueOffset); err != nil {
		return nil, 0, err
	}

	debug("parseCAMFEntry: ID=0x%08x, Version=%d, EntrySize=%d, NameOffset=%d, ValueOffset=%d, dataLen=%d",
		entry.ID, entry.Version, entry.EntrySize, entry.NameOffset, entry.ValueOffset, len(data))

	// 读取名称
	if entry.NameOffset < 20 || int(entry.NameOffset) >= len(data) {
		return nil, 0, fmt.Errorf("无效的名称偏移: NameOffset=%d, dataLen=%d", entry.NameOffset, len(data))
	}

	nameEnd := entry.ValueOffset
	if entry.ValueOffset == 0 {
		nameEnd = entry.EntrySize
	}
	entry.NameSize = nameEnd - entry.NameOffset

	nameBytes := data[entry.NameOffset:nameEnd]
	// 查找 null 终止符
	nullPos := bytes.IndexByte(nameBytes, 0)
	if nullPos >= 0 {
		nameBytes = nameBytes[:nullPos]
	}
	entry.Name = string(nameBytes)

	// 根据类型解析数据
	switch entry.ID {
	case CMbT:
		f.parseCAMFTextEntry(entry, data)
	case CMbP:
		f.parseCAMFPropertyEntry(entry, data)
	case CMbM:
		f.parseCAMFMatrixEntry(entry, data)
	}

	return entry, int64(entry.EntrySize), nil
}

// parseCAMFTextEntry 解析文本条目
func (f *File) parseCAMFTextEntry(entry *CAMFEntry, data []byte) error {
	if entry.ValueOffset == 0 || int(entry.ValueOffset) >= len(data) {
		return nil
	}

	// Text entry 的 value 部分：前4字节是文本长度，然后是文本内容
	if int(entry.ValueOffset)+4 > len(data) {
		return nil
	}

	textSize := binary.LittleEndian.Uint32(data[entry.ValueOffset : entry.ValueOffset+4])
	textStart := entry.ValueOffset + 4
	entry.ValueSize = textSize

	if int(textStart+textSize) > len(data) {
		textSize = uint32(len(data)) - textStart
	}

	if textSize > 0 {
		textBytes := data[textStart : textStart+textSize]
		// 移除尾部的null字节
		nullPos := bytes.IndexByte(textBytes, 0)
		if nullPos >= 0 {
			textBytes = textBytes[:nullPos]
		}
		entry.Text = string(textBytes)
		debug("parseCAMFTextEntry: '%s' text='%s' (len=%d)", entry.Name, entry.Text, len(entry.Text))
	}

	return nil
}

// parseCAMFPropertyEntry 解析属性列表条目
func (f *File) parseCAMFPropertyEntry(entry *CAMFEntry, data []byte) error {
	if entry.ValueOffset == 0 || int(entry.ValueOffset) >= len(data) {
		return nil
	}

	buf := bytes.NewReader(data[entry.ValueOffset:])

	// 读取属性数量
	var numProperties uint32
	if err := binary.Read(buf, binary.LittleEndian, &numProperties); err != nil {
		return err
	}

	// 读取偏移基准(相对于 entry 起始的字节偏移)
	var offsetBase uint32
	if err := binary.Read(buf, binary.LittleEndian, &offsetBase); err != nil {
		return err
	}

	if debugEnabled {
		fmt.Printf("parseCAMFPropertyEntry: '%s' numProperties=%d, offsetBase=%d\n",
			entry.Name, numProperties, offsetBase)
	}

	// 读取属性表
	type propertyTableEntry struct {
		NameOffset  uint32 // 相对于 offsetBase 的字节偏移
		ValueOffset uint32 // 相对于 offsetBase 的字节偏移
	}

	propertyTable := make([]propertyTableEntry, numProperties)
	for i := uint32(0); i < numProperties; i++ {
		if err := binary.Read(buf, binary.LittleEndian, &propertyTable[i].NameOffset); err != nil {
			return err
		}
		if err := binary.Read(buf, binary.LittleEndian, &propertyTable[i].ValueOffset); err != nil {
			return err
		}
		if debugEnabled && i < 3 {
			fmt.Printf("  property[%d]: nameOff=%d, valueOff=%d\n", i, propertyTable[i].NameOffset, propertyTable[i].ValueOffset)
		}
	}

	// 偏移是相对于 data 起始的,data 从 entry.EntryOffset 开始
	// offsetBase 是相对于 data[0] (即 entry 起始) 的偏移
	// 所以实际位置是 data[offsetBase + name/value offset]

	if debugEnabled {
		// 打印前 64 字节的字符串数据池
		dataOff := offsetBase
		if int(dataOff)+64 < len(data) {
			fmt.Printf("  string pool at offset %d: %x\n", dataOff, data[dataOff:dataOff+64])
		}
	}

	entry.PropertyNames = make([]string, numProperties)
	entry.PropertyValues = make([]string, numProperties)

	for i := uint32(0); i < numProperties; i++ {
		// 偏移量是相对于 data[0] (entry 起始)
		// 字符串是 null-terminated ASCII

		// 读取名称 (null-terminated ASCII string)
		nameOffset := offsetBase + propertyTable[i].NameOffset
		if int(nameOffset) < len(data) {
			nameBytes := data[nameOffset:]
			// 查找 null terminator
			nullPos := -1
			for j := 0; j < len(nameBytes); j++ {
				if nameBytes[j] == 0 {
					nullPos = j
					break
				}
			}
			if nullPos >= 0 {
				entry.PropertyNames[i] = string(nameBytes[:nullPos])
			}
		}

		// 读取值 (null-terminated ASCII string)
		valueOffset := offsetBase + propertyTable[i].ValueOffset
		if int(valueOffset) < len(data) {
			valueBytes := data[valueOffset:]
			// 查找 null terminator
			nullPos := -1
			for j := 0; j < len(valueBytes); j++ {
				if valueBytes[j] == 0 {
					nullPos = j
					break
				}
			}
			if nullPos >= 0 {
				entry.PropertyValues[i] = string(valueBytes[:nullPos])
			}
		}
	}

	return nil
}

// parseCAMFMatrixEntry 解析矩阵条目
func (f *File) parseCAMFMatrixEntry(entry *CAMFEntry, data []byte) error {
	if entry.ValueOffset == 0 || int(entry.ValueOffset) >= len(data) {
		return nil
	}

	buf := bytes.NewReader(data[entry.ValueOffset:])

	// 读取矩阵类型 (matrix_type, +0)
	if err := binary.Read(buf, binary.LittleEndian, &entry.MatrixType); err != nil {
		return err
	}

	// 读取矩阵维度数 (matrix_dim, +4)
	var numDims uint32
	if err := binary.Read(buf, binary.LittleEndian, &numDims); err != nil {
		return err
	}

	// 读取矩阵数据偏移 (matrix_data_off, +8)
	var matrixDataOff uint32
	if err := binary.Read(buf, binary.LittleEndian, &matrixDataOff); err != nil {
		return err
	}

	// 读取每个维度
	entry.MatrixDims = make([]CAMFDimEntry, numDims)
	entry.MatrixElements = 1
	for i := uint32(0); i < numDims; i++ {
		if err := binary.Read(buf, binary.LittleEndian, &entry.MatrixDims[i].Size); err != nil {
			return err
		}
		if err := binary.Read(buf, binary.LittleEndian, &entry.MatrixDims[i].NameOffset); err != nil {
			return err
		}
		if err := binary.Read(buf, binary.LittleEndian, &entry.MatrixDims[i].N); err != nil {
			return err
		}

		// 读取维度名称（如果有）
		nameOffset := entry.MatrixDims[i].NameOffset
		if nameOffset > 0 && int(nameOffset) < len(data) {
			// 查找 null 结尾
			nameEnd := nameOffset
			for nameEnd < uint32(len(data)) && data[nameEnd] != 0 {
				nameEnd++
			}
			if nameEnd > nameOffset {
				entry.MatrixDims[i].Name = string(data[nameOffset:nameEnd])
			}
		}

		debug("parseCAMFMatrixEntry: '%s' dim[%d] Size=%d, NameOffset=%d, N=%d, Name='%s'",
			entry.Name, i, entry.MatrixDims[i].Size, entry.MatrixDims[i].NameOffset, entry.MatrixDims[i].N, entry.MatrixDims[i].Name)
		entry.MatrixElements *= entry.MatrixDims[i].Size
	}

	debug("parseCAMFMatrixEntry: '%s' numDims=%d, MatrixElements=%d, MatrixType=%d, MatrixDataOff=%d",
		entry.Name, numDims, entry.MatrixElements, entry.MatrixType, matrixDataOff)

	// 矩阵数据偏移是相对于条目起始的
	dataOffset := matrixDataOff

	// 计算可用空间
	availableSpace := entry.EntrySize - matrixDataOff
	if entry.MatrixElements == 0 {
		debug("parseCAMFMatrixEntry: MatrixElements is 0, skipping")
		return nil
	}
	entry.MatrixElementSize = availableSpace / entry.MatrixElements

	// 读取矩阵数据
	matrixDataSize := entry.MatrixElements * entry.MatrixElementSize
	entry.MatrixData = make([]byte, matrixDataSize)
	if int(dataOffset+matrixDataSize) <= len(data) {
		copy(entry.MatrixData, data[dataOffset:dataOffset+matrixDataSize])
	}

	// 解码矩阵数据
	f.decodeMatrixData(entry)

	return nil
}

// decodeMatrixData 解码矩阵数据
func (f *File) decodeMatrixData(entry *CAMFEntry) error {
	if len(entry.MatrixData) == 0 {
		return nil
	}

	// 根据 entry.MatrixType 决定如何解码
	// MatrixType 参考 C 代码 set_matrix_element_info:
	//   0: 2 bytes, int16
	//   1: 4 bytes, uint32
	//   2: 4 bytes, uint32
	//   3: 4 bytes, float32  ← 重要!
	//   5: 1 byte,  uint8
	//   6: 2 bytes, uint16
	var matrixType MatrixType

	switch entry.MatrixType {
	case 0:
		matrixType = MatrixTypeInt16
	case 1, 2:
		matrixType = MatrixTypeUint32
	case 3:
		matrixType = MatrixTypeFloat32 // 4字节 float
	case 5:
		matrixType = MatrixTypeUint8
	case 6:
		matrixType = MatrixTypeUint16
	default:
		// 兜底:根据元素大小判断
		if entry.MatrixElementSize == 8 {
			matrixType = MatrixTypeFloat
		} else if entry.MatrixElementSize == 4 {
			matrixType = MatrixTypeUint32
		} else if entry.MatrixElementSize == 2 {
			matrixType = MatrixTypeUint16
		} else {
			// 未知类型
			return nil
		}
	}

	// 解码数据
	buf := bytes.NewReader(entry.MatrixData)

	switch matrixType {
	case MatrixTypeFloat:
		floatData := make([]float64, entry.MatrixElements)
		for i := uint32(0); i < entry.MatrixElements; i++ {
			var val float64
			if err := binary.Read(buf, binary.LittleEndian, &val); err != nil {
				break
			}
			// 检查是否为 NaN 或 Inf
			if math.IsNaN(val) || math.IsInf(val, 0) {
				val = 0
			}
			floatData[i] = val
		}
		entry.MatrixDecoded = floatData

	case MatrixTypeFloat32:
		// 读取 float32 并转换为 float64
		floatData := make([]float64, entry.MatrixElements)
		for i := uint32(0); i < entry.MatrixElements; i++ {
			var val float32
			if err := binary.Read(buf, binary.LittleEndian, &val); err != nil {
				break
			}
			// 检查是否为 NaN 或 Inf
			val64 := float64(val)
			if math.IsNaN(val64) || math.IsInf(val64, 0) {
				val64 = 0
			}
			floatData[i] = val64
		}
		entry.MatrixDecoded = floatData

	case MatrixTypeUint32:
		uintData := make([]uint32, entry.MatrixElements)
		for i := uint32(0); i < entry.MatrixElements; i++ {
			var val uint32
			if err := binary.Read(buf, binary.LittleEndian, &val); err != nil {
				break
			}
			uintData[i] = val
		}
		entry.MatrixDecoded = uintData

	case MatrixTypeInt32:
		intData := make([]int32, entry.MatrixElements)
		for i := uint32(0); i < entry.MatrixElements; i++ {
			var val int32
			if err := binary.Read(buf, binary.LittleEndian, &val); err != nil {
				break
			}
			intData[i] = val
		}
		entry.MatrixDecoded = intData

	case MatrixTypeUint16:
		// 读取 uint16 并转换为 uint32 以便统一处理
		uintData := make([]uint32, entry.MatrixElements)
		for i := uint32(0); i < entry.MatrixElements; i++ {
			var val uint16
			if err := binary.Read(buf, binary.LittleEndian, &val); err != nil {
				break
			}
			uintData[i] = uint32(val)
		}
		entry.MatrixDecoded = uintData

	case MatrixTypeInt16:
		// 读取 int16 并转换为 int32
		intData := make([]int32, entry.MatrixElements)
		for i := uint32(0); i < entry.MatrixElements; i++ {
			var val int16
			if err := binary.Read(buf, binary.LittleEndian, &val); err != nil {
				break
			}
			intData[i] = int32(val)
		}
		entry.MatrixDecoded = intData

	case MatrixTypeUint8:
		// 读取 uint8 并转换为 uint32
		uintData := make([]uint32, entry.MatrixElements)
		for i := uint32(0); i < entry.MatrixElements; i++ {
			var val uint8
			if err := binary.Read(buf, binary.LittleEndian, &val); err != nil {
				break
			}
			uintData[i] = uint32(val)
		}
		entry.MatrixDecoded = uintData
	}

	return nil
}

// GetCAMFText 获取文本条目
func (f *File) GetCAMFText(name string) (string, bool) {
	if f.CAMFSection == nil {
		return "", false
	}

	for _, entry := range f.CAMFSection.Entries {
		if entry.Name == name && entry.ID == CMbT {
			return entry.Text, true
		}
	}

	return "", false
}

// GetCAMFProperty 获取属性值
func (f *File) GetCAMFProperty(listName, propName string) (string, bool) {
	if f.CAMFSection == nil {
		return "", false
	}

	for _, entry := range f.CAMFSection.Entries {
		if entry.Name == listName && entry.ID == CMbP {
			if debugEnabled {
				fmt.Printf("GetCAMFProperty: Found property list '%s' with %d entries\n", listName, len(entry.PropertyNames))
				for i, name := range entry.PropertyNames {
					fmt.Printf("  [%d] '%s' = '%s'\n", i, name, entry.PropertyValues[i])
				}
			}
			for i, name := range entry.PropertyNames {
				if name == propName {
					return entry.PropertyValues[i], true
				}
			}
		}
	}

	if debugEnabled {
		fmt.Printf("GetCAMFProperty: Property '%s' not found in list '%s'\n", propName, listName)
	}
	return "", false
}

// GetCAMFFloat 获取单个浮点数
func (f *File) GetCAMFFloat(name string) (float64, bool) {
	if f.CAMFSection == nil {
		return 0, false
	}

	for _, entry := range f.CAMFSection.Entries {
		if entry.Name == name && entry.ID == CMbM {
			if entry.MatrixElements != 1 {
				continue
			}
			if floatData, ok := entry.MatrixDecoded.([]float64); ok && len(floatData) > 0 {
				return floatData[0], true
			}
		}
	}

	return 0, false
}

// GetCAMFUint32 获取单个无符号整数
func (f *File) GetCAMFUint32(name string) (uint32, bool) {
	if f.CAMFSection == nil {
		return 0, false
	}

	for _, entry := range f.CAMFSection.Entries {
		if entry.Name == name && entry.ID == CMbM {
			if entry.MatrixElements != 1 {
				continue
			}
			if uintData, ok := entry.MatrixDecoded.([]uint32); ok && len(uintData) > 0 {
				return uintData[0], true
			}
		}
	}

	return 0, false
}

// GetCAMFInt32 获取单个有符号整数
func (f *File) GetCAMFInt32(name string) (int32, bool) {
	if f.CAMFSection == nil {
		return 0, false
	}

	for _, entry := range f.CAMFSection.Entries {
		if entry.Name == name && entry.ID == CMbM {
			if entry.MatrixElements != 1 {
				continue
			}
			if intData, ok := entry.MatrixDecoded.([]int32); ok && len(intData) > 0 {
				return intData[0], true
			}
		}
	}

	return 0, false
}

// GetCAMFFloatVector 获取浮点数向量
func (f *File) GetCAMFFloatVector(name string, expectedSize uint32) ([]float64, bool) {
	if f.CAMFSection == nil {
		return nil, false
	}

	for _, entry := range f.CAMFSection.Entries {
		if entry.Name == name && entry.ID == CMbM {
			if expectedSize > 0 && entry.MatrixElements != expectedSize {
				continue
			}
			if floatData, ok := entry.MatrixDecoded.([]float64); ok {
				return floatData, true
			}
		}
	}

	return nil, false
}

// GetCAMFInt32Vector 获取有符号整数向量
func (f *File) GetCAMFInt32Vector(name string, expectedSize uint32) ([]int32, bool) {
	if f.CAMFSection == nil {
		return nil, false
	}

	for _, entry := range f.CAMFSection.Entries {
		if entry.Name == name && entry.ID == CMbM {
			if expectedSize > 0 && entry.MatrixElements != expectedSize {
				continue
			}
			if intData, ok := entry.MatrixDecoded.([]int32); ok {
				return intData, true
			}
		}
	}

	return nil, false
}

// GetCAMFMatrix 获取矩阵（通用方法）
func (f *File) GetCAMFMatrix(name string) (interface{}, []uint32, bool) {
	if f.CAMFSection == nil {
		return nil, nil, false
	}

	for _, entry := range f.CAMFSection.Entries {
		if entry.Name == name && entry.ID == CMbM {
			dims := make([]uint32, len(entry.MatrixDims))
			for i, d := range entry.MatrixDims {
				dims[i] = d.Size
			}
			return entry.MatrixDecoded, dims, true
		}
	}

	return nil, nil, false
}

// GetCAMFMatrixUint32 获取指定维度的 uint32 矩阵
func (f *File) GetCAMFMatrixUint32(name string, expectedRows, expectedCols uint32) ([]uint32, bool) {
	data, dims, ok := f.GetCAMFMatrix(name)
	if !ok {
		return nil, false
	}

	// 检查维度
	if len(dims) != 2 || dims[0] != expectedRows || dims[1] != expectedCols {
		return nil, false
	}

	// 转换为 uint32 数组
	switch v := data.(type) {
	case []uint32:
		return v, true
	default:
		return nil, false
	}
}

// GetWhiteBalance 获取白平衡预设名称
func (f *File) GetWhiteBalance() string {
	if wb, ok := f.GetCAMFUint32("WhiteBalance"); ok {
		switch wb {
		case 1:
			return "Auto"
		case 2:
			return "Sunlight"
		case 3:
			return "Shadow"
		case 4:
			return "Overcast"
		case 5:
			return "Incandescent"
		case 6:
			return "Florescent"
		case 7:
			return "Flash"
		case 8:
			return "Custom"
		case 11:
			return "ColorTemp"
		case 12:
			return "AutoLSP"
		default:
			return "Auto"
		}
	}

	// 从文件头获取白平衡
	wb := string(bytes.TrimRight(f.Header.WhiteBalance[:], "\x00"))
	if wb != "" {
		return wb
	}

	return "Auto"
}

// IsTRUEEngine 判断是否为 TRUE 引擎
func (f *File) IsTRUEEngine() bool {
	if f.CAMFSection == nil {
		return false
	}

	// 检查是否有 WhiteBalanceColorCorrections 和 WhiteBalanceGains
	hasColorCorrections := false
	hasGains := false

	for _, entry := range f.CAMFSection.Entries {
		if entry.ID == CMbP {
			if entry.Name == "WhiteBalanceColorCorrections" || entry.Name == "DP1_WhiteBalanceColorCorrections" {
				hasColorCorrections = true
			}
			if entry.Name == "WhiteBalanceGains" || entry.Name == "DP1_WhiteBalanceGains" {
				hasGains = true
			}
		}
	}

	return hasColorCorrections && hasGains
}

// GetMaxRAW 获取最大 RAW 值
func (f *File) GetMaxRAW() ([3]uint32, bool) {
	// 优先尝试从 ImageDepth 获取
	if imageDepth, ok := f.GetCAMFUint32("ImageDepth"); ok {
		maxVal := (uint32(1) << imageDepth) - 1
		debug("GetMaxRAW: from ImageDepth=%d, maxVal=%d", imageDepth, maxVal)
		return [3]uint32{maxVal, maxVal, maxVal}, true
	}

	// 根据是否为 TRUE 引擎选择不同的字段
	var fieldName string
	if f.IsTRUEEngine() {
		fieldName = "RawSaturationLevel"
	} else {
		fieldName = "SaturationLevel"
	}

	debug("GetMaxRAW: trying field '%s'", fieldName)
	if maxRaw, ok := f.GetCAMFInt32Vector(fieldName, 3); ok && len(maxRaw) == 3 {
		debug("GetMaxRAW: from %s = (%d, %d, %d)", fieldName, maxRaw[0], maxRaw[1], maxRaw[2])
		return [3]uint32{uint32(maxRaw[0]), uint32(maxRaw[1]), uint32(maxRaw[2])}, true
	}

	debug("GetMaxRAW: not found")
	return [3]uint32{0, 0, 0}, false
}

// GetCAMFMatrixForWB 根据白平衡预设获取矩阵
func (f *File) GetCAMFMatrixForWB(listName, wb string, dims []uint32) ([]float64, bool) {
	// 从属性列表中获取矩阵名称
	matrixName, ok := f.GetCAMFProperty(listName, wb)
	if !ok {
		if debugEnabled {
			fmt.Printf("GetCAMFMatrixForWB: GetCAMFProperty('%s', '%s') failed\n", listName, wb)
		}
		// SD1 的 Workaround: Daylight -> Sunlight
		if wb == "Daylight" {
			return f.GetCAMFMatrixForWB(listName, "Sunlight", dims)
		}
		return nil, false
	}

	if debugEnabled {
		fmt.Printf("GetCAMFMatrixForWB: '%s' -> '%s', looking up matrix...\n", wb, matrixName)
	}

	// 获取矩阵数据
	data, matrixDims, ok := f.GetCAMFMatrix(matrixName)
	if !ok {
		if debugEnabled {
			fmt.Printf("GetCAMFMatrixForWB: GetCAMFMatrix('%s') failed\n", matrixName)
		}
		return nil, false
	}

	if debugEnabled {
		fmt.Printf("GetCAMFMatrixForWB: got matrix '%s' with dims %v\n", matrixName, matrixDims)
	}

	// 检查维度
	if len(dims) > 0 {
		if len(matrixDims) != len(dims) {
			if debugEnabled {
				fmt.Printf("GetCAMFMatrixForWB: dimension count mismatch: expected %d, got %d\n", len(dims), len(matrixDims))
			}
			return nil, false
		}
		for i, expected := range dims {
			if expected > 0 && matrixDims[i] != expected {
				if debugEnabled {
					fmt.Printf("GetCAMFMatrixForWB: dimension[%d] mismatch: expected %d, got %d\n", i, expected, matrixDims[i])
				}
				return nil, false
			}
		}
	}

	// 转换为 float64
	if floatData, ok := data.([]float64); ok {
		if debugEnabled {
			fmt.Printf("GetCAMFMatrixForWB: successfully got float data, length=%d\n", len(floatData))
		}
		return floatData, true
	}

	if debugEnabled {
		fmt.Printf("GetCAMFMatrixForWB: data is not []float64, type=%T\n", data)
	}
	return nil, false
}

// GetWhiteBalanceGain 获取白平衡增益
func (f *File) GetWhiteBalanceGain(wb string) ([3]float64, bool) {
	var gain [3]float64

	// 尝试从 WhiteBalanceGains 或 DP1_WhiteBalanceGains 获取
	if gainVec, ok := f.GetCAMFMatrixForWB("WhiteBalanceGains", wb, []uint32{3}); ok && len(gainVec) == 3 {
		gain[0], gain[1], gain[2] = gainVec[0], gainVec[1], gainVec[2]
	} else if gainVec, ok := f.GetCAMFMatrixForWB("DP1_WhiteBalanceGains", wb, []uint32{3}); ok && len(gainVec) == 3 {
		gain[0], gain[1], gain[2] = gainVec[0], gainVec[1], gainVec[2]
	} else if camToXYZ, ok1 := f.GetCAMFMatrixForWB("WhiteBalanceIlluminants", wb, []uint32{3, 3}); ok1 {
		// 旧式白平衡计算
		if wbCorrection, ok2 := f.GetCAMFMatrixForWB("WhiteBalanceCorrections", wb, []uint32{3, 3}); ok2 {
			// raw_to_xyz = wb_correction * cam_to_xyz
			rawToXYZ := multiply3x3(wbCorrection, camToXYZ)

			// 计算 raw_neutral
			rawNeutral := getRawNeutral(rawToXYZ)

			// gain = 1 / raw_neutral
			gain[0] = 1.0 / rawNeutral[0]
			gain[1] = 1.0 / rawNeutral[1]
			gain[2] = 1.0 / rawNeutral[2]
		} else {
			return [3]float64{}, false
		}
	} else {
		return [3]float64{}, false
	}

	// 应用传感器调整增益
	if sensorGain, ok := f.GetCAMFFloatVector("SensorAdjustmentGainFact", 3); ok && len(sensorGain) == 3 {
		gain[0] *= sensorGain[0]
		gain[1] *= sensorGain[1]
		gain[2] *= sensorGain[2]
	}

	// 应用温度增益
	if tempGain, ok := f.GetCAMFFloatVector("TempGainFact", 3); ok && len(tempGain) == 3 {
		gain[0] *= tempGain[0]
		gain[1] *= tempGain[1]
		gain[2] *= tempGain[2]
	}

	// 应用光圈增益
	if fNumberGain, ok := f.GetCAMFFloatVector("FNumberGainFact", 3); ok && len(fNumberGain) == 3 {
		gain[0] *= fNumberGain[0]
		gain[1] *= fNumberGain[1]
		gain[2] *= fNumberGain[2]
	}

	return gain, true
}

// getRawNeutral 计算 RAW 中性值
func getRawNeutral(rawToXYZ []float64) [3]float64 {
	// XYZ 中性值 (D65 白点)
	xyzNeutral := [3]float64{0.950456, 1.0, 1.089058}

	// xyz_to_raw = inverse(raw_to_xyz)
	xyzToRaw := Inverse3x3(rawToXYZ)

	// raw_neutral = xyz_to_raw * xyz_neutral
	rawNeutral := [3]float64{
		xyzToRaw[0]*xyzNeutral[0] + xyzToRaw[1]*xyzNeutral[1] + xyzToRaw[2]*xyzNeutral[2],
		xyzToRaw[3]*xyzNeutral[0] + xyzToRaw[4]*xyzNeutral[1] + xyzToRaw[5]*xyzNeutral[2],
		xyzToRaw[6]*xyzNeutral[0] + xyzToRaw[7]*xyzNeutral[1] + xyzToRaw[8]*xyzNeutral[2],
	}

	return rawNeutral
}

// multiply3x3 3x3 矩阵相乘
func multiply3x3(a, b []float64) []float64 {
	if len(a) != 9 || len(b) != 9 {
		return make([]float64, 9)
	}

	result := make([]float64, 9)
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			sum := 0.0
			for k := 0; k < 3; k++ {
				sum += a[i*3+k] * b[k*3+j]
			}
			result[i*3+j] = sum
		}
	}
	return result
}

// Inverse3x3 3x3 矩阵求逆
func Inverse3x3(m []float64) []float64 {
	if len(m) != 9 {
		return make([]float64, 9)
	}

	inv := make([]float64, 9)

	// 计算行列式
	det := m[0]*(m[4]*m[8]-m[5]*m[7]) -
		m[1]*(m[3]*m[8]-m[5]*m[6]) +
		m[2]*(m[3]*m[7]-m[4]*m[6])

	if math.Abs(det) < 1e-10 {
		// 矩阵奇异，返回单位矩阵
		inv[0], inv[4], inv[8] = 1, 1, 1
		return inv
	}

	invDet := 1.0 / det

	inv[0] = (m[4]*m[8] - m[5]*m[7]) * invDet
	inv[1] = (m[2]*m[7] - m[1]*m[8]) * invDet
	inv[2] = (m[1]*m[5] - m[2]*m[4]) * invDet
	inv[3] = (m[5]*m[6] - m[3]*m[8]) * invDet
	inv[4] = (m[0]*m[8] - m[2]*m[6]) * invDet
	inv[5] = (m[2]*m[3] - m[0]*m[5]) * invDet
	inv[6] = (m[3]*m[7] - m[4]*m[6]) * invDet
	inv[7] = (m[1]*m[6] - m[0]*m[7]) * invDet
	inv[8] = (m[0]*m[4] - m[1]*m[3]) * invDet

	return inv
}

// GetColorMatrix 获取色彩矩阵 (RAW -> XYZ)
func (f *File) GetColorMatrix(wb string) ([]float64, bool) {
	// 获取增益
	gain, ok := f.GetWhiteBalanceGain(wb)
	if !ok {
		return nil, false
	}

	// 获取 bmt_to_xyz
	bmtToXYZ, ok := f.GetBMTToXYZ(wb)
	if !ok {
		return nil, false
	}

	// 创建增益对角矩阵
	gainMat := make([]float64, 9)
	gainMat[0] = gain[0]
	gainMat[4] = gain[1]
	gainMat[8] = gain[2]

	// raw_to_xyz = bmt_to_xyz * gain_mat
	rawToXYZ := multiply3x3(bmtToXYZ, gainMat)

	return rawToXYZ, true
}

// GetCAMFUint32Vector 获取无符号整数向量
func (f *File) GetCAMFUint32Vector(name string, expectedSize uint32) ([]uint32, bool) {
	if f.CAMFSection == nil {
		return nil, false
	}

	for _, entry := range f.CAMFSection.Entries {
		if entry.Name == name && entry.ID == CMbM {
			if expectedSize > 0 && entry.MatrixElements != expectedSize {
				continue
			}
			if uintData, ok := entry.MatrixDecoded.([]uint32); ok {
				return uintData, true
			}
		}
	}

	return nil, false
}

// GetActiveImageArea 获取活动图像区域 [x0, y0, x1, y1]
// x0, y0 是左上角坐标，x1, y1 是右下角坐标（inclusive）
func (f *File) GetActiveImageArea() (x0, y0, x1, y1 uint32, ok bool) {
	if f.CAMFSection == nil {
		debug("GetActiveImageArea: CAMF section is nil")
		return 0, 0, 0, 0, false
	}

	debug("GetActiveImageArea: searching in %d entries", len(f.CAMFSection.Entries))
	for _, entry := range f.CAMFSection.Entries {
		if entry.Name == "ActiveImageArea" {
			debug("GetActiveImageArea: found entry, ID=0x%08x, MatrixElements=%d", entry.ID, entry.MatrixElements)
			if entry.ID == CMbM {
				debug("GetActiveImageArea: MatrixDecoded type: %T", entry.MatrixDecoded)
			}
		}
	}

	area, found := f.GetCAMFUint32Vector("ActiveImageArea", 4)
	if !found || len(area) != 4 {
		debug("GetActiveImageArea: not found or wrong size, found=%v, len=%d", found, len(area))
		return 0, 0, 0, 0, false
	}
	debug("GetActiveImageArea: found [%d, %d, %d, %d]", area[0], area[1], area[2], area[3])
	return area[0], area[1], area[2], area[3], true
}

// decodeCAMFType2 解码 Type 2 CAMF 数据（XOR 解密）
func (f *File) decodeCAMFType2(encodedData []byte, fullData []byte) ([]byte, error) {
	// 从偏移 24-27 读取 crypt_key (对应 SEC 头部的 row_stride 位置)
	if len(fullData) < 28 {
		return nil, fmt.Errorf("data too short for type 2")
	}
	cryptKey := binary.LittleEndian.Uint32(fullData[24:28])

	decoded := make([]byte, len(encodedData))
	key := cryptKey

	for i := 0; i < len(encodedData); i++ {
		old := encodedData[i]
		key = (key*1597 + 51749) % 244944
		tmp := uint32((int64(key) * 301593171) >> 24)
		new := old ^ uint8((((key<<8)-tmp)>>1+tmp)>>17)
		decoded[i] = new
	}

	return decoded, nil
}

// decodeCAMFType4 解码 Type 4 CAMF 数据（Huffman）
func (f *File) decodeCAMFType4(encodedData []byte, fullData []byte) ([]byte, error) {
	// 1. 读取 Huffman 表
	elements := []TRUEHuffmanElement{}
	offset := 0
	for offset < len(encodedData) && encodedData[offset] != 0 {
		if offset+1 >= len(encodedData) {
			break
		}
		element := TRUEHuffmanElement{
			CodeSize: encodedData[offset],
			Code:     encodedData[offset+1],
		}
		elements = append(elements, element)
		offset += 2
	}

	debug("decodeCAMFType4: read %d huffman elements", len(elements))

	// 2. 构建 Huffman 树
	tree := NewHuffmanTree(8)
	PopulateTRUEHuffmanTree(tree, elements)

	// 3. 从绝对偏移 28 读取 decoding_size（相对于 encodedData 开始）
	if 32 > len(encodedData) {
		return nil, fmt.Errorf("data too short for decoding_size")
	}
	decodingSize := binary.LittleEndian.Uint32(encodedData[28:32])
	debug("decodeCAMFType4: decodingSize=%d", decodingSize)

	// 4. 从绝对偏移 32 开始是 Huffman 编码数据
	huffmanStart := 32
	if huffmanStart >= len(encodedData) {
		return nil, fmt.Errorf("no huffman data")
	}

	// 5. 读取参数
	decodedDataSize := binary.LittleEndian.Uint32(fullData[12:16])
	decodeBias := binary.LittleEndian.Uint32(fullData[16:20])
	blockSize := binary.LittleEndian.Uint32(fullData[20:24])
	blockCount := binary.LittleEndian.Uint32(fullData[24:28])

	debug("decodeCAMFType4: decodedDataSize=%d, decodeBias=%d, blockSize=%d, blockCount=%d",
		decodedDataSize, decodeBias, blockSize, blockCount)

	// 6. 解码
	return f.camfDecodeType4Data(encodedData[huffmanStart:], tree, decodedDataSize, decodeBias, blockSize, blockCount)
}

// decodeCAMFType5 解码 Type 5 CAMF 数据
func (f *File) decodeCAMFType5(encodedData []byte, fullData []byte) ([]byte, error) {
	// Type 5 与 Type 4 类似，但解码逻辑更简单
	// 1. 读取 Huffman 表
	elements := []TRUEHuffmanElement{}
	offset := 0
	for offset < len(encodedData) && encodedData[offset] != 0 {
		if offset+1 >= len(encodedData) {
			break
		}
		element := TRUEHuffmanElement{
			CodeSize: encodedData[offset],
			Code:     encodedData[offset+1],
		}
		elements = append(elements, element)
		offset += 2
	}

	// 2. 构建 Huffman 树
	tree := NewHuffmanTree(8)
	PopulateTRUEHuffmanTree(tree, elements)

	// 3. 读取参数
	decodedDataSize := binary.LittleEndian.Uint32(fullData[12:16])
	decodeBias := binary.LittleEndian.Uint32(fullData[16:20])

	// 4. 从绝对偏移 32 开始是 Huffman 编码数据
	huffmanStart := 32
	if huffmanStart >= len(encodedData) {
		return nil, fmt.Errorf("no huffman data")
	}

	// 5. 解码（简单的累加模式）
	return f.camfDecodeType5Data(encodedData[huffmanStart:], tree, decodedDataSize, int32(decodeBias))
}

// camfDecodeType4Data 执行 Type 4 的实际解码
func (f *File) camfDecodeType4Data(huffmanData []byte, tree *HuffmanTree, decodedSize, seed, blockSize, blockCount uint32) ([]byte, error) {
	decoded := make([]byte, decodedSize)
	bs := &BitState{}
	SetBitState(bs, huffmanData)

	rowStartAcc := [2][2]int32{{int32(seed), int32(seed)}, {int32(seed), int32(seed)}}

	dstIdx := 0
	oddDst := false

	for row := uint32(0); row < blockCount && dstIdx < len(decoded); row++ {
		oddRow := (row & 1) == 1
		colAcc := [2]int32{0, 0}

		for col := uint32(0); col < blockSize && dstIdx < len(decoded); col++ {
			oddCol := (col & 1) == 1

			diff := GetTRUEDiff(bs, tree)

			var prev int32
			if col < 2 {
				if oddRow {
					if oddCol {
						prev = rowStartAcc[1][1]
					} else {
						prev = rowStartAcc[1][0]
					}
				} else {
					if oddCol {
						prev = rowStartAcc[0][1]
					} else {
						prev = rowStartAcc[0][0]
					}
				}
			} else {
				if oddCol {
					prev = colAcc[1]
				} else {
					prev = colAcc[0]
				}
			}

			value := prev + diff

			if oddCol {
				colAcc[1] = value
			} else {
				colAcc[0] = value
			}

			if col < 2 {
				if oddRow {
					if oddCol {
						rowStartAcc[1][1] = value
					} else {
						rowStartAcc[1][0] = value
					}
				} else {
					if oddCol {
						rowStartAcc[0][1] = value
					} else {
						rowStartAcc[0][0] = value
					}
				}
			}

			// 写入解码数据（12-bit 值打包到字节）
			if !oddDst {
				decoded[dstIdx] = uint8((value >> 4) & 0xff)
				dstIdx++
				if dstIdx >= len(decoded) {
					break
				}
				decoded[dstIdx] = uint8((value << 4) & 0xf0)
			} else {
				decoded[dstIdx] |= uint8((value >> 8) & 0x0f)
				dstIdx++
				if dstIdx >= len(decoded) {
					break
				}
				decoded[dstIdx] = uint8(value & 0xff)
				dstIdx++
				if dstIdx >= len(decoded) {
					break
				}
			}

			oddDst = !oddDst
		}
	}

	return decoded, nil
}

// camfDecodeType5Data 执行 Type 5 的实际解码
func (f *File) camfDecodeType5Data(huffmanData []byte, tree *HuffmanTree, decodedSize uint32, seed int32) ([]byte, error) {
	decoded := make([]byte, decodedSize)
	bs := &BitState{}
	SetBitState(bs, huffmanData)

	acc := seed
	for i := uint32(0); i < decodedSize; i++ {
		diff := GetTRUEDiff(bs, tree)
		acc = acc + diff
		decoded[i] = uint8(acc & 0xff)
	}

	return decoded, nil
}

// GetColorMatrix1ForDNG 获取 DNG ColorMatrix1 (XYZ to sRGB)
// 注意: 在 Sigma X3F 实现中，ColorMatrix1 是固定的 XYZ_to_sRGB 标准矩阵
// 不依赖于相机或白平衡设置
func GetColorMatrix1ForDNG() []float64 {
	// XYZ_to_sRGB (D65) 标准矩阵（高精度）
	// 使用 sRGB 标准的完整精度值
	return []float64{
		3.2404542, -1.5371385, -0.4985314,
		-0.9692660, 1.8760108, 0.0415560,
		0.0556434, -0.2040259, 1.0572252,
	}
}

// GetCameraCalibration1ForDNG 获取 DNG CameraCalibration1 (对角矩阵)
// 包含白平衡增益信息，简单的增益倒数
func GetCameraCalibration1ForDNG(gain [3]float64) []float64 {
	// 对角矩阵：1/gain
	return []float64{
		1.0 / gain[0], 0, 0,
		0, 1.0 / gain[1], 0,
		0, 0, 1.0 / gain[2],
	}
}

// GetD65ToD50Matrix 获取 Bradford 色适应矩阵 (D65 → D50)
func GetD65ToD50Matrix() []float64 {
	// Bradford chromatic adaptation matrix from D65 to D50
	return []float64{
		1.0478112, 0.0228866, -0.0501270,
		0.0295424, 0.9904844, -0.0170491,
		-0.0092345, 0.0150436, 0.7521316,
	}
}

// GetForwardMatrix1ForDNG 获取 DNG ForwardMatrix1
// ForwardMatrix1 = D65_to_D50 × bmt_to_xyz
// 其中 bmt_to_xyz = sRGB_to_XYZ × ColorCorrections
func (f *File) GetForwardMatrix1ForDNG(wb string) ([]float64, bool) {
	bmtToXYZ, ok := f.GetBMTToXYZ(wb)
	if !ok {
		return nil, false
	}

	d65ToD50 := GetD65ToD50Matrix()
	forwardMatrix := multiply3x3(d65ToD50, bmtToXYZ)

	return forwardMatrix, true
}

// GetBMTToXYZ 获取 BMT 到 XYZ 的转换矩阵
func (f *File) GetBMTToXYZ(wb string) ([]float64, bool) {
	// sRGB -> XYZ 标准矩阵
	sRGBToXYZ := GetSRGBToXYZMatrix()

	// 尝试从 WhiteBalanceColorCorrections 获取
	if ccMatrix, ok := f.GetCAMFMatrixForWB("WhiteBalanceColorCorrections", wb, []uint32{3, 3}); ok {
		return multiply3x3(sRGBToXYZ, ccMatrix), true
	}

	if ccMatrix, ok := f.GetCAMFMatrixForWB("DP1_WhiteBalanceColorCorrections", wb, []uint32{3, 3}); ok {
		return multiply3x3(sRGBToXYZ, ccMatrix), true
	}

	// 旧式方法: WhiteBalanceIlluminants + WhiteBalanceCorrections
	if camToXYZ, ok1 := f.GetCAMFMatrixForWB("WhiteBalanceIlluminants", wb, []uint32{3, 3}); ok1 {
		if wbCorrection, ok2 := f.GetCAMFMatrixForWB("WhiteBalanceCorrections", wb, []uint32{3, 3}); ok2 {
			// raw_to_xyz = wb_correction * cam_to_xyz
			rawToXYZ := multiply3x3(wbCorrection, camToXYZ)

			// raw_neutral
			rawNeutral := getRawNeutral(rawToXYZ)

			// raw_neutral_mat (对角矩阵)
			rawNeutralMat := make([]float64, 9)
			rawNeutralMat[0] = rawNeutral[0]
			rawNeutralMat[4] = rawNeutral[1]
			rawNeutralMat[8] = rawNeutral[2]

			// bmt_to_xyz = raw_to_xyz * raw_neutral_mat
			return multiply3x3(rawToXYZ, rawNeutralMat), true
		}
	}

	return nil, false
}

// GetCAMFRect 获取 CAMF 中定义的矩形区域
func (f *File) GetCAMFRect(name string) (x0, y0, x1, y1 uint32, ok bool) {
	if f.CAMFSection == nil {
		return 0, 0, 0, 0, false
	}

	for _, entry := range f.CAMFSection.Entries {
		if entry.Name == name && entry.ID == CMbM {
			// 应该是一个 4 元素的 uint32 矩阵 [x0, y0, x1, y1]
			if entry.MatrixElements != 4 {
				return 0, 0, 0, 0, false
			}

			// 读取数据
			if len(entry.MatrixData) < 16 {
				return 0, 0, 0, 0, false
			}

			var coords [4]uint32
			for i := 0; i < 4; i++ {
				coords[i] = binary.LittleEndian.Uint32(entry.MatrixData[i*4 : (i+1)*4])
			}

			return coords[0], coords[1], coords[2], coords[3], true
		}
	}

	return 0, 0, 0, 0, false
}

// GetCAMFRectScaled 获取 CAMF 矩形区域，并根据图像实际尺寸进行缩放
// rescale=true 时，坐标会从 KeepImageArea 的分辨率缩放到 imageWidth x imageHeight
func (f *File) GetCAMFRectScaled(name string, imageWidth, imageHeight uint32, rescale bool) (x0, y0, x1, y1 uint32, ok bool) {
	// 获取原始 rect
	x0, y0, x1, y1, ok = f.GetCAMFRect(name)
	if !ok {
		return 0, 0, 0, 0, false
	}

	// 获取 KeepImageArea
	keepX0, keepY0, keepX1, keepY1, keepOk := f.GetCAMFRect("KeepImageArea")
	if !keepOk {
		return 0, 0, 0, 0, false
	}

	keepCols := keepX1 - keepX0 + 1
	keepRows := keepY1 - keepY0 + 1

	// 检查 rect 是否在 KeepImageArea 范围内
	if x0 > keepX1 || y0 > keepY1 || x1 < keepX0 || y1 < keepY0 {
		return 0, 0, 0, 0, false
	}

	// 调整 rect 到 KeepImageArea 的相对坐标
	if x0 < keepX0 {
		x0 = keepX0
	}
	if y0 < keepY0 {
		y0 = keepY0
	}
	if x1 > keepX1 {
		x1 = keepX1
	}
	if y1 > keepY1 {
		y1 = keepY1
	}

	// 转换为相对于 KeepImageArea 的坐标
	x0 -= keepX0
	y0 -= keepY0
	x1 -= keepX0
	y1 -= keepY0

	// 如果需要缩放，根据实际图像尺寸和 KeepImageArea 的比例进行缩放
	if rescale {
		x0 = x0 * imageWidth / keepCols
		y0 = y0 * imageHeight / keepRows
		x1 = x1 * imageWidth / keepCols
		y1 = y1 * imageHeight / keepRows
	}

	return x0, y0, x1, y1, true
}

// SpatialGainCorr Spatial Gain 校正数据
type SpatialGainCorr struct {
	Gain     []float32 // Gain 数据（rows × cols × channels）
	Rows     int       // 行数
	Cols     int       // 列数
	Channels int       // 通道数
	RowOff   int       // 行偏移
	ColOff   int       // 列偏移
}

// GetSpatialGain 获取 Spatial Gain 数据用于 DNG Opcode List 2
func (f *File) GetSpatialGain(wb string) []SpatialGainCorr {
	var result []SpatialGainCorr

	// 1. 尝试 Merrill 类型 Spatial Gain
	if merrillGains := f.getMerrillTypeSpatialGain(); len(merrillGains) > 0 {
		return merrillGains
	}

	// 2. 尝试从 SpatialGainTables 获取（基于白平衡）
	if propName, ok := f.GetCAMFProperty("SpatialGainTables", wb); ok {
		if data, dims, ok := f.GetCAMFMatrix(propName); ok {
			if float64Data, ok := data.([]float64); ok && len(dims) == 3 {
				corr := convertSpatialGain(float64Data, dims)
				if corr != nil {
					result = append(result, *corr)
					return result
				}
			} else if float32Data, ok := data.([]float32); ok && len(dims) == 3 {
				result = append(result, SpatialGainCorr{
					Gain:     float32Data,
					Rows:     int(dims[0]),
					Cols:     int(dims[1]),
					Channels: int(dims[2]),
					RowOff:   0,
					ColOff:   0,
				})
				return result
			}
		}
	}

	// 3. 尝试从 SpatialGain 直接获取
	if data, dims, ok := f.GetCAMFMatrix("SpatialGain"); ok {
		if float64Data, ok := data.([]float64); ok && len(dims) == 3 {
			corr := convertSpatialGain(float64Data, dims)
			if corr != nil {
				result = append(result, *corr)
			}
		} else if float32Data, ok := data.([]float32); ok && len(dims) == 3 {
			result = append(result, SpatialGainCorr{
				Gain:     float32Data,
				Rows:     int(dims[0]),
				Cols:     int(dims[1]),
				Channels: int(dims[2]),
				RowOff:   0,
				ColOff:   0,
			})
		}
	}

	return result
}

// merrillGainBlock 表示一个 Merrill Spatial Gain 数据块
type merrillGainBlock struct {
	name     string
	x        float64 // 1/aperture
	y        float64 // lens position
	minGains [3]float64
	deltas   [3]float64
	gains    [3][]uint32 // 压缩的 gain 数据
	rows     [3]int      // 每个通道可能有不同的尺寸
	cols     [3]int
}

// getMerrillTypeSpatialGain 获取 Merrill 类型相机的 Spatial Gain（使用四点双线性插值）
func (f *File) getMerrillTypeSpatialGain() []SpatialGainCorr {
	// 1. 获取光圈值
	captureAperture := 0.0
	foundAperture := false

	if aperture, ok := f.GetCAMFFloat("CaptureAperture"); ok {
		captureAperture = aperture
		foundAperture = true
	} else if f.Properties != nil {
		for _, prop := range f.Properties.Properties {
			if prop.Name == "APERTURE" || prop.Name == "Aperture" {
				fmt.Sscanf(prop.Value, "%f", &captureAperture)
				foundAperture = true
				break
			}
		}
	}

	if !foundAperture {
		return nil
	}

	// 2. 获取对焦距离
	objectDistance := math.Inf(1)
	if dist, ok := f.GetCAMFFloat("ObjectDistance"); ok {
		objectDistance = dist * 10.0 // 转换 cm 到 mm
	}

	// 3. 获取焦距
	focalLength := 30.0 // 默认值
	if f.Properties != nil {
		for _, prop := range f.Properties.Properties {
			if prop.Name == "FLENGTH" {
				fmt.Sscanf(prop.Value, "%f", &focalLength)
				break
			}
		}
	}

	// 4. 获取 MOD (Minimum Object Distance)
	mod := 280.0 // 默认值 (DP2 Merrill)
	if lensInfo, ok := f.GetCAMFInt32("LensInformation"); ok {
		switch lensInfo {
		case 1003:
			mod = 200.0 // DP1 Merrill
		case 1004:
			mod = 280.0 // DP2 Merrill
		case 1005:
			mod = 226.0 // DP3 Merrill
		}
	}

	// 5. 计算镜头位置
	lensPosition := func(focal, objDist float64) float64 {
		if math.IsInf(objDist, 1) {
			return focal
		}
		return 1.0 / (1.0/focal - 1.0/objDist)
	}

	targetX := 1.0 / captureAperture
	targetY := lensPosition(focalLength, objectDistance)

	// 6. 获取光圈列表
	fstopData, fstopDims, hasFstop := f.GetCAMFMatrix("SpatialGain_Fstop")
	var fstops []float64
	if hasFstop && len(fstopDims) == 1 {
		if floatData, ok := fstopData.([]float64); ok {
			fstops = floatData
		}
	}

	// 7. 收集所有可用的 Spatial Gain blocks
	var blocks []*merrillGainBlock

	if f.CAMFSection != nil {
		for _, entry := range f.CAMFSection.Entries {
			if entry.ID != CMbP {
				continue
			}

			var block merrillGainBlock
			block.name = entry.Name

			// 解析 block 名称
			var apertureIndex int
			var distance string
			n, _ := fmt.Sscanf(entry.Name, "SpatialGainsProps_%d_%s", &apertureIndex, &distance)

			if n == 2 && fstops != nil && apertureIndex >= 0 && apertureIndex < len(fstops) {
				// 格式 1: SpatialGainsProps_<index>_<distance>
				aperture := fstops[apertureIndex]
				block.x = 1.0 / aperture

				if distance == "INF" {
					block.y = lensPosition(focalLength, math.Inf(1))
				} else if distance == "MOD" {
					block.y = lensPosition(focalLength, mod)
				} else {
					continue
				}
			} else {
				// 格式 2: SpatialGainsProps_<aperture>_<lenspos>
				var aperture, lensPos float64
				n, _ = fmt.Sscanf(entry.Name, "SpatialGainsProps_%f_%f", &aperture, &lensPos)
				if n != 2 {
					continue
				}
				block.x = 1.0 / aperture
				block.y = lensPos
			}

			// 读取三个通道的数据
			channels := []string{"R", "G", "B"}
			validBlock := true

			for i, ch := range channels {
				gainsTableKey, ok := f.GetCAMFProperty(entry.Name, "GainsTable"+ch)
				if !ok {
					validBlock = false
					break
				}

				gainsData, dims, ok := f.GetCAMFMatrix(gainsTableKey)
				if !ok || len(dims) != 2 {
					validBlock = false
					break
				}

				minGainsStr, ok := f.GetCAMFProperty(entry.Name, "MinGains"+ch)
				if !ok {
					validBlock = false
					break
				}
				block.minGains[i], _ = strconv.ParseFloat(minGainsStr, 64)

				deltaStr, ok := f.GetCAMFProperty(entry.Name, "Delta"+ch)
				if !ok {
					validBlock = false
					break
				}
				block.deltas[i], _ = strconv.ParseFloat(deltaStr, 64)

				uint32Data, ok := gainsData.([]uint32)
				if !ok {
					validBlock = false
					break
				}
				block.gains[i] = uint32Data
				block.rows[i] = int(dims[0])
				block.cols[i] = int(dims[1])
			}

			if validBlock {
				blocks = append(blocks, &block)
			}
		}
	}

	if len(blocks) == 0 {
		return nil
	}

	// 8. 找到四个象限中最接近的 blocks（双线性插值）
	type closestInfo struct {
		block *merrillGainBlock
		dx    float64
		dy    float64
		d2    float64
	}

	qClosest := [4]*closestInfo{
		{d2: math.Inf(1)},
		{d2: math.Inf(1)},
		{d2: math.Inf(1)},
		{d2: math.Inf(1)},
	}

	for _, block := range blocks {
		dx := block.x - targetX
		dy := block.y - targetY
		d2 := dx*dx + dy*dy

		// 确定象限
		var q int
		if dx > 0.0 && dy > 0.0 {
			q = 0 // 右上
		} else if dx > 0.0 {
			q = 3 // 右下
		} else if dy > 0.0 {
			q = 1 // 左上
		} else {
			q = 2 // 左下
		}

		if d2 < qClosest[q].d2 {
			qClosest[q] = &closestInfo{
				block: block,
				dx:    dx,
				dy:    dy,
				d2:    d2,
			}
		}
	}

	// 9. 计算插值权重
	qWeightX := [4]float64{1.0, 1.0, 1.0, 1.0}
	qWeightY := [4]float64{1.0, 1.0, 1.0, 1.0}

	if qClosest[0].block != nil && qClosest[1].block != nil {
		dx0 := qClosest[0].dx
		dx1 := qClosest[1].dx
		if dx1-dx0 != 0 {
			qWeightX[0] = qClosest[1].dx / (qClosest[1].dx - qClosest[0].dx)
			qWeightX[1] = qClosest[0].dx / (qClosest[0].dx - qClosest[1].dx)
		}
	}

	if qClosest[2].block != nil && qClosest[3].block != nil {
		dx2 := qClosest[2].dx
		dx3 := qClosest[3].dx
		if dx3-dx2 != 0 {
			qWeightX[2] = qClosest[3].dx / (qClosest[3].dx - qClosest[2].dx)
			qWeightX[3] = qClosest[2].dx / (qClosest[2].dx - qClosest[3].dx)
		}
	}

	if qClosest[0].block != nil && qClosest[3].block != nil {
		dy0 := qClosest[0].dy
		dy3 := qClosest[3].dy
		if dy3-dy0 != 0 {
			qWeightY[0] = qClosest[3].dy / (qClosest[3].dy - qClosest[0].dy)
			qWeightY[3] = qClosest[0].dy / (qClosest[0].dy - qClosest[3].dy)
		}
	}

	if qClosest[1].block != nil && qClosest[2].block != nil {
		dy1 := qClosest[1].dy
		dy2 := qClosest[2].dy
		if dy2-dy1 != 0 {
			qWeightY[1] = qClosest[2].dy / (qClosest[2].dy - qClosest[1].dy)
			qWeightY[2] = qClosest[1].dy / (qClosest[1].dy - qClosest[2].dy)
		}
	}

	qWeight := [4]float64{}
	for i := 0; i < 4; i++ {
		if math.IsNaN(qWeightX[i]) {
			qWeightX[i] = 1.0
		}
		if math.IsNaN(qWeightY[i]) {
			qWeightY[i] = 1.0
		}
		qWeight[i] = qWeightX[i] * qWeightY[i]
	}

	// 10. 检查至少有一个有效的 block
	hasValidBlock := false
	for i := 0; i < 4; i++ {
		if qClosest[i].block != nil {
			hasValidBlock = true
			break
		}
	}
	if !hasValidBlock {
		return nil
	}

	// 11. 对每个通道进行加权插值（每个通道有自己的尺寸）
	var result []SpatialGainCorr

	for ch := 0; ch < 3; ch++ {
		// 获取当前通道的尺寸（从第一个有效 block）
		var rows, cols int
		for q := 0; q < 4; q++ {
			if qClosest[q].block != nil {
				rows = qClosest[q].block.rows[ch]
				cols = qClosest[q].block.cols[ch]
				break
			}
		}

		if rows == 0 || cols == 0 {
			continue
		}

		numPixels := rows * cols
		finalGain := make([]float32, numPixels)

		for j := 0; j < numPixels; j++ {
			gain := 0.0
			for q := 0; q < 4; q++ {
				if qClosest[q].block != nil {
					block := qClosest[q].block
					if j < len(block.gains[ch]) {
						compressed := float64(block.gains[ch][j])
						gain += qWeight[q] * (block.minGains[ch] + block.deltas[ch]*compressed)
					}
				}
			}
			finalGain[j] = float32(gain)
		}

		result = append(result, SpatialGainCorr{
			Gain:     finalGain,
			Rows:     rows,
			Cols:     cols,
			Channels: 1,
			RowOff:   0,
			ColOff:   0,
		})
	}

	return result
}

// convertSpatialGain 将 float64 数据转换为 float32
func convertSpatialGain(data []float64, dims []uint32) *SpatialGainCorr {
	if len(dims) != 3 {
		return nil
	}

	gain := make([]float32, len(data))
	for i, v := range data {
		gain[i] = float32(v)
	}

	return &SpatialGainCorr{
		Gain:     gain,
		Rows:     int(dims[0]),
		Cols:     int(dims[1]),
		Channels: int(dims[2]),
		RowOff:   0,
		ColOff:   0,
	}
}

// GetSRGBToXYZMatrix 获取 sRGB 到 XYZ 的标准矩阵
// 使用 sRGB 标准的完整精度值
func GetSRGBToXYZMatrix() []float64 {
	return []float64{
		0.4124564, 0.3575761, 0.1804375,
		0.2126729, 0.7151522, 0.0721750,
		0.0193339, 0.1191920, 0.9503041,
	}
}

// GetRawToXYZ 获取 raw_to_xyz 矩阵 (包含白平衡增益)
// C 代码: x3f_get_raw_to_xyz = bmt_to_xyz × diag(gain)
func (f *File) GetRawToXYZ(wb string) ([]float64, bool) {
	// 获取 bmt_to_xyz
	bmtToXYZ, ok := f.GetBMTToXYZ(wb)
	if !ok {
		return nil, false
	}

	// 获取白平衡增益
	gain, ok := f.GetWhiteBalanceGain(wb)
	if !ok {
		return nil, false
	}

	// 构造增益对角矩阵
	gainMat := []float64{
		gain[0], 0, 0,
		0, gain[1], 0,
		0, 0, gain[2],
	}

	// raw_to_xyz = bmt_to_xyz × gain_mat
	return multiply3x3(bmtToXYZ, gainMat), true
}

// GetForwardMatrixWithSRGB 获取基于 sRGB 标准矩阵的 ForwardMatrix1
// 用于 "Unconverted" 和 "Linear sRGB" profiles
func GetForwardMatrixWithSRGB() []float64 {
	sRGBToXYZ := GetSRGBToXYZMatrix()
	d65ToD50 := GetD65ToD50Matrix()
	return multiply3x3(d65ToD50, sRGBToXYZ)
}

// GetForwardMatrixGrayscale 获取灰度模式的 ForwardMatrix1
// grayscaleMix: [R, G, B] 权重数组，例如 [1/3, 1/3, 1/3] 或 [2, -1, 0]
func GetForwardMatrixGrayscale(grayscaleMix [3]float64) []float64 {
	// D50 白点 XYZ 值
	d50XYZ := [3]float64{0.96422, 1.00000, 0.82521}

	// 创建对角矩阵 diag(grayscaleMix)
	grayscaleMixMat := [9]float64{
		grayscaleMix[0], 0, 0,
		0, grayscaleMix[1], 0,
		0, 0, grayscaleMix[2],
	}

	// 创建全 1 矩阵
	ones := [9]float64{
		1, 1, 1,
		1, 1, 1,
		1, 1, 1,
	}

	// bmt_to_grayscale = ones × grayscale_mix_mat
	bmtToGrayscale := [9]float64{}
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			sum := 0.0
			for k := 0; k < 3; k++ {
				sum += ones[i*3+k] * grayscaleMixMat[k*3+j]
			}
			bmtToGrayscale[i*3+j] = sum
		}
	}

	// 创建对角矩阵 diag(d50_xyz)
	d50XYZMat := [9]float64{
		d50XYZ[0], 0, 0,
		0, d50XYZ[1], 0,
		0, 0, d50XYZ[2],
	}

	// bmt_to_d50 = d50_xyz_mat × bmt_to_grayscale
	result := make([]float64, 9)
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			sum := 0.0
			for k := 0; k < 3; k++ {
				sum += d50XYZMat[i*3+k] * bmtToGrayscale[k*3+j]
			}
			result[i*3+j] = sum
		}
	}

	return result
}
