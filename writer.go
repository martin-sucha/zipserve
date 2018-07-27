// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package zipserve

import (
	"encoding/binary"
	"errors"
	"io"
	"unicode/utf8"
)

var (
	errLongName  = errors.New("zip: FileHeader.Name too long")
	errLongExtra = errors.New("zip: FileHeader.Extra too long")
)

type header struct {
	*FileHeader
	offset uint64
}

// detectUTF8 reports whether s is a valid UTF-8 string, and whether the string
// must be considered UTF-8 encoding (i.e., not compatible with CP-437, ASCII,
// or any other common encoding).
func detectUTF8(s string) (valid, require bool) {
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		// Officially, ZIP uses CP-437, but many readers use the system's
		// local character encoding. Most encoding are compatible with a large
		// subset of CP-437, which itself is ASCII-like.
		//
		// Forbid 0x7e and 0x5c since EUC-KR and Shift-JIS replace those
		// characters with localized currency and overline characters.
		if r < 0x20 || r > 0x7d || r == 0x5c {
			if !utf8.ValidRune(r) || (r == utf8.RuneError && size == 1) {
				return false, false
			}
			require = true
		}
	}
	return true, require
}

func writeHeader(w io.Writer, h *FileHeader) error {
	const maxUint16 = 1<<16 - 1
	if len(h.Name) > maxUint16 {
		return errLongName
	}
	if len(h.Extra) > maxUint16 {
		return errLongExtra
	}

	modifiedDate, modifiedTime := timeToMsDosTime(h.Modified)

	var buf [fileHeaderLen]byte
	b := writeBuf(buf[:])
	b.uint32(uint32(fileHeaderSignature))
	b.uint16(h.ReaderVersion)
	b.uint16(h.Flags)
	b.uint16(h.Method)
	b.uint16(modifiedTime)
	b.uint16(modifiedDate)
	b.uint32(0) // since we are writing a data descriptor crc32,
	b.uint32(0) // compressed size,
	b.uint32(0) // and uncompressed size should be zero
	b.uint16(uint16(len(h.Name)))
	b.uint16(uint16(len(h.Extra)))
	if _, err := w.Write(buf[:]); err != nil {
		return err
	}
	if _, err := io.WriteString(w, h.Name); err != nil {
		return err
	}
	_, err := w.Write(h.Extra)
	return err
}

type countWriter struct {
	w     io.Writer
	count int64
}

func (w *countWriter) Write(p []byte) (int, error) {
	n, err := w.w.Write(p)
	w.count += int64(n)
	return n, err
}

type writeBuf []byte

func (b *writeBuf) uint8(v uint8) {
	(*b)[0] = v
	*b = (*b)[1:]
}

func (b *writeBuf) uint16(v uint16) {
	binary.LittleEndian.PutUint16(*b, v)
	*b = (*b)[2:]
}

func (b *writeBuf) uint32(v uint32) {
	binary.LittleEndian.PutUint32(*b, v)
	*b = (*b)[4:]
}

func (b *writeBuf) uint64(v uint64) {
	binary.LittleEndian.PutUint64(*b, v)
	*b = (*b)[8:]
}
