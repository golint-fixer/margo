package mar // import "go.mozilla.org/mar"

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
)

const (
	// MarIDLen is the length of the MAR ID header.
	// A MAR file starts with 4 bytes containing the MAR ID, typically "MAR1"
	MarIDLen = 4

	// OffsetToIndexLen is the length of the offset to index value.
	// The MAR file continues with the position of the index relative
	// to the beginning of the file
	OffsetToIndexLen = 4

	// SignaturesHeaderLen is the length of the signatures header
	// The signature header contains the total size of the MAR file on 8 bytes
	// and the number of signatures in the file on 4 bytes
	SignaturesHeaderLen = 12

	// SignatureEntryHeaderLen is the length of the header of each signature entry
	// Each signature entry contains an algorithm and a size, each on 4 bytes
	SignatureEntryHeaderLen = 8

	// AdditionalSectionsHeaderLen is the length of the additional sections header
	// Optional additional sections can be added, their number is stored on 4 bytes
	AdditionalSectionsHeaderLen = 4

	// AdditionalSectionsEntryHeaderLen is the length of the header of each add. section
	// Each additional section has a block size and block identifier on 4 bytes each
	AdditionalSectionsEntryHeaderLen = 8

	// IndexHeaderLen is the length of the index header
	// The size of the index is stored in a header on 4 bytes
	IndexHeaderLen = 4

	// IndexEntryHeaderLen is the length of the header of each index entry.
	// Each index entry contains a header with an offset to content (relative to
	// the beginning of the file), a content size and permission flags, each on 4 bytes
	IndexEntryHeaderLen = 12

	// SigAlgRsaPkcs1Sha1 is the ID of a signature of type RSA-PKCS1-SHA1
	SigAlgRsaPkcs1Sha1 = 1

	// SigAlgRsaPkcs1Sha384 is the ID of a signature of type RSA-PKCS1-SHA384
	SigAlgRsaPkcs1Sha384 = 2

	// BlockIDProductInfo is the ID of a Product Information Block in additional sections
	BlockIDProductInfo = 1
)

// File is a parsed MAR file.
type File struct {
	MarID                    string                   `json:"mar_id" yaml:"mar_id"`
	OffsetToIndex            uint32                   `json:"offset_to_index" yaml:"offset_to_index"`
	ProductInformation       string                   `json:"product_information,omitempty" yaml:"product_information,omitempty"`
	SignaturesHeader         SignaturesHeader         `json:"signature_header" yaml:"signature_header"`
	Signatures               []Signature              `json:"signatures" yaml:"signatures"`
	AdditionalSectionsHeader AdditionalSectionsHeader `json:"additional_sections_header" yaml:"additional_sections_header"`
	AdditionalSections       []AdditionalSection      `json:"additional_sections" yaml:"additional_sections"`
	IndexHeader              IndexHeader              `json:"index_header" yaml:"index_header"`
	Index                    []IndexEntry             `json:"index" yaml:"index"`
	Content                  map[string]Entry         `json:"content" yaml:"content"`
}

// SignaturesHeader contains the total file size and number of signatures in the MAR file
type SignaturesHeader struct {
	FileSize      uint64 `json:"file_size" yaml:"file_size"`
	NumSignatures uint32 `json:"num_signatures" yaml:"num_signatures"`
}

// Signature is a single signature on the MAR file
type Signature struct {
	signatureEntryHeader
	Algorithm string `json:"algorithm" yaml:"algorithm"`
	Data      []byte `json:"data" yaml:"data"`
}

type signatureEntryHeader struct {
	AlgorithmID uint32 `json:"algorithm_id" yaml:"algorithm_id"`
	Size        uint32 `json:"size" yaml:"size"`
}

// AdditionalSectionsHeader contains the number of additional sections in the MAR file
type AdditionalSectionsHeader struct {
	NumAdditionalSections uint32 `json:"num_additional_sections" yaml:"num_additional_sections"`
}

// AdditionalSection is a single additional section on the MAR file
type AdditionalSection struct {
	additionalSectionEntryHeader
	Data []byte `json:"data" yaml:"data"`
}

type additionalSectionEntryHeader struct {
	BlockSize uint32 `json:"block_size" yaml:"block_size"`
	BlockID   uint32 `json:"block_id" yaml:"block_id"`
}

// Entry is a single file entry in the MAR file. If IsCompressed is true, the content
// is compressed with xz
type Entry struct {
	Data         []byte `json:"data" yaml:"data"`
	IsCompressed bool   `json:"is_compressed" yaml:"is_compressed"`
}

// IndexHeader is the size of the index section of the MAR file, in bytes
type IndexHeader struct {
	Size uint32 `json:"size" yaml:"size"`
}

// IndexEntry is a single index entry in the MAR index
type IndexEntry struct {
	indexEntryHeader
	FileName string `json:"file_name" yaml:"file_name"`
}

type indexEntryHeader struct {
	OffsetToContent uint32 `json:"offset_to_content" yaml:"offset_to_content"`
	Size            uint32 `json:"size" yaml:"size"`
	Flags           uint32 `json:"flags" yaml:"flags"`
}

// Unmarshal takes an unparsed MAR file as input and parses it into a File struct
func Unmarshal(input []byte, file *File) error {
	var (
		// current position of the cursor in the file
		cursor int

		i uint32
	)
	if len(input) < MarIDLen+OffsetToIndexLen+SignaturesHeaderLen+AdditionalSectionsHeaderLen+IndexHeaderLen {
		return fmt.Errorf("input is smaller than minimum MAR size and cannot be parsed")
	}

	// Parse the MAR ID
	marid := make([]byte, MarIDLen, MarIDLen)
	err := parse(input, &marid, cursor, MarIDLen)
	if err != nil {
		return fmt.Errorf("parsing failed at position %d: %v", cursor, err)
	}
	cursor += MarIDLen
	file.MarID = string(marid)

	// Parse the offset to the index
	err = parse(input, &file.OffsetToIndex, cursor, OffsetToIndexLen)
	if err != nil {
		return fmt.Errorf("parsing failed at position %d: %v", cursor, err)
	}
	cursor += OffsetToIndexLen

	fmt.Fprintf(os.Stderr, "Header: MAR ID=%q, Offset to Index=%d\n", file.MarID, file.OffsetToIndex)

	// Parse the Signature header
	err = parse(input, &file.SignaturesHeader, cursor, SignaturesHeaderLen)
	if err != nil {
		return fmt.Errorf("parsing failed at position %d: %v", cursor, err)
	}
	cursor += SignaturesHeaderLen
	fmt.Fprintf(os.Stderr, "\nSignatures Header: FileSize=%d, NumSignatures=%d\n", file.SignaturesHeader.FileSize, file.SignaturesHeader.NumSignatures)

	// Parse each signature and append them to the File
	for i = 0; i < file.SignaturesHeader.NumSignatures; i++ {
		var (
			sigEntryHeader signatureEntryHeader
			sig            Signature
		)

		err = parse(input, &sigEntryHeader, cursor, SignatureEntryHeaderLen)
		if err != nil {
			return fmt.Errorf("parsing failed at position %d: %v", cursor, err)
		}
		cursor += SignatureEntryHeaderLen

		sig.AlgorithmID = sigEntryHeader.AlgorithmID
		sig.Size = sigEntryHeader.Size
		switch sig.AlgorithmID {
		case SigAlgRsaPkcs1Sha1:
			sig.Algorithm = "RSA-PKCS1-SHA1"
		case SigAlgRsaPkcs1Sha384:
			sig.Algorithm = "RSA-PKCS1-SHA384"
		default:
			sig.Algorithm = "unknown"
		}

		fmt.Fprintf(os.Stderr, "* Signature %d Entry Header: Algorithm=%q, Size=%d\n", i, sig.Algorithm, sig.Size)

		sig.Data = make([]byte, sig.Size, sig.Size)
		err = parse(input, &sig.Data, cursor, int(sig.Size))
		if err != nil {
			return fmt.Errorf("parsing failed at position %d: %v", cursor, err)
		}
		cursor += int(sig.Size)
		fmt.Fprintf(os.Stderr, "* Signature %d Data (len=%d): %X\n", i, len(sig.Data), sig.Data)
		file.Signatures = append(file.Signatures, sig)
	}

	// Parse the additional sections header
	err = parse(input, &file.AdditionalSectionsHeader, cursor, AdditionalSectionsHeaderLen)
	if err != nil {
		return fmt.Errorf("parsing failed at position %d: %v", cursor, err)
	}
	cursor += AdditionalSectionsHeaderLen
	fmt.Fprintf(os.Stderr, "\nAdditional Sections: %d\n", file.AdditionalSectionsHeader.NumAdditionalSections)

	// Parse each additional section and append them to the File
	for i = 0; i < file.AdditionalSectionsHeader.NumAdditionalSections; i++ {
		var (
			ash     additionalSectionEntryHeader
			as      AdditionalSection
			blockid string
		)

		err = parse(input, &ash, cursor, AdditionalSectionsEntryHeaderLen)
		if err != nil {
			return fmt.Errorf("parsing failed at position %d: %v", cursor, err)
		}
		cursor += AdditionalSectionsEntryHeaderLen

		as.BlockID = ash.BlockID
		as.BlockSize = ash.BlockSize
		dataSize := ash.BlockSize - AdditionalSectionsEntryHeaderLen
		as.Data = make([]byte, dataSize, dataSize)

		err = parse(input, &as.Data, cursor, int(dataSize))
		if err != nil {
			return fmt.Errorf("parsing failed at position %d: %v", cursor, err)
		}
		cursor += int(dataSize)

		switch ash.BlockID {
		case BlockIDProductInfo:
			blockid = "Product Information"
			// remove all the null bytes from the product info string
			file.ProductInformation = fmt.Sprintf("%s", strings.Replace(strings.Trim(string(as.Data), "\x00"), "\x00", " ", -1))
		default:
			blockid = fmt.Sprintf("%d (unknown)", ash.BlockID)
		}
		fmt.Fprintf(os.Stderr, "* Additional Section %d: BlockSize=%d, BlockID=%q, Data=%q (len=%d)\n", i, ash.BlockSize, blockid, as.Data, dataSize)
		file.AdditionalSections = append(file.AdditionalSections, as)
	}

	// Parse the index before parsing the content
	cursor = int(file.OffsetToIndex)
	fmt.Fprintf(os.Stderr, "\nJumping to index at offset %d\n", cursor)

	err = parse(input, &file.IndexHeader, cursor, IndexHeaderLen)
	if err != nil {
		return fmt.Errorf("parsing failed at position %d: %v", cursor, err)
	}
	cursor += IndexHeaderLen

	fmt.Fprintf(os.Stderr, "Index Size: %d\n", file.IndexHeader.Size)

	for i = 0; ; i++ {
		var (
			idxEntryHeader indexEntryHeader
			idxEntry       IndexEntry
		)
		if cursor >= int(file.SignaturesHeader.FileSize) {
			break
		}
		err = parse(input, &idxEntryHeader, cursor, IndexEntryHeaderLen)
		if err != nil {
			return fmt.Errorf("parsing failed at position %d: %v", cursor, err)
		}
		cursor += IndexEntryHeaderLen

		idxEntry.Size = idxEntryHeader.Size
		idxEntry.Flags = idxEntryHeader.Flags
		idxEntry.OffsetToContent = idxEntryHeader.OffsetToContent

		endNamePos := bytes.Index(input[cursor:], []byte("\x00"))
		if endNamePos < 0 {
			return fmt.Errorf("malformed index is missing null terminator in file name")
		}
		idxEntry.FileName = string(input[cursor : cursor+endNamePos])
		cursor += endNamePos + 1

		fmt.Fprintf(os.Stderr, "* Index Entry %3d: Size=%10d Flags=%s Offset=%10d Name=%q\n",
			i, idxEntry.Size, os.FileMode(idxEntry.Flags), idxEntry.OffsetToContent, idxEntry.FileName)
		file.Index = append(file.Index, idxEntry)
	}

	file.Content = make(map[string]Entry)
	for _, idxEntry := range file.Index {
		var entry Entry
		// seek and read content
		entry.Data = append(entry.Data, input[idxEntry.OffsetToContent:idxEntry.OffsetToContent+idxEntry.Size]...)
		// files in MAR archives can be compressed with xz, so we test
		// the first 6 bytes to check for that
		//                                                             /---XZ's magic number--\
		if len(entry.Data) > 6 && bytes.Equal(entry.Data[0:6], []byte("\xFD\x37\x7A\x58\x5A\x00")) {
			entry.IsCompressed = true
		}
		if _, ok := file.Content[idxEntry.FileName]; ok {
			return fmt.Errorf("file named %q already exists in the archive, duplicates are not permitted", idxEntry.FileName)
		}
		file.Content[idxEntry.FileName] = entry
	}
	return nil
}

// MarshalForSignature returns an []byte of the data to be signed, or verified
func (file *File) MarshalForSignature() ([]byte, error) {
	// the total size of a signature block is the original file minus the signature data
	var sigDataSize uint32
	for _, sig := range file.Signatures {
		sigDataSize += sig.Size
	}
	output := make([]byte, file.SignaturesHeader.FileSize-uint64(sigDataSize))

	buf := new(bytes.Buffer)
	err := binary.Write(buf, binary.BigEndian, []byte(file.MarID))
	if err != nil {
		return nil, err
	}
	err = binary.Write(buf, binary.BigEndian, file.OffsetToIndex)
	if err != nil {
		return nil, err
	}
	err = binary.Write(buf, binary.BigEndian, file.SignaturesHeader)
	if err != nil {
		return nil, err
	}
	for _, sig := range file.Signatures {
		err = binary.Write(buf, binary.BigEndian, sig.AlgorithmID)
		if err != nil {
			return nil, err
		}
		err = binary.Write(buf, binary.BigEndian, sig.Size)
		if err != nil {
			return nil, err
		}
	}
	err = binary.Write(buf, binary.BigEndian, file.AdditionalSectionsHeader)
	if err != nil {
		return nil, err
	}
	for _, as := range file.AdditionalSections {
		err = binary.Write(buf, binary.BigEndian, as.BlockSize)
		if err != nil {
			return nil, err
		}
		err = binary.Write(buf, binary.BigEndian, as.BlockID)
		if err != nil {
			return nil, err
		}
		err = binary.Write(buf, binary.BigEndian, as.Data)
		if err != nil {
			return nil, err
		}
	}
	// insert the first section at the beginning of the file
	copy(output[0:buf.Len()], buf.Bytes())

	// we need to marshal the content according to the index
	idxBuf := new(bytes.Buffer)
	err = binary.Write(idxBuf, binary.BigEndian, file.IndexHeader)
	if err != nil {
		return nil, err
	}
	for _, idx := range file.Index {
		err = binary.Write(idxBuf, binary.BigEndian, idx.OffsetToContent)
		if err != nil {
			return nil, err
		}
		err = binary.Write(idxBuf, binary.BigEndian, idx.Size)
		if err != nil {
			return nil, err
		}
		err = binary.Write(idxBuf, binary.BigEndian, idx.Flags)
		if err != nil {
			return nil, err
		}
		err = binary.Write(idxBuf, binary.BigEndian, []byte(idx.FileName))
		if err != nil {
			return nil, err
		}
		_, err = idxBuf.Write([]byte("\x00"))
		if err != nil {
			return nil, err
		}
		// copy the content in the right position earlier in the file
		// since we don't signatures, we remove their size from the offsets
		copy(output[idx.OffsetToContent-sigDataSize:idx.OffsetToContent+idx.Size-sigDataSize], file.Content[idx.FileName].Data)
	}
	if uint32(idxBuf.Len()) != file.IndexHeader.Size+IndexHeaderLen {
		return nil, fmt.Errorf("marshalled index has size %d when size %d was expected", idxBuf.Len(), file.IndexHeader.Size)
	}
	// append the index to the end of the output
	copy(output[file.OffsetToIndex-sigDataSize:file.OffsetToIndex+uint32(idxBuf.Len())-sigDataSize], idxBuf.Bytes())

	return output, nil
}

func parse(input []byte, data interface{}, startPos, readLen int) error {
	if len(input) < startPos+readLen {
		return fmt.Errorf("refusing to read more bytes than present in input")
	}
	r := bytes.NewReader(input[startPos : startPos+readLen])
	return binary.Read(r, binary.BigEndian, data)
}
