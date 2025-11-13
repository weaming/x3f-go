package x3f

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"unicode/utf16"
	"unsafe"
)

var le = binary.LittleEndian

// opens an X3F file for reading
func Open(filename string) (*File, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}

	stat, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	x3f := &File{
		reader: f,
		size:   stat.Size(),
	}

	if err := x3f.readHeader(); err != nil {
		f.Close()
		return nil, fmt.Errorf("failed to read header: %w", err)
	}

	if err := x3f.readDirectory(); err != nil {
		f.Close()
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	return x3f, nil
}

// closes the X3F file
func (f *File) Close() error {
	if closer, ok := f.reader.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// reads the X3F file header
func (f *File) readHeader() error {
	buf := make([]byte, 4+4+16+4+4+4+4+32+32+64*4+64*4)
	if _, err := f.reader.ReadAt(buf, 0); err != nil {
		return fmt.Errorf("failed to read header: %w", err)
	}

	f.Header = &FileHeader{}
	offset := 0

	copy(f.Header.Identifier[:], buf[offset:offset+4])
	offset += 4

	magic := le.Uint32(f.Header.Identifier[:])
	if magic != FOVb {
		return fmt.Errorf("invalid X3F file: magic bytes 0x%08x != 0x%08x", magic, FOVb)
	}

	f.Header.Version = le.Uint32(buf[offset : offset+4])
	offset += 4

	copy(f.Header.UniqueIdentifier[:], buf[offset:offset+16])
	offset += 16

	// version < 4.0 有 mark_bits, columns, rows, rotation
	// version >= 4.0 (Quattro) 没有这些字段
	if f.Header.Version < Version40 {
		f.Header.MarkBits = le.Uint32(buf[offset : offset+4])
		offset += 4

		f.Header.Columns = le.Uint32(buf[offset : offset+4])
		offset += 4

		f.Header.Rows = le.Uint32(buf[offset : offset+4])
		offset += 4

		f.Header.Rotation = le.Uint32(buf[offset : offset+4])
		offset += 4

		if f.Header.Version >= Version21 {
			numExtData := NumExtData21
			if f.Header.Version >= Version30 {
				numExtData = NumExtData30
			}

			copy(f.Header.WhiteBalance[:], buf[offset:offset+32])
			offset += 32

			if f.Header.Version >= Version23 {
				copy(f.Header.ColorMode[:], buf[offset:offset+32])
				offset += 32
			}

			// 注意：C 版本中是先 types 后 data
			// ExtendedDataTypes 是字节数组
			for i := 0; i < numExtData; i++ {
				f.Header.ExtendedDataTypes[i] = buf[offset]
				offset++
			}

			for i := 0; i < numExtData; i++ {
				f.Header.ExtendedData[i] = float32frombits(le.Uint32(buf[offset : offset+4]))
				offset += 4
			}
		}
	}

	return nil
}

// reads the directory section from the end of file
func (f *File) readDirectory() error {
	buf := make([]byte, 4)
	if _, err := f.reader.ReadAt(buf, f.size-4); err != nil {
		return fmt.Errorf("failed to read directory offset: %w", err)
	}

	dirOffset := int64(le.Uint32(buf))

	dirHeaderBuf := make([]byte, 12)
	if _, err := f.reader.ReadAt(dirHeaderBuf, dirOffset); err != nil {
		return fmt.Errorf("failed to read directory header: %w", err)
	}

	f.Directory = &Directory{}
	copy(f.Directory.Identifier[:], dirHeaderBuf[0:4])

	dirID := le.Uint32(f.Directory.Identifier[:])
	if dirID != SECd {
		return fmt.Errorf("invalid directory identifier: 0x%08x", dirID)
	}

	f.Directory.Version = le.Uint32(dirHeaderBuf[4:8])
	f.Directory.NumEntries = le.Uint32(dirHeaderBuf[8:12])

	entriesBuf := make([]byte, f.Directory.NumEntries*12)
	if _, err := f.reader.ReadAt(entriesBuf, dirOffset+12); err != nil {
		return fmt.Errorf("failed to read directory entries: %w", err)
	}

	f.Directory.Entries = make([]DirectoryEntry, f.Directory.NumEntries)
	for i := uint32(0); i < f.Directory.NumEntries; i++ {
		offset := i * 12
		f.Directory.Entries[i] = DirectoryEntry{
			Offset: le.Uint32(entriesBuf[offset : offset+4]),
			Length: le.Uint32(entriesBuf[offset+4 : offset+8]),
			Type:   le.Uint32(entriesBuf[offset+8 : offset+12]),
		}
	}

	return nil
}

// loads a specific section by type
func (f *File) LoadSection(sectionType uint32) error {
	for i := range f.Directory.Entries {
		entry := &f.Directory.Entries[i]
		if entry.Type == sectionType {
			return f.loadSectionData(entry)
		}
		// 某些文件（包括一些 Merrill）使用直接类型而不是 SEC* 包装
		// 例如：CAMF 而不是 SECc，PROP 而不是 SECp，IMA2 而不是 SECi
		if sectionType == SECc && entry.Type == CAMF {
			return f.LoadCAMFSection(entry)
		}
		if sectionType == SECp && entry.Type == PROP {
			return f.loadPropertySection(entry)
		}
		if sectionType == SECi && (entry.Type == IMA2 || entry.Type == IMAG) {
			return f.LoadImageSection(entry)
		}
	}
	return fmt.Errorf("section type 0x%08x not found", sectionType)
}

// loads data for a directory entry
func (f *File) loadSectionData(entry *DirectoryEntry) error {
	switch entry.Type {
	case SECp:
		return f.loadPropertySection(entry)
	case SECc:
		return f.LoadCAMFSection(entry)
	case SECi:
		return f.LoadImageSection(entry)
	default:
		return fmt.Errorf("unsupported section type: 0x%08x", entry.Type)
	}
}

// loads the property section
func (f *File) loadPropertySection(entry *DirectoryEntry) error {
	buf := make([]byte, entry.Length)
	if _, err := f.reader.ReadAt(buf, int64(entry.Offset)); err != nil {
		return fmt.Errorf("failed to read property section: %w", err)
	}

	offset := 0

	if entry.Type == SECp {
		if len(buf) < 8 {
			return fmt.Errorf("property section too short")
		}
		sectionID := le.Uint32(buf[0:4])
		if sectionID != SECp {
			return fmt.Errorf("invalid property section identifier")
		}
		// SECp wrapper 只有 identifier(4) + version(4) = 8字节
		offset = 8
	} else if entry.Type == PROP {
		// PROP 类型：跳过 "PROP" 标识符(4字节) + version(4字节)
		offset = 8
	} else {
		return fmt.Errorf("invalid property section type: 0x%08x", entry.Type)
	}

	if len(buf) < offset+16 {
		return fmt.Errorf("property section too short for header")
	}

	f.Properties = &PropertyList{}
	f.Properties.NumProperties = le.Uint32(buf[offset : offset+4])
	f.Properties.CharacterFormat = le.Uint32(buf[offset+4 : offset+8])
	f.Properties.Reserved = le.Uint32(buf[offset+8 : offset+12])
	f.Properties.TotalLength = le.Uint32(buf[offset+12 : offset+16])

	propTableSize := f.Properties.NumProperties * 8
	propTableOffset := uint32(offset + 16)

	if uint32(len(buf)) < propTableOffset+propTableSize {
		return fmt.Errorf("buffer too small for property table")
	}

	f.Properties.Properties = make([]Property, f.Properties.NumProperties)

	for i := uint32(0); i < f.Properties.NumProperties; i++ {
		tableOffset := propTableOffset + i*8
		f.Properties.Properties[i].NameOffset = le.Uint32(buf[tableOffset : tableOffset+4])
		f.Properties.Properties[i].ValueOffset = le.Uint32(buf[tableOffset+4 : tableOffset+8])
	}

	dataOffset := propTableOffset + propTableSize
	if uint32(len(buf)) < dataOffset {
		return fmt.Errorf("buffer too small for property data")
	}

	f.Properties.Data = buf[dataOffset:]

	for i := range f.Properties.Properties {
		prop := &f.Properties.Properties[i]

		// 偏移量是UTF-16字符偏移量，需要乘以2得到字节偏移量
		nameStart := prop.NameOffset * 2
		if nameStart >= uint32(len(f.Properties.Data)) {
			continue
		}

		nameEnd := nameStart
		for nameEnd+1 < uint32(len(f.Properties.Data)) {
			if f.Properties.Data[nameEnd] == 0 && f.Properties.Data[nameEnd+1] == 0 {
				break
			}
			nameEnd += 2
		}

		if nameEnd <= uint32(len(f.Properties.Data)) {
			prop.NameUTF16 = bytesToUTF16(f.Properties.Data[nameStart:nameEnd])
			prop.Name = utf16ToString(prop.NameUTF16)
		}

		// 偏移量是UTF-16字符偏移量，需要乘以2得到字节偏移量
		valueStart := prop.ValueOffset * 2
		if valueStart >= uint32(len(f.Properties.Data)) {
			continue
		}

		valueEnd := valueStart
		for valueEnd+1 < uint32(len(f.Properties.Data)) {
			if f.Properties.Data[valueEnd] == 0 && f.Properties.Data[valueEnd+1] == 0 {
				break
			}
			valueEnd += 2
		}

		if valueEnd <= uint32(len(f.Properties.Data)) {
			prop.ValueUTF16 = bytesToUTF16(f.Properties.Data[valueStart:valueEnd])
			prop.Value = utf16ToString(prop.ValueUTF16)
		}
	}

	return nil
}

// returns the value of a property by name
func (f *File) GetProperty(name string) (string, bool) {
	if f.Properties == nil {
		return "", false
	}
	for _, prop := range f.Properties.Properties {
		if prop.Name == name {
			return prop.Value, true
		}
	}
	return "", false
}

// Helper functions

func float32frombits(b uint32) float32 {
	return *(*float32)(unsafe.Pointer(&b))
}

func bytesToUTF16(b []byte) []uint16 {
	if len(b)%2 != 0 {
		b = append(b, 0)
	}
	result := make([]uint16, len(b)/2)
	for i := 0; i < len(result); i++ {
		result[i] = le.Uint16(b[i*2 : i*2+2])
	}
	return result
}

func utf16ToString(s []uint16) string {
	return string(utf16.Decode(s))
}
