// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package zipserve provides support for serving zip archives over HTTP,
allowing range queries and resumable downloads. To be able to do that, it requires
to know CRC32 of the uncompressed data, compressed and uncompressed size of files in advance, which must be
supplied by the user. The actual file data is fetched on demand from user-provided
ReaderAt allowing it to be fetched remotely.

See: https://www.pkware.com/appnote, https://golang.org/pkg/archive/zip/

This package does not support disk spanning.
*/
package zipserve

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Template defines contents and options of a ZIP archive.
type Template struct {
	// Prefix is the content at the beginning of the file before ZIP entries.
	//
	// It may be used to create self-extracting archives, for example.
	//
	// Prefix may implement ReaderAt interface from this package, in that case
	// Prefix's ReadAtContext method will be called instead of ReadAt.
	Prefix io.ReaderAt

	// PrefixSize is size of Prefix in bytes.
	PrefixSize int64

	// Entries is a list of files in the archive.
	Entries []*FileHeader

	// Comment is archive comment text.
	//
	// It may be up to 64K long.
	Comment string

	// CreateTime is the last modified time of the archive.
	//
	// It is used to populate Last-Modified HTTP header.
	// The maximum Modified time of the archive entries will be used if CreateTime is zero time.
	CreateTime time.Time
}

// Archive represents the ZIP file data to be downloaded by the user.
//
// It is a ReaderAt, so allows concurrent access to different byte ranges of the archive.
type Archive struct {
	parts      multiReaderAt
	createTime time.Time
	etag       string
}

// NewArchive creates a new Archive from a Template.
//
// The archive stores the archive metadata (such as list of files) in memory, while actual file data is fetched on
// demand. Apart from other fields required when using archive/zip, all entries in the template must have
// CRC32, UncompressedSize64 and CompressedSize64 set to correct values in advance.
//
// The template becomes owned by the archive. The archive will use and modify the template as necessary, so the caller
// should not use the template after the call to NewArchive. This includes all FileHeader instances in Entries.
func NewArchive(t *Template) (*Archive, error) {
	return newArchive(t, bufferView, nil)
}

type bufferViewFunc func(content func(w io.Writer) error) (sizeReaderAt, error)

func bufferView(content func(w io.Writer) error) (sizeReaderAt, error) {
	var buf bytes.Buffer

	err := content(&buf)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(buf.Bytes()), nil
}

func readerAt(r io.ReaderAt) ReaderAt {
	if v, ok := r.(ReaderAt); ok {
		return v
	}
	return ignoreContext{r: r}
}

func newArchive(t *Template, view bufferViewFunc, testHookCloseSizeOffset func(size, offset uint64)) (*Archive, error) {
	if len(t.Comment) > uint16max {
		return nil, errors.New("comment too long")
	}

	ar := new(Archive)
	dir := make([]*header, 0, len(t.Entries))
	etagHash := md5.New()

	if t.Prefix != nil {
		ar.parts.add(readerAt(t.Prefix), t.PrefixSize)

		var buf [8]byte
		binary.LittleEndian.PutUint64(buf[:], uint64(t.PrefixSize))
		etagHash.Write(buf[:])
	}

	var maxTime time.Time

	for _, entry := range t.Entries {
		prepareEntry(entry)
		dir = append(dir, &header{FileHeader: entry, offset: uint64(ar.parts.size)})
		header, err := view(func(w io.Writer) error {
			return writeHeader(w, entry)
		})
		if err != nil {
			return nil, err
		}
		ar.parts.addSizeReaderAt(header)
		io.Copy(etagHash, io.NewSectionReader(header, 0, header.Size()))
		if strings.HasSuffix(entry.Name, "/") {
			if entry.Content != nil {
				return nil, errors.New("directory entry non-nil content")
			}
		} else {
			if entry.Content != nil {
				ar.parts.add(readerAt(entry.Content), int64(entry.CompressedSize64))
			} else if entry.CompressedSize64 != 0 {
				return nil, errors.New("empty entry with nonzero length")
			}
			// data descriptor
			dataDescriptor := makeDataDescriptor(entry)
			ar.parts.addSizeReaderAt(bytes.NewReader(dataDescriptor))
			etagHash.Write(dataDescriptor)
		}
		if entry.Modified.After(maxTime) {
			maxTime = entry.Modified
		}
	}

	// capture central directory offset and comment so that content func for central directory
	// may be called multiple times and we don't store reference to t in the closure
	centralDirectoryOffset := ar.parts.size
	comment := t.Comment
	centralDirectory, err := view(func(w io.Writer) error {
		return writeCentralDirectory(centralDirectoryOffset, dir, w, comment, testHookCloseSizeOffset)
	})
	if err != nil {
		return nil, err
	}
	ar.parts.addSizeReaderAt(centralDirectory)
	io.Copy(etagHash, io.NewSectionReader(centralDirectory, 0, centralDirectory.Size()))

	ar.createTime = t.CreateTime
	if ar.createTime.IsZero() {
		ar.createTime = maxTime
	}

	ar.etag = fmt.Sprintf("\"%s\"", hex.EncodeToString(etagHash.Sum(nil)))

	return ar, nil
}

// Size returns the size of the archive in bytes.
func (ar *Archive) Size() int64 { return ar.parts.Size() }

// ReadAt provides the data of the file.
//
// This is same as calling ReadAtContext with context.TODO()
//
// See io.ReaderAt for the interface.
func (ar *Archive) ReadAt(p []byte, off int64) (int, error) {
	return ar.parts.ReadAtContext(context.TODO(), p, off)
}

// ReadAtContext provides the data of the file.
//
// This methods implements ReaderAt interface.
//
// The context is passed to ReadAtContext of individual entries, if they implement it. The context is ignored if an
// entry implements just io.ReaderAt.
func (ar *Archive) ReadAtContext(ctx context.Context, p []byte, off int64) (int, error) {
	return ar.parts.ReadAtContext(ctx, p, off)
}

// ServeHTTP serves the archive over HTTP.
//
// ServeHTTP supports range headers, see http.ServeContent for details.
//
// Content-Type and Etag headers are added automatically if they are not already present
// in the ResponseWriter.
func (ar *Archive) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_, haveType := w.Header()["Content-Type"]
	if !haveType {
		w.Header().Set("Content-Type", "application/zip")
	}

	_, haveEtag := w.Header()["Etag"]
	if !haveEtag {
		w.Header().Set("Etag", ar.etag)
	}

	readseeker := io.NewSectionReader(withContext{r: &ar.parts, ctx: r.Context()}, 0, ar.parts.Size())
	http.ServeContent(w, r, "", ar.createTime, readseeker)
}
