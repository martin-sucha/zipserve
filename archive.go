// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package zipserve

import (
	"bytes"
	"errors"
	"go4.org/readerutil"
	"strings"
)

type Template struct {
	Prefix  readerutil.SizeReaderAt
	Entries []*FileHeader
	Comment string
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

	dir := make([]*header, 0, len(t.Entries))
	var pb partsBuilder

	if t.Prefix != nil {
		pb.add(t.Prefix)
	}

	for _, entry := range t.Entries {
		prepareEntry(entry)
		dir = append(dir, &header{FileHeader: entry, offset: uint64(pb.offset)})
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
			pb.add(bytes.NewReader(dataDescriptor))
		}
	}

	centralDirectory, err := makeCentralDirectory(pb.offset, dir, t.Comment)
	if err != nil {
		return nil, err
	}
	pb.add(centralDirectory)

	return readerutil.NewMultiReaderAt(pb.parts...), nil
}

func makeLocalFileHeader(fh *FileHeader) (readerutil.SizeReaderAt, error) {
	var buf bytes.Buffer

	err := writeHeader(&buf, fh)
	if err != nil {
		return nil, err
	}

	return bytes.NewReader(buf.Bytes()), nil
}

func makeCentralDirectory(start int64, dir []*header, comment string) (readerutil.SizeReaderAt, error) {
	var buf bytes.Buffer
	err := writeCentralDirectory(start, dir, &buf, comment)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(buf.Bytes()), nil
}

