package output

import (
	"encoding/binary"
	"io"
	"os"
	"sort"
)

// IFDWriter 自动管理 IFD 标签和数据偏移的写入器
// 借鉴 chai2010/tiff 的设计:
// 1. 动态扩展 pointer area
// 2. 统一的数据存储格式
// 3. 自动判断内联
type IFDWriter struct {
	file     *os.File
	entries  []*TagEntry
	startPos int64
}

// TagEntry IFD 标签条目(统一格式)
type TagEntry struct {
	tag   uint16
	typ   uint16
	count uint32
	data  []uint32 // 统一用 uint32 数组存储(包括 RATIONAL = 2个uint32)
}

// NewIFDWriter 创建新的 IFD 写入器
func NewIFDWriter(file *os.File) *IFDWriter {
	pos, _ := file.Seek(0, io.SeekCurrent)
	return &IFDWriter{
		file:     file,
		entries:  make([]*TagEntry, 0),
		startPos: pos,
	}
}

// byteSizeForType 返回每个数据类型的字节大小
func byteSizeForType(typ uint16) int {
	switch typ {
	case TypeByte, TypeASCII, TypeUndefined:
		return 1
	case TypeShort:
		return 2
	case TypeLong, TypeSRational, TypeRational:
		return 4
	default:
		return 4
	}
}

// putData 将 uint32 数组写入字节缓冲区
func (e *TagEntry) putData(p []byte) {
	for _, d := range e.data {
		switch e.typ {
		case TypeByte, TypeASCII, TypeUndefined:
			p[0] = byte(d)
			p = p[1:]
		case TypeShort:
			binary.LittleEndian.PutUint16(p, uint16(d))
			p = p[2:]
		case TypeLong, TypeRational, TypeSRational:
			binary.LittleEndian.PutUint32(p, d)
			p = p[4:]
		}
	}
}

// AddShort 添加 SHORT 类型标签
func (w *IFDWriter) AddShort(tag uint16, value uint16) {
	w.entries = append(w.entries, &TagEntry{
		tag:   tag,
		typ:   TypeShort,
		count: 1,
		data:  []uint32{uint32(value)},
	})
}

// AddShortArray 添加 SHORT 数组
func (w *IFDWriter) AddShortArray(tag uint16, values []uint16) {
	data := make([]uint32, len(values))
	for i, v := range values {
		data[i] = uint32(v)
	}
	w.entries = append(w.entries, &TagEntry{
		tag:   tag,
		typ:   TypeShort,
		count: uint32(len(values)),
		data:  data,
	})
}

// AddLong 添加 LONG 类型标签
func (w *IFDWriter) AddLong(tag uint16, value uint32) {
	w.entries = append(w.entries, &TagEntry{
		tag:   tag,
		typ:   TypeLong,
		count: 1,
		data:  []uint32{value},
	})
}

// AddLongArray 添加 LONG 数组
func (w *IFDWriter) AddLongArray(tag uint16, values []uint32) {
	w.entries = append(w.entries, &TagEntry{
		tag:   tag,
		typ:   TypeLong,
		count: uint32(len(values)),
		data:  values,
	})
}

// AddByte 添加 BYTE 类型标签(4个字节内联)
func (w *IFDWriter) AddByte(tag uint16, value uint32) {
	w.entries = append(w.entries, &TagEntry{
		tag:   tag,
		typ:   TypeByte,
		count: 4,
		data:  []uint32{value & 0xFF, (value >> 8) & 0xFF, (value >> 16) & 0xFF, (value >> 24) & 0xFF},
	})
}

// AddASCII 添加 ASCII 字符串
func (w *IFDWriter) AddASCII(tag uint16, str string, length int) {
	data := make([]uint32, length)
	for i := 0; i < len(str) && i < length; i++ {
		data[i] = uint32(str[i])
	}
	w.entries = append(w.entries, &TagEntry{
		tag:   tag,
		typ:   TypeASCII,
		count: uint32(length),
		data:  data,
	})
}

// AddRational 添加 RATIONAL(2个 uint32: 分子/分母)
func (w *IFDWriter) AddRational(tag uint16, numerator, denominator uint32) {
	w.entries = append(w.entries, &TagEntry{
		tag:   tag,
		typ:   TypeRational,
		count: 1,
		data:  []uint32{numerator, denominator},
	})
}

// AddRationalArray 添加 RATIONAL 数组
func (w *IFDWriter) AddRationalArray(tag uint16, values [][2]uint32) {
	data := make([]uint32, len(values)*2)
	for i, v := range values {
		data[i*2] = v[0]
		data[i*2+1] = v[1]
	}
	w.entries = append(w.entries, &TagEntry{
		tag:   tag,
		typ:   TypeRational,
		count: uint32(len(values)),
		data:  data,
	})
}

// AddSRational 添加 SRATIONAL(signed)
func (w *IFDWriter) AddSRational(tag uint16, numerator, denominator int32) {
	w.entries = append(w.entries, &TagEntry{
		tag:   tag,
		typ:   TypeSRational,
		count: 1,
		data:  []uint32{uint32(numerator), uint32(denominator)},
	})
}

// AddSRationalArray 添加 SRATIONAL 数组
func (w *IFDWriter) AddSRationalArray(tag uint16, values [][2]int32) {
	data := make([]uint32, len(values)*2)
	for i, v := range values {
		data[i*2] = uint32(v[0])
		data[i*2+1] = uint32(v[1])
	}
	w.entries = append(w.entries, &TagEntry{
		tag:   tag,
		typ:   TypeSRational,
		count: uint32(len(values)),
		data:  data,
	})
}

// AddRationalFromFloat 从浮点数添加 RATIONAL
func (w *IFDWriter) AddRationalFromFloat(tag uint16, value float64, signed bool) {
	num, denom := floatToRational(value, 1000000000)
	if signed {
		w.AddSRational(tag, int32(num), int32(denom))
	} else {
		w.AddRational(tag, uint32(num), uint32(denom))
	}
}

// AddRationalArrayFromFloats 从浮点数数组添加 RATIONAL 数组
func (w *IFDWriter) AddRationalArrayFromFloats(tag uint16, values []float64, signed bool) {
	// 使用 2^26 作为最大分母，与 C 版本 libtiff 的行为接近
	// 这样可以避免过大的分子/分母导致精度损失
	const maxDenom = 67108864 // 2^26

	if signed {
		svals := make([][2]int32, len(values))
		for i, v := range values {
			num, denom := floatToRational(v, maxDenom)
			svals[i] = [2]int32{int32(num), int32(denom)}
		}
		w.AddSRationalArray(tag, svals)
	} else {
		uvals := make([][2]uint32, len(values))
		for i, v := range values {
			num, denom := floatToRational(v, maxDenom)
			uvals[i] = [2]uint32{uint32(num), uint32(denom)}
		}
		w.AddRationalArray(tag, uvals)
	}
}

// AddUndefined 添加 UNDEFINED 类型数据
func (w *IFDWriter) AddUndefined(tag uint16, data []byte) {
	uintData := make([]uint32, len(data))
	for i, b := range data {
		uintData[i] = uint32(b)
	}
	w.entries = append(w.entries, &TagEntry{
		tag:   tag,
		typ:   TypeUndefined,
		count: uint32(len(data)),
		data:  uintData,
	})
}

// ReservePointer 预留一个指针位置(返回 entry 索引)
func (w *IFDWriter) ReservePointer(tag uint16) int {
	w.entries = append(w.entries, &TagEntry{
		tag:   tag,
		typ:   TypeLong,
		count: 1,
		data:  []uint32{0}, // 占位符
	})
	return len(w.entries) - 1
}

// UpdatePointer 更新预留的指针值
func (w *IFDWriter) UpdatePointer(index int, offset uint32) error {
	if index < 0 || index >= len(w.entries) {
		return nil // 忽略错误,保持兼容
	}
	w.entries[index].data[0] = offset
	return nil
}

// Write 写入 IFD 和所有数据(借鉴 chai2010/tiff 的两阶段写入)
func (w *IFDWriter) Write() (int64, error) {
	// 按 tag 升序排序
	sort.Slice(w.entries, func(i, j int) bool {
		return w.entries[i].tag < w.entries[j].tag
	})

	const ifdEntryLen = 12
	numEntries := uint16(len(w.entries))

	// 动态扩展的 pointer area(借鉴 chai2010/tiff)
	parea := make([]byte, 1024)
	pareaOffset := int64(w.startPos) + int64(2+numEntries*ifdEntryLen+4)
	currentPareaPos := 0

	// 写入条目数
	if err := binary.Write(w.file, binary.LittleEndian, numEntries); err != nil {
		return 0, err
	}

	var buf [ifdEntryLen]byte

	// 写入所有 IFD entries
	for _, entry := range w.entries {
		binary.LittleEndian.PutUint16(buf[0:2], entry.tag)
		binary.LittleEndian.PutUint16(buf[2:4], entry.typ)

		count := entry.count
		if entry.typ == TypeRational || entry.typ == TypeSRational {
			// RATIONAL 类型: count 表示有理数个数,但 data 是 2*count 个 uint32
			// 这里 count 已经是正确的有理数个数
		}
		binary.LittleEndian.PutUint32(buf[4:8], count)

		// 计算数据长度
		datalen := int(count) * byteSizeForType(entry.typ)
		if entry.typ == TypeRational || entry.typ == TypeSRational {
			datalen = int(count) * 8 // 每个有理数 8 字节
		}

		// 判断是否内联(自动判断,借鉴 chai2010/tiff)
		if datalen <= 4 {
			// 内联: 直接写入 value 字段
			entry.putData(buf[8:12])
		} else {
			// 外部: 写入指针
			// 检查 parea 是否需要扩展
			if (currentPareaPos + datalen) > len(parea) {
				newlen := len(parea) + 1024
				for (currentPareaPos + datalen) > newlen {
					newlen += 1024
				}
				newarea := make([]byte, newlen)
				copy(newarea, parea)
				parea = newarea
			}

			entry.putData(parea[currentPareaPos : currentPareaPos+datalen])
			binary.LittleEndian.PutUint32(buf[8:12], uint32(pareaOffset+int64(currentPareaPos)))
			currentPareaPos += datalen
		}

		if _, err := w.file.Write(buf[:]); err != nil {
			return 0, err
		}
	}

	// 写入 Next IFD offset = 0
	if err := binary.Write(w.file, binary.LittleEndian, uint32(0)); err != nil {
		return 0, err
	}

	// 写入 pointer area
	if _, err := w.file.Write(parea[:currentPareaPos]); err != nil {
		return 0, err
	}

	// 返回当前文件位置
	return w.file.Seek(0, io.SeekCurrent)
}

// GetCurrentPosition 获取 IFD 写入后的预期文件位置
func (w *IFDWriter) GetCurrentPosition() int64 {
	numEntries := uint16(len(w.entries))
	ifdSize := int64(2 + numEntries*12 + 4)

	totalDataSize := int64(0)
	for _, entry := range w.entries {
		datalen := int(entry.count) * byteSizeForType(entry.typ)
		if entry.typ == TypeRational || entry.typ == TypeSRational {
			datalen = int(entry.count) * 8
		}
		if datalen > 4 {
			totalDataSize += int64(datalen)
		}
	}

	return w.startPos + ifdSize + totalDataSize
}
