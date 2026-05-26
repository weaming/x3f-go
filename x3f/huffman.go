package x3f

import (
	"encoding/binary"
	"fmt"
)

// HuffmanNode Huffman 树节点
type HuffmanNode struct {
	Branch [2]*HuffmanNode
	Leaf   uint32
}

// HuffmanTree Huffman 树
type HuffmanTree struct {
	Nodes         []HuffmanNode
	FreeNodeIndex int
}

// TRUEHuffmanElement TRUE 引擎 Huffman 表元素
type TRUEHuffmanElement struct {
	CodeSize uint8
	Code     uint8
}

// BitState 位读取状态
type BitState struct {
	Data      []byte
	BytePos   int
	BitOffset uint8
	Bits      [8]uint8
}

// 创建新的 Huffman 树
func NewHuffmanTree(bits int) *HuffmanTree {
	leaves := 1 << bits
	maxNodes := (2 * leaves) - 1

	tree := &HuffmanTree{
		Nodes:         make([]HuffmanNode, maxNodes),
		FreeNodeIndex: 0,
	}

	// 初始化所有节点
	for i := range tree.Nodes {
		tree.Nodes[i].Leaf = UndefinedLeaf
	}

	return tree
}

// 创建新节点
func (tree *HuffmanTree) newNode() *HuffmanNode {
	if tree.FreeNodeIndex >= len(tree.Nodes) {
		return nil
	}

	node := &tree.Nodes[tree.FreeNodeIndex]
	node.Branch[0] = nil
	node.Branch[1] = nil
	node.Leaf = UndefinedLeaf

	tree.FreeNodeIndex++
	return node
}

// 将编码添加到树
func (tree *HuffmanTree) AddCodeToTree(length int, code uint32, value uint32) {
	if tree.FreeNodeIndex == 0 {
		tree.newNode() // 创建根节点
	}

	node := &tree.Nodes[0]

	for i := 0; i < length; i++ {
		// 从最高位开始读取
		pos := length - 1 - i
		bit := (code >> pos) & 1

		if node.Branch[bit] == nil {
			node.Branch[bit] = tree.newNode()
		}

		node = node.Branch[bit]
		if node == nil {
			return
		}
	}

	node.Leaf = value
}

// 填充 TRUE 引擎 Huffman 树
func PopulateTRUEHuffmanTree(tree *HuffmanTree, table []TRUEHuffmanElement) {
	tree.newNode() // 创建根节点

	for i, element := range table {
		length := int(element.CodeSize)

		if length != 0 && length <= 8 {
			// 右对齐编码
			code := uint32(element.Code>>(8-length)) & 0xff
			value := uint32(i)

			tree.AddCodeToTree(length, code, value)
		}
	}
}

// 填充传统 Huffman 树
func PopulateHuffmanTree(tree *HuffmanTree, table []uint32, mapping []uint16) {
	tree.newNode() // 创建根节点

	for i, element := range table {
		if element != 0 {
			length := int((element >> 27) & 0x1f)
			code := element & 0x07ffffff

			var value uint32
			if len(mapping) == len(table) {
				value = uint32(mapping[i])
			} else {
				value = uint32(i)
			}

			tree.AddCodeToTree(length, code, value)
		}
	}
}

// 设置位状态
func SetBitState(bs *BitState, data []byte) {
	bs.Data = data
	bs.BytePos = 0
	bs.BitOffset = 8
}

// 读取一位
func GetBit(bs *BitState) uint8 {
	if bs.BitOffset == 8 {
		if bs.BytePos >= len(bs.Data) {
			return 0
		}

		byteVal := bs.Data[bs.BytePos]

		// 将字节分解为位（从高位到低位）
		for i := 7; i >= 0; i-- {
			bs.Bits[i] = byteVal & 1
			byteVal = byteVal >> 1
		}

		bs.BytePos++
		bs.BitOffset = 0
	}

	bit := bs.Bits[bs.BitOffset]
	bs.BitOffset++
	return bit
}

// 使用 Huffman 树解码差值
func GetHuffmanDiff(bs *BitState, tree *HuffmanTree) int32 {
	node := &tree.Nodes[0]

	for node.Branch[0] != nil || node.Branch[1] != nil {
		bit := GetBit(bs)
		node = node.Branch[bit]

		if node == nil {
			return 0
		}
	}

	return int32(node.Leaf)
}

// 使用 TRUE 算法解码差值
func GetTRUEDiff(bs *BitState, tree *HuffmanTree) int32 {
	node := &tree.Nodes[0]

	for node.Branch[0] != nil || node.Branch[1] != nil {
		bit := GetBit(bs)
		node = node.Branch[bit]

		if node == nil {
			return 0
		}
	}

	bits := uint8(node.Leaf)

	if bits == 0 {
		return 0
	}

	firstBit := GetBit(bs)
	diff := int32(firstBit)

	for i := 1; i < int(bits); i++ {
		diff = (diff << 1) + int32(GetBit(bs))
	}

	if firstBit == 0 {
		diff -= (1 << bits) - 1
	}

	return diff
}

// 解码一行 Huffman 数据
func HuffmanDecodeRow(data []byte, rowOffset uint32, columns int, tree *HuffmanTree, offset int16, minimum *int16) []uint16 {
	result := make([]uint16, columns*3)

	c := [3]int16{offset, offset, offset}

	bs := &BitState{}
	SetBitState(bs, data[rowOffset:])

	for col := 0; col < columns; col++ {
		for color := 0; color < 3; color++ {
			diff := GetHuffmanDiff(bs, tree)
			c[color] += int16(diff)

			var cFix uint16
			if c[color] < 0 {
				cFix = 0
				if minimum != nil && c[color] < *minimum {
					*minimum = c[color]
				}
			} else {
				cFix = uint16(c[color])
			}

			result[3*col+color] = cFix
		}
	}

	return result
}

// 使用 TRUE 算法解码一个颜色平面
func TRUEDecodeOneColor(data []byte, rows, columns int, tree *HuffmanTree, seed uint32) []uint16 {
	result := make([]uint16, rows*columns)

	bs := &BitState{}
	SetBitState(bs, data)

	// 行起始累加器 [2][2]，用 seed 初始化
	seedInt32 := int32(seed)
	rowStartAcc := [2][2]int32{
		{seedInt32, seedInt32},
		{seedInt32, seedInt32},
	}

	for row := 0; row < rows; row++ {
		// 列累加器 [2]，每行重置
		colAcc := [2]int32{}

		for col := 0; col < columns; col++ {
			rowMod := row & 1
			colMod := col & 1

			diff := GetTRUEDiff(bs, tree)

			// 计算前一个值（来自行起始或列累加器）
			var prev int32
			if col < 2 {
				prev = rowStartAcc[rowMod][colMod]
			} else {
				prev = colAcc[colMod]
			}

			// 计算当前值
			value := prev + diff

			// 更新累加器
			colAcc[colMod] = value
			if col < 2 {
				rowStartAcc[rowMod][colMod] = value
			}

			if value < 0 {
				result[row*columns+col] = 0
			} else {
				result[row*columns+col] = uint16(value)
			}
		}
	}

	return result
}

// storeTRUEPlaneData 将单通道 TRUE 解码结果写入交错三通道缓冲。
func storeTRUEPlaneData(decodedData []uint16, colorData []uint16, planeRows, planeCols, dstRows, dstCols, color int) {
	rowsToWrite := planeRows
	if rowsToWrite > dstRows {
		rowsToWrite = dstRows
	}

	for row := 0; row < rowsToWrite; row++ {
		for col := 0; col < planeCols; col++ {
			if col >= dstCols {
				continue
			}

			srcIdx := row*planeCols + col
			dstIdx := (row*dstCols+col)*3 + color
			decodedData[dstIdx] = colorData[srcIdx]
		}
	}
}

// ImageSection 图像段数据
type QuattroPlane struct {
	Columns uint16
	Rows    uint16
}

type ImageSection struct {
	Type      uint32
	Format    uint32
	Columns   uint32
	Rows      uint32
	RowStride uint32

	// 解码后的实际尺寸（对于 Quattro 可能与 Columns/Rows 不同）
	DecodedColumns uint32
	DecodedRows    uint32

	// Huffman 数据
	HuffmanTree       *HuffmanTree
	RowOffsets        []uint32
	Data              []byte
	HuffmanMapping    []uint16
	HuffmanBits       int
	HuffmanCompressed bool

	// TRUE 引擎数据
	TRUETable      []TRUEHuffmanElement
	TRUEPlaneSizes []uint32
	TRUESeeds      [3]uint16

	// Quattro 专用数据
	QuattroPlanes  [3]QuattroPlane
	QuattroLayout  int      // 0=binned, 1=normal
	QuattroTopData []uint16 // Quattro top 层数据（单独存储）
	QuattroTopRows int      // top 层行数
	QuattroTopCols int      // top 层列数

	// 解码后的数据
	DecodedData []uint16
}

// isTRUEFormat 判断图像格式是否使用 TRUE 解码路径。
func isTRUEFormat(format uint32) bool {
	switch format {
	case ImageRAWTRUE, ImageRAWMerrill:
		return true
	case ImageRAWQuattro, ImageRAWSDQ, ImageRAWSDQH:
		return true
	}

	return isTRUEFormatType(format) || isQuattroFormat(format)
}

// isTRUEFormatType 判断是否为裸 TRUE 格式标识。
func isTRUEFormatType(format uint32) bool {
	return format&0xFF == FormatTypeTRUE
}

// isQuattroFormat 判断图像格式是否包含 Quattro 布局信息。
func isQuattroFormat(format uint32) bool {
	switch format {
	case ImageRAWQuattro, ImageRAWSDQ, ImageRAWSDQH:
		return true
	}

	switch format & 0xFF {
	case FormatTypeQuattro, FormatTypeSDQ, FormatTypeSDQH:
		return true
	default:
		return false
	}
}

// 加载图像段
func (f *File) LoadImageSection(entry *DirectoryEntry) error {
	// 对于 version >= 4.0，entry.Type 直接是 IMA2/IMAG
	// 对于 version < 4.0，entry.Type 是 SECi
	if entry.Type != SECi && entry.Type != IMA2 && entry.Type != IMAG {
		return fmt.Errorf("不是图像段")
	}

	data := make([]byte, entry.Length)
	_, err := f.reader.ReadAt(data, int64(entry.Offset))
	if err != nil {
		return fmt.Errorf("读取图像段失败: %w", err)
	}

	section := &ImageSection{}

	// 所有图像段都以 28 字节的 SECi 头部开始
	if len(data) < 28 {
		return fmt.Errorf("图像段数据太短")
	}

	// 验证 SECi 标识符
	sectionID := binary.LittleEndian.Uint32(data[0:4])
	if sectionID != SECi {
		return fmt.Errorf("无效的图像段标识符: 0x%08x", sectionID)
	}

	// 读取 SECi 头部（28 字节）
	seciVersion := binary.LittleEndian.Uint32(data[4:8])
	section.Type = binary.LittleEndian.Uint32(data[8:12])
	section.Format = binary.LittleEndian.Uint32(data[12:16]) // Data format - 这是关键字段！
	section.Columns = binary.LittleEndian.Uint32(data[16:20])
	section.Rows = binary.LittleEndian.Uint32(data[20:24])
	section.RowStride = binary.LittleEndian.Uint32(data[24:28])

	debug("SECi header: version=0x%08x, Type=0x%08x, Format=0x%08x, Cols=%d, Rows=%d, RowStride=%d",
		seciVersion, section.Type, section.Format, section.Columns, section.Rows, section.RowStride)

	// 检查是否是缩略图/预览图 (Type 的低位为 2 表示预览)
	// RAW 图像的 Type 通常是 0x00000001
	if section.Type == 0x00000002 || section.Type&0xFF == 0x02 {
		return nil
	}

	// 图像数据从 SECi 头部后开始（偏移 28）
	imageDataStart := 28
	formatID := section.Format

	// 检查 Rows 是否合理
	if section.Rows > 100000 {
		return fmt.Errorf("图像头部解析异常（Rows=%d）, Type=0x%08x, Format=0x%08x",
			section.Rows, section.Type, section.Format)
	}

	// 根据格式类型加载数据
	var loadErr error
	var isRAWImage bool

	switch {
	case formatID == ImageRAWHuffmanX530 || formatID == ImageRAWHuffman10bit:
		loadErr = loadHuffmanImage(section, data[imageDataStart:])
		isRAWImage = true
	case isTRUEFormat(formatID):
		loadErr = loadTRUEImage(section, data[imageDataStart:])
		isRAWImage = true
	default:
		// 跳过未知格式
		return nil
	}

	if loadErr != nil {
		return loadErr
	}

	// 只添加 RAW 图像到图像数据列表
	if isRAWImage {
		f.ImageData = append(f.ImageData, section)
	}

	return nil
}

// 加载传统 Huffman 图像
func loadHuffmanImage(section *ImageSection, data []byte) error {
	offset := 0
	bits := 10
	tableSize := 1 << bits

	section.HuffmanBits = bits

	if len(data) < tableSize*2 {
		return fmt.Errorf("Huffman 数据太短，无法读取映射表")
	}

	section.HuffmanMapping = make([]uint16, tableSize)
	for i := 0; i < tableSize; i++ {
		section.HuffmanMapping[i] = binary.LittleEndian.Uint16(data[offset : offset+2])
		offset += 2
	}

	if section.RowStride != 0 {
		section.HuffmanCompressed = false
		section.Data = data[offset:]
		return nil
	}

	section.HuffmanCompressed = true
	if len(data) < offset+tableSize*4+int(section.Rows)*4 {
		return fmt.Errorf("Huffman 数据太短，无法读取压缩表")
	}

	table := make([]uint32, tableSize)
	for i := 0; i < tableSize; i++ {
		table[i] = binary.LittleEndian.Uint32(data[offset : offset+4])
		offset += 4
	}

	rowOffsetsSize := int(section.Rows) * 4
	encodedDataEnd := len(data) - rowOffsetsSize
	if encodedDataEnd < offset {
		return fmt.Errorf("Huffman 行偏移表位置无效")
	}

	section.Data = data[offset:encodedDataEnd]

	section.RowOffsets = make([]uint32, section.Rows)
	for i := uint32(0); i < section.Rows; i++ {
		tableOffset := encodedDataEnd + int(i)*4
		section.RowOffsets[i] = binary.LittleEndian.Uint32(data[tableOffset : tableOffset+4])
	}

	section.HuffmanTree = NewHuffmanTree(bits)
	PopulateHuffmanTree(section.HuffmanTree, table, section.HuffmanMapping)

	return nil
}

// 加载 TRUE 引擎图像
// 加载 Quattro 平面信息
func loadQuattroPlanes(section *ImageSection, data []byte, offset int) (int, error) {
	if len(data) < offset+12 {
		return offset, fmt.Errorf("Quattro 数据太短")
	}

	for i := 0; i < 3; i++ {
		section.QuattroPlanes[i].Columns = binary.LittleEndian.Uint16(data[offset : offset+2])
		section.QuattroPlanes[i].Rows = binary.LittleEndian.Uint16(data[offset+2 : offset+4])
		offset += 4
	}

	debug("  Quattro planes: [(%d,%d), (%d,%d), (%d,%d)], offset=%d",
		section.QuattroPlanes[0].Columns, section.QuattroPlanes[0].Rows,
		section.QuattroPlanes[1].Columns, section.QuattroPlanes[1].Rows,
		section.QuattroPlanes[2].Columns, section.QuattroPlanes[2].Rows, offset)

	// 判断 Quattro 布局
	if section.QuattroPlanes[0].Rows == uint16(section.Rows/2) {
		section.QuattroLayout = 1 // Quattro layout
	} else if section.QuattroPlanes[0].Rows == uint16(section.Rows) {
		section.QuattroLayout = 0 // Binned Quattro
	} else {
		return offset, fmt.Errorf("未知的 Quattro 层大小: plane[0].rows=%d, image.rows=%d",
			section.QuattroPlanes[0].Rows, section.Rows)
	}

	return offset, nil
}

// 加载 TRUE 种子
func loadTRUESeeds(section *ImageSection, data []byte, offset int) (int, error) {
	if len(data) < offset+8 {
		return offset, fmt.Errorf("数据太短，无法读取 TRUE seeds")
	}

	for i := 0; i < 3; i++ {
		section.TRUESeeds[i] = binary.LittleEndian.Uint16(data[offset : offset+2])
		debug("  seed[%d] from data[%d:%d] = 0x%02x%02x → %d",
			i, offset, offset+2, data[offset], data[offset+1], section.TRUESeeds[i])
		offset += 2
	}
	offset += 2 // 跳过 unknown (uint16)

	debug("TRUE seeds: [%d, %d, %d], offset=%d",
		section.TRUESeeds[0], section.TRUESeeds[1], section.TRUESeeds[2], offset)

	return offset, nil
}

// 加载 TRUE Huffman 表
func loadTRUEHuffmanTable(section *ImageSection, data []byte, offset int) (int, error) {
	section.TRUETable = make([]TRUEHuffmanElement, 0, 256)

	for {
		if len(data) < offset+2 {
			return offset, fmt.Errorf("数据太短，无法读取 Huffman 表")
		}

		element := TRUEHuffmanElement{
			CodeSize: data[offset],
			Code:     data[offset+1],
		}
		section.TRUETable = append(section.TRUETable, element)
		offset += 2

		if element.CodeSize == 0 {
			break
		}
	}

	debug("  Huffman table: %d elements, offset=%d", len(section.TRUETable), offset)
	return offset, nil
}

// 加载 TRUE 平面大小
func loadTRUEPlaneSizes(section *ImageSection, data []byte, offset int) (int, error) {
	if len(data) < offset+12 {
		return offset, fmt.Errorf("数据太短，无法读取平面大小")
	}

	section.TRUEPlaneSizes = make([]uint32, 3)
	for i := 0; i < 3; i++ {
		section.TRUEPlaneSizes[i] = binary.LittleEndian.Uint32(data[offset : offset+4])
		offset += 4
	}

	debug("  Plane sizes: [%d, %d, %d], data len=%d",
		section.TRUEPlaneSizes[0], section.TRUEPlaneSizes[1], section.TRUEPlaneSizes[2], len(data))

	return offset, nil
}

func loadTRUEImage(section *ImageSection, data []byte) error {
	offset := 0
	isQuattro := isQuattroFormat(section.Type) || isQuattroFormat(section.Format)

	debug("loadTRUEImage: isQuattro=%v, dataLen=%d", isQuattro, len(data))

	var err error
	if isQuattro {
		offset, err = loadQuattroPlanes(section, data, offset)
		if err != nil {
			return err
		}
	}

	offset, err = loadTRUESeeds(section, data, offset)
	if err != nil {
		return err
	}

	offset, err = loadTRUEHuffmanTable(section, data, offset)
	if err != nil {
		return err
	}

	// 对于 Quattro，有额外的 uint32 unknown
	if isQuattro {
		if len(data) < offset+4 {
			return fmt.Errorf("数据太短，无法读取 Quattro unknown")
		}
		offset += 4
		debug("  Quattro unknown skipped, offset=%d", offset)
	}

	offset, err = loadTRUEPlaneSizes(section, data, offset)
	if err != nil {
		return err
	}

	// 构建 Huffman 树
	section.HuffmanTree = NewHuffmanTree(8)
	PopulateTRUEHuffmanTree(section.HuffmanTree, section.TRUETable)

	// 保存压缩数据
	section.Data = data[offset:]

	return nil
}

// 解码图像数据
func (section *ImageSection) DecodeImage() error {
	// 对于 Quattro 文件，使用 Type 字段判断格式
	formatID := section.Format
	if isQuattroFormat(section.Type) || isTRUEFormatType(section.Type) {
		formatID = section.Type
	}

	switch {
	case formatID == ImageRAWHuffmanX530 || formatID == ImageRAWHuffman10bit:
		return section.decodeHuffmanImage()
	case isTRUEFormat(formatID):
		return section.decodeTRUEImage()
	default:
		return fmt.Errorf("不支持的图像格式: Type=0x%08x Format=0x%08x", section.Type, section.Format)
	}
}

// 解码传统 Huffman 图像
func (section *ImageSection) decodeHuffmanImage() error {
	totalPixels := int(section.Rows * section.Columns * 3)
	section.DecodedData = make([]uint16, totalPixels)

	// 设置解码后的实际尺寸
	section.DecodedColumns = section.Columns
	section.DecodedRows = section.Rows

	if section.HuffmanCompressed {
		return section.decodeCompressedHuffmanImage()
	}

	return section.decodeSimpleHuffmanImage()
}

func (section *ImageSection) decodeCompressedHuffmanImage() error {
	minimum := int16(0)
	offset := int16(0)
	section.decodeCompressedHuffmanRows(offset, &minimum)

	if minimum < 0 {
		offset = -minimum
		minimum = 0
		section.decodeCompressedHuffmanRows(offset, &minimum)
	}

	return nil
}

func (section *ImageSection) decodeCompressedHuffmanRows(offset int16, minimum *int16) {
	for row := uint32(0); row < section.Rows; row++ {
		rowData := HuffmanDecodeRow(section.Data, section.RowOffsets[row], int(section.Columns), section.HuffmanTree, offset, minimum)
		copy(section.DecodedData[row*section.Columns*3:], rowData)
	}
}

func (section *ImageSection) decodeSimpleHuffmanImage() error {
	rowStride := int(section.RowStride)
	if rowStride == 0 {
		return fmt.Errorf("Huffman simple decode 缺少 row stride")
	}

	for row := uint32(0); row < section.Rows; row++ {
		rowStart := int(row) * rowStride
		if rowStart+int(section.Columns)*4 > len(section.Data) {
			return fmt.Errorf("Huffman simple decode 数据越界: row=%d", row)
		}

		acc := [3]uint16{}
		for col := uint32(0); col < section.Columns; col++ {
			packed := binary.LittleEndian.Uint32(section.Data[rowStart+int(col)*4:])
			for color := 0; color < 3; color++ {
				index := uint16((packed >> (uint(color) * uint(section.HuffmanBits))) & ((1 << section.HuffmanBits) - 1))
				acc[color] += uint16(section.simpleHuffmanDiff(index))

				value := acc[color]
				if int16(value) < 0 {
					value = 0
				}

				dstIdx := int(row)*int(section.Columns)*3 + int(col)*3 + color
				section.DecodedData[dstIdx] = value
			}
		}
	}

	return nil
}

func (section *ImageSection) simpleHuffmanDiff(index uint16) int32 {
	if len(section.HuffmanMapping) == 0 {
		return int32(index)
	}
	if int(index) >= len(section.HuffmanMapping) {
		return 0
	}
	return int32(section.HuffmanMapping[index])
}

// 解码 TRUE 引擎图像
func (section *ImageSection) decodeTRUEImage() error {
	// 检查是否是 Quattro 格式
	isQuattro := isQuattroFormat(section.Type) || isQuattroFormat(section.Format)
	isQuattroLayout := isQuattro && section.QuattroLayout == 1

	debug("decodeTRUEImage: size=%dx%d, isQuattro=%v, layout=%d",
		section.Columns, section.Rows, isQuattro, section.QuattroLayout)

	// 对于 Quattro 1:1:4，主图像使用 bottom/middle 层的低分辨率（与 C 版本一致）
	var mainRows, mainCols int
	if isQuattroLayout {
		// 使用 bottom/middle 层的低分辨率
		mainRows = int(section.QuattroPlanes[0].Rows)
		mainCols = int(section.QuattroPlanes[0].Columns)
		debug("  Quattro 1:1:4 layout: main(low-res)=%dx%d, bottom/middle=%dx%d, top_plane=%dx%d",
			mainCols, mainRows,
			section.QuattroPlanes[0].Columns, section.QuattroPlanes[0].Rows,
			section.QuattroPlanes[2].Columns, section.QuattroPlanes[2].Rows)
	} else {
		mainRows = int(section.Rows)
		mainCols = int(section.Columns)
	}

	totalPixels := mainRows * mainCols * 3
	section.DecodedData = make([]uint16, totalPixels)

	// 设置解码后的实际尺寸
	section.DecodedColumns = uint32(mainCols)
	section.DecodedRows = uint32(mainRows)

	// 验证平面大小
	totalPlaneSize := 0
	for i := 0; i < 3; i++ {
		if section.TRUEPlaneSizes[i] > uint32(len(section.Data)) {
			return fmt.Errorf("TRUE 图像解码失败：平面 %d 大小 (%d bytes) 超出可用数据 (%d bytes)\n"+
				"这可能是由于 TRUE/Merrill 格式的数据结构解析不正确\n"+
				"请使用原始 C 版本的 x3f_extract 工具处理此文件",
				i, section.TRUEPlaneSizes[i], len(section.Data))
		}
		totalPlaneSize += int(section.TRUEPlaneSizes[i])
	}

	if totalPlaneSize > len(section.Data) {
		return fmt.Errorf("TRUE 图像解码失败：总平面大小 (%d bytes) 超出可用数据 (%d bytes)\n"+
			"这表明 TRUE/Merrill 格式的头部解析存在问题\n"+
			"请使用原始 C 版本的 x3f_extract 工具处理此文件",
			totalPlaneSize, len(section.Data))
	}

	// 解码三个颜色平面
	dataOffset := 0
	var topLayerData []uint16
	var topLayerRows, topLayerCols int

	for color := 0; color < 3; color++ {
		planeSize := int(section.TRUEPlaneSizes[color])
		planeData := section.Data[dataOffset : dataOffset+planeSize]

		var planeRows, planeCols int
		if isQuattro {
			// Quattro/SDQ: 每个 plane 使用自己的尺寸。
			// binned Quattro 的 plane 2 可能比输出图像更宽，写入时会丢弃右侧附加数据。
			planeRows = int(section.QuattroPlanes[color].Rows)
			planeCols = int(section.QuattroPlanes[color].Columns)
		} else {
			// 标准格式：所有平面使用相同尺寸
			planeRows = mainRows
			planeCols = mainCols
		}

		colorData := TRUEDecodeOneColor(
			planeData,
			planeRows,
			planeCols,
			section.HuffmanTree,
			uint32(section.TRUESeeds[color]),
		)

		if isQuattroLayout && color == 2 {
			// Quattro top layer: 单独保存，不存储到 DecodedData
			topLayerData = colorData
			topLayerRows = planeRows
			topLayerCols = planeCols
		} else {
			storeTRUEPlaneData(section.DecodedData, colorData, planeRows, planeCols, mainRows, mainCols, color)
		}

		// 平面数据按 16 字节对齐（与 C 代码一致）
		alignedSize := ((planeSize + 15) / 16) * 16
		dataOffset += alignedSize
	}

	if isQuattroLayout && topLayerData != nil {
		// 保存 top 层数据供后续处理使用
		section.QuattroTopData = topLayerData
		section.QuattroTopRows = topLayerRows
		section.QuattroTopCols = topLayerCols
		debug("  Top layer saved separately: %dx%d", topLayerCols, topLayerRows)
	}

	return nil
}
