package x3f

// X3F file format constants
const (
	// File identifiers
	FOVb uint32 = 0x62564f46 // Main file identifier "FOVb"
	SECd uint32 = 0x64434553 // Directory identifier "SECd"

	// Property section identifiers
	PROP uint32 = 0x504f5250 // "PROP"
	SECp uint32 = 0x70434553 // "SECp"

	// Image section identifiers
	IMAG uint32 = 0x46414d49 // "IMAG"
	IMA2 uint32 = 0x32414d49 // "IMA2"
	SECi uint32 = 0x69434553 // "SECi"

	// CAMF section identifiers
	CAMF uint32 = 0x464d4143 // "CAMF"
	SECc uint32 = 0x63434553 // "SECc"

	// SDQ section identifiers
	SPPA uint32 = 0x41505053 // "SPPA"
	SECs uint32 = 0x73434553 // "SECs"
)

// Image type identifiers
const (
	ImageThumbPlain   uint32 = 0x00020003
	ImageThumbHuffman uint32 = 0x0002000b
	ImageThumbJPEG    uint32 = 0x00020012
	ImageThumbSDQ     uint32 = 0x00020019

	ImageRAWHuffmanX530  uint32 = 0x00030005
	ImageRAWHuffman10bit uint32 = 0x00030006
	ImageRAWTRUE         uint32 = 0x0003001e
	ImageRAWMerrill      uint32 = 0x0001001e
	ImageRAWQuattro      uint32 = 0x00010023
	ImageRAWSDQ          uint32 = 0x00010025
	ImageRAWSDQH         uint32 = 0x00010027
)

// X3F format versions
const (
	Version20 uint32 = (2 << 16) | 0
	Version21 uint32 = (2 << 16) | 1
	Version22 uint32 = (2 << 16) | 2
	Version23 uint32 = (2 << 16) | 3
	Version30 uint32 = (3 << 16) | 0
	Version40 uint32 = (4 << 16) | 0
	Version41 uint32 = (4 << 16) | 1
)

// Camera IDs
const (
	CameraIDDP1M uint32 = 77
	CameraIDDP2M uint32 = 78
	CameraIDDP3M uint32 = 78
	CameraIDDP0Q uint32 = 83
	CameraIDDP1Q uint32 = 80
	CameraIDDP2Q uint32 = 81
	CameraIDDP3Q uint32 = 82
	CameraIDSDQ  uint32 = 0x0F // 15 - Sigma dp Quattro
	CameraIDSDQH uint32 = 0x10 // 16 - Sigma dp Quattro H
)

// Header sizes
const (
	ImageHeaderSize        = 28
	CAMFHeaderSize         = 28
	PropertyListHeaderSize = 24
	UniqueIdentifierSize   = 16
	WhiteBalanceSize       = 32
	ColorModeSize          = 32
	NumExtData21           = 32
	NumExtData30           = 64
	NumExtData             = NumExtData30
)

// Image format type masks
const (
	FormatTypeQuattro uint32 = 0x23 // Quattro format identifier
	FormatTypeTRUE    uint32 = 0x1E // TRUE format identifier
	FormatTypeJPEG    uint32 = 0x02 // JPEG format identifier
)

// Huffman tree constants
const UndefinedLeaf uint32 = 0xffffffff

// Color encoding types
type ColorEncoding int

const (
	ColorEncodingNone ColorEncoding = iota
	ColorEncodingSRGB
	ColorEncodingAdobeRGB
	ColorEncodingProPhotoRGB
	ColorEncodingUnprocessed
	ColorEncodingQTop
)

// White balance presets
const (
	WBAuto         = "Auto"
	WBSunlight     = "Sunlight"
	WBShadow       = "Shadow"
	WBOvercast     = "Overcast"
	WBIncandescent = "Incandescent"
	WBFlorescent   = "Florescent"
	WBFlash        = "Flash"
	WBCustom       = "Custom"
	WBColorTemp    = "ColorTemp"
	WBAutoLSP      = "AutoLSP"
)
