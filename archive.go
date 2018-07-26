// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package zipserve

import (
	"bytes"
	"errors"
	"go4.org/readerutil"
	"io"
	"strings"
)

type Template struct {
	Prefix  readerutil.SizeReaderAt
	Entries []*FileHeader
	Comment string
}

type archive struct {
	dir []*header
}

type partsBuilder struct {
	parts  []readerutil.SizeReaderAt
	offset int64
}

func (pb *partsBuilder) add(r readerutil.SizeReaderAt) {
	size := r.Size()
	if size == 0 {
		return
	}
	pb.parts = append(pb.parts, r)
	pb.offset += size
}

func NewArchive(t *Template) (readerutil.SizeReaderAt, error) {
	if len(t.Comment) > uint16max {
		return nil, errors.New("Comment too long")
	}

	ar := &archive{
		dir: make([]*header, 0, len(t.Entries)),
	}
	var pb partsBuilder

	if t.Prefix != nil {
		pb.add(t.Prefix)
	}

	for _, entry := range t.Entries {
		prepareEntry(entry)
		ar.dir = append(ar.dir, &header{FileHeader: entry, offset: uint64(pb.offset)})
		header, err := makeLocalFileHeader(entry)
		if err != nil {
			return nil, err
		}
		pb.add(header)
		if entry.Content != nil {
			pb.add(entry.Content)
		} else if entry.CompressedSize64 != 0 {
			return nil, errors.New("Empty entry with nonzero length")
		}
		if !strings.HasSuffix(entry.Name, "/") {
			// data descriptor
			dataDescriptor := makeDataDescriptor(entry)
			pb.add(dataDescriptor)
		}
	}

	centralDirectory, err := makeCentralDirectory(pb.offset, ar.dir, t.Comment)
	if err != nil {
		return nil, err
	}
	pb.add(centralDirectory)

	return readerutil.NewMultiReaderAt(pb.parts...), nil
}

func prepareEntry(fh *FileHeader) {
	// The ZIP format has a sad state of affairs regarding character encoding.
	// Officially, the name and comment fields are supposed to be encoded
	// in CP-437 (which is mostly compatible with ASCII), unless the UTF-8
	// flag bit is set. However, there are several problems:
	//
	//	* Many ZIP readers still do not support UTF-8.
	//	* If the UTF-8 flag is cleared, several readers simply interpret the
	//	name and comment fields as whatever the local system encoding is.
	//
	// In order to avoid breaking readers without UTF-8 support,
	// we avoid setting the UTF-8 flag if the strings are CP-437 compatible.
	// However, if the strings require multibyte UTF-8 encoding and is a
	// valid UTF-8 string, then we set the UTF-8 bit.
	//
	// For the case, where the user explicitly wants to specify the encoding
	// as UTF-8, they will need to set the flag bit themselves.
	utf8Valid1, utf8Require1 := detectUTF8(fh.Name)
	utf8Valid2, utf8Require2 := detectUTF8(fh.Comment)
	switch {
	case fh.NonUTF8:
		fh.Flags &^= 0x800
	case (utf8Require1 || utf8Require2) && (utf8Valid1 && utf8Valid2):
		fh.Flags |= 0x800
	}

	fh.CreatorVersion = fh.CreatorVersion&0xff00 | zipVersion20 // preserve compatibility byte
	fh.ReaderVersion = zipVersion20

	// Use "extended timestamp" format since this is what Info-ZIP uses.
	// Nearly every major ZIP implementation uses a different format,
	// but at least most seem to be able to understand the other formats.
	//
	// This format happens to be identical for both local and central header
	// if modification time is the only timestamp being encoded.
	var mbuf [9]byte // 2*SizeOf(uint16) + SizeOf(uint8) + SizeOf(uint32)
	mt := uint32(fh.Modified.Unix())
	eb := writeBuf(mbuf[:])
	eb.uint16(extTimeExtraID)
	eb.uint16(5)  // Size: SizeOf(uint8) + SizeOf(uint32)
	eb.uint8(1)   // Flags: ModTime
	eb.uint32(mt) // ModTime
	fh.Extra = append(fh.Extra, mbuf[:]...)

	if strings.HasSuffix(fh.Name, "/") {
		// Set the compression method to Store to ensure data length is truly zero,
		// which the writeHeader method always encodes for the size fields.
		// This is necessary as most compression formats have non-zero lengths
		// even when compressing an empty string.
		fh.Method = Store
		fh.Flags &^= 0x8 // we will not write a data descriptor

		// Explicitly clear sizes as they have no meaning for directories.
		fh.CompressedSize64 = 0
		fh.UncompressedSize64 = 0
	} else {
		fh.Flags |= 0x8 // we will write a data descriptor
	}
}

func makeLocalFileHeader(fh *FileHeader) (readerutil.SizeReaderAt, error) {
	var buf bytes.Buffer

	err := writeHeader(&buf, fh)
	if err != nil {
		return nil, err
	}

	return bytes.NewReader(buf.Bytes()), nil
}

func makeDataDescriptor(fh *FileHeader) readerutil.SizeReaderAt {
	var compressedSize, uncompressedSize uint32

	if fh.isZip64() {
		compressedSize = uint32max
		uncompressedSize = uint32max
		fh.ReaderVersion = zipVersion45 // requires 4.5 - File uses ZIP64 format extensions
	} else {
		compressedSize = uint32(fh.CompressedSize64)
		uncompressedSize = uint32(fh.UncompressedSize64)
	}

	// Write data descriptor. This is more complicated than one would
	// think, see e.g. comments in zipfile.c:putextended() and
	// http://bugs.sun.com/bugdatabase/view_bug.do?bug_id=7073588.
	// The approach here is to write 8 byte sizes if needed without
	// adding a zip64 extra in the local header (too late anyway).
	var buf []byte
	if fh.isZip64() {
		buf = make([]byte, dataDescriptor64Len)
	} else {
		buf = make([]byte, dataDescriptorLen)
	}
	b := writeBuf(buf)
	b.uint32(dataDescriptorSignature) // de-facto standard, required by OS X
	b.uint32(fh.CRC32)
	if fh.isZip64() {
		b.uint64(fh.CompressedSize64)
		b.uint64(fh.UncompressedSize64)
	} else {
		b.uint32(compressedSize)
		b.uint32(uncompressedSize)
	}

	return bytes.NewReader(buf)
}

func makeCentralDirectory(start int64, dir []*header, comment string) (readerutil.SizeReaderAt, error) {
	var buf bytes.Buffer
	err := writeCentralDirectory(start, dir, &buf, comment)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(buf.Bytes()), nil
}

func writeCentralDirectory(start int64, dir []*header, writer io.Writer, comment string) error {
	// write central directory
	cw := &countWriter{w: writer}
	for _, h := range dir {
		modifiedDate, modifiedTime := timeToMsDosTime(h.Modified)

		var buf [directoryHeaderLen]byte
		b := writeBuf(buf[:])
		b.uint32(uint32(directoryHeaderSignature))
		b.uint16(h.CreatorVersion)
		b.uint16(h.ReaderVersion)
		b.uint16(h.Flags)
		b.uint16(h.Method)
		b.uint16(modifiedTime)
		b.uint16(modifiedDate)
		b.uint32(h.CRC32)
		if h.isZip64() || h.offset >= uint32max {
			// the file needs a zip64 header. store maxint in both
			// 32 bit size fields (and offset later) to signal that the
			// zip64 extra header should be used.
			b.uint32(uint32max) // compressed size
			b.uint32(uint32max) // uncompressed size

			// append a zip64 extra block to Extra
			var buf [28]byte // 2x uint16 + 3x uint64
			eb := writeBuf(buf[:])
			eb.uint16(zip64ExtraID)
			eb.uint16(24) // size = 3x uint64
			eb.uint64(h.UncompressedSize64)
			eb.uint64(h.CompressedSize64)
			eb.uint64(h.offset)
			h.Extra = append(h.Extra, buf[:]...)
		} else {
			b.uint32(uint32(h.CompressedSize64))
			b.uint32(uint32(h.UncompressedSize64))
		}

		b.uint16(uint16(len(h.Name)))
		b.uint16(uint16(len(h.Extra)))
		b.uint16(uint16(len(h.Comment)))
		b = b[4:] // skip disk number start and internal file attr (2x uint16)
		b.uint32(h.ExternalAttrs)
		if h.offset > uint32max {
			b.uint32(uint32max)
		} else {
			b.uint32(uint32(h.offset))
		}
		if _, err := cw.Write(buf[:]); err != nil {
			return err
		}
		if _, err := io.WriteString(cw, h.Name); err != nil {
			return err
		}
		if _, err := cw.Write(h.Extra); err != nil {
			return err
		}
		if _, err := io.WriteString(cw, h.Comment); err != nil {
			return err
		}
	}
	size := uint64(cw.count)
	end := uint64(start) + size

	records := uint64(len(dir))
	offset := uint64(start)

	if records >= uint16max || size >= uint32max || offset >= uint32max {
		var buf [directory64EndLen + directory64LocLen]byte
		b := writeBuf(buf[:])

		// zip64 end of central directory record
		b.uint32(directory64EndSignature)
		b.uint64(directory64EndLen - 12) // length minus signature (uint32) and length fields (uint64)
		b.uint16(zipVersion45)           // version made by
		b.uint16(zipVersion45)           // version needed to extract
		b.uint32(0)                      // number of this disk
		b.uint32(0)                      // number of the disk with the start of the central directory
		b.uint64(records)                // total number of entries in the central directory on this disk
		b.uint64(records)                // total number of entries in the central directory
		b.uint64(size)                   // size of the central directory
		b.uint64(offset)                 // offset of start of central directory with respect to the starting disk number

		// zip64 end of central directory locator
		b.uint32(directory64LocSignature)
		b.uint32(0)           // number of the disk with the start of the zip64 end of central directory
		b.uint64(uint64(end)) // relative offset of the zip64 end of central directory record
		b.uint32(1)           // total number of disks

		if _, err := cw.Write(buf[:]); err != nil {
			return err
		}

		// store max values in the regular end record to signal that
		// that the zip64 values should be used instead
		records = uint16max
		size = uint32max
		offset = uint32max
	}

	// write end record
	var buf [directoryEndLen]byte
	b := writeBuf(buf[:])
	b.uint32(uint32(directoryEndSignature))
	b = b[4:]                      // skip over disk number and first disk number (2x uint16)
	b.uint16(uint16(records))      // number of entries this disk
	b.uint16(uint16(records))      // number of entries total
	b.uint32(uint32(size))         // size of directory
	b.uint32(uint32(offset))       // start of directory
	b.uint16(uint16(len(comment))) // byte size of EOCD comment
	if _, err := cw.Write(buf[:]); err != nil {
		return err
	}
	if _, err := io.WriteString(cw, comment); err != nil {
		return err
	}

	return nil
}
