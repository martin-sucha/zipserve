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
	"net/http"
	"time"
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"encoding/hex"
)

type Template struct {
	Prefix     io.ReaderAt
	PrefixSize int64
	Entries    []*FileHeader
	Comment    string
	CreateTime time.Time
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

type Archive struct {
	data       readerutil.SizeReaderAt
	createTime time.Time
	etag       string
}

func NewArchive(t *Template) (*Archive, error) {
	if len(t.Comment) > uint16max {
		return nil, errors.New("Comment too long")
	}

	dir := make([]*header, 0, len(t.Entries))
	var pb partsBuilder
	etagHash := md5.New()

	if t.Prefix != nil {
		pb.add(&addsize{size: t.PrefixSize, source: t.Prefix})

		var buf [8]byte
		binary.LittleEndian.PutUint64(buf[:], uint64(t.PrefixSize))
		etagHash.Write(buf[:])
	}

	var maxTime time.Time

	for _, entry := range t.Entries {
		prepareEntry(entry)
		dir = append(dir, &header{FileHeader: entry, offset: uint64(pb.offset)})
		header, err := makeLocalFileHeader(entry)
		if err != nil {
			return nil, err
		}
		pb.add(bytes.NewReader(header))
		etagHash.Write(header)
		if entry.Content != nil {
			pb.add(&addsize{size: int64(entry.CompressedSize64), source: entry.Content})
		} else if entry.CompressedSize64 != 0 {
			return nil, errors.New("Empty entry with nonzero length")
		}
		if !strings.HasSuffix(entry.Name, "/") {
			// data descriptor
			dataDescriptor := makeDataDescriptor(entry)
			pb.add(bytes.NewReader(dataDescriptor))
			etagHash.Write(dataDescriptor)
		}
		if entry.Modified.After(maxTime) {
			maxTime = entry.Modified
		}
	}

	centralDirectory, err := makeCentralDirectory(pb.offset, dir, t.Comment)
	if err != nil {
		return nil, err
	}
	pb.add(bytes.NewReader(centralDirectory))
	etagHash.Write(centralDirectory)

	createTime := t.CreateTime
	if createTime.IsZero() {
		createTime = maxTime
	}

	etag := fmt.Sprintf("\"%s\"", hex.EncodeToString(etagHash.Sum(nil)))

	return &Archive{
		data: readerutil.NewMultiReaderAt(pb.parts...),
		createTime: createTime,
		etag: etag}, nil
}

func (ar *Archive) Size() int64 {return ar.data.Size()}
func (ar *Archive) ReadAt(p []byte, off int64) (int, error) {return ar.data.ReadAt(p, off)}

func (ar *Archive) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_, haveType := w.Header()["Content-Type"]
	if !haveType {
		w.Header().Set("Content-Type", "application/zip")
	}

	_, haveEtag := w.Header()["Etag"]
	if !haveEtag {
		w.Header().Set("Etag", ar.etag)
	}

	readseeker := io.NewSectionReader(ar.data, 0, ar.data.Size())
	http.ServeContent(w, r, "", ar.createTime, readseeker)
}

func makeLocalFileHeader(fh *FileHeader) ([]byte, error) {
	var buf bytes.Buffer

	err := writeHeader(&buf, fh)
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func makeCentralDirectory(start int64, dir []*header, comment string) ([]byte, error) {
	var buf bytes.Buffer
	err := writeCentralDirectory(start, dir, &buf, comment)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type addsize struct {
	size int64
	source io.ReaderAt
}

func (as *addsize) Size() int64 {return as.size}
func (as *addsize) ReadAt(p []byte, off int64) (int, error) {return as.source.ReadAt(p, off)}
