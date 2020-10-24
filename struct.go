// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package zipserve

import (
	"io"
	"os"
	"path"
	"time"
)

// Compression methods.
const (
	Store   uint16 = 0 // no compression
	Deflate uint16 = 8 // DEFLATE compressed
)

const (
	fileHeaderSignature      = 0x04034b50
	directoryHeaderSignature = 0x02014b50
	directoryEndSignature    = 0x06054b50
	directory64LocSignature  = 0x07064b50
	directory64EndSignature  = 0x06064b50
	dataDescriptorSignature  = 0x08074b50 // de-facto standard; required by OS X Finder
	fileHeaderLen            = 30         // + filename + extra
	directoryHeaderLen       = 46         // + filename + extra + comment
	directoryEndLen          = 22         // + comment
	dataDescriptorLen        = 16         // four uint32: descriptor signature, crc32, compressed size, size
	dataDescriptor64Len      = 24         // descriptor with 8 byte sizes
	directory64LocLen        = 20         //
	directory64EndLen        = 56         // + extra
	extTimeExtraLen          = 9          // 2*SizeOf(uint16) + SizeOf(uint8) + SizeOf(uint32)

	// Constants for the first byte in CreatorVersion.
	creatorFAT    = 0
	creatorUnix   = 3
	creatorNTFS   = 11
	creatorVFAT   = 14
	creatorMacOSX = 19

	// Version numbers.
	zipVersion20 = 20 // 2.0
	zipVersion45 = 45 // 4.5 (reads and writes zip64 archives)

	// Limits for non zip64 files.
	uint16max = (1 << 16) - 1
	uint32max = (1 << 32) - 1

	// Extra header IDs.
	//
	// IDs 0..31 are reserved for official use by PKWARE.
	// IDs above that range are defined by third-party vendors.
	// Since ZIP lacked high precision timestamps (nor a official specification
	// of the timezone used for the date fields), many competing extra fields
	// have been invented. Pervasive use effectively makes them "official".
	//
	// See http://mdfs.net/Docs/Comp/Archiving/Zip/ExtraField
	zip64ExtraID   = 0x0001 // Zip64 extended information
	extTimeExtraID = 0x5455 // Extended timestamp
)

// FileHeader describes a file within a zip file.
// See the zip spec for details.
type FileHeader struct {
	// Name is the name of the file.
	//
	// It must be a relative path, not start with a drive letter (such as "C:"),
	// and must use forward slashes instead of back slashes. A trailing slash
	// indicates that this file is a directory and should have no data.
	Name string

	// Comment is any arbitrary user-defined string shorter than 64KiB.
	Comment string

	// NonUTF8 indicates that Name and Comment are not encoded in UTF-8.
	//
	// By specification, the only other encoding permitted should be CP-437,
	// but historically many ZIP readers interpret Name and Comment as whatever
	// the system's local character encoding happens to be.
	//
	// This flag should only be set if the user intends to encode a non-portable
	// ZIP file for a specific localized region. Otherwise, the Writer
	// automatically sets the ZIP format's UTF-8 flag for valid UTF-8 strings.
	NonUTF8 bool

	CreatorVersion uint16
	ReaderVersion  uint16
	Flags          uint16

	// Method is the compression method. If zero, Store is used.
	Method uint16

	// Modified is the modified time of the file.
	//
	// An extended timestamp (which is timezone-agnostic) is always emitted.
	// The legacy MS-DOS date field is encoded according to the
	// location of the Modified time.
	Modified time.Time

	// CRC32 is a checksum of the uncompressed file data.
	//
	// It can be created using crc32.NewIEEE() from hash/crc32 package.
	CRC32 uint32

	CompressedSize64   uint64
	UncompressedSize64 uint64
	Extra              []byte
	ExternalAttrs      uint32 // Meaning depends on CreatorVersion

	// Content is the (compressed) data of the file.
	//
	// Size of content is specified in the CompressedSize64 field and
	// the content must be compressed using the Method specified.
	// In case Store is used (the default), the compressed data is the same as
	// uncompressed data.
	//
	// Content may implement ReaderAt interface from this package, in that case
	// Content's ReadAtContext method will be called instead of ReadAt.
	Content io.ReaderAt
}

// FileInfo returns an os.FileInfo for the FileHeader.
func (h *FileHeader) FileInfo() os.FileInfo {
	return headerFileInfo{h}
}

// headerFileInfo implements os.FileInfo.
type headerFileInfo struct {
	fh *FileHeader
}

func (fi headerFileInfo) Name() string { return path.Base(fi.fh.Name) }
func (fi headerFileInfo) Size() int64 {
	return int64(fi.fh.UncompressedSize64)
}
func (fi headerFileInfo) IsDir() bool        { return fi.Mode().IsDir() }
func (fi headerFileInfo) ModTime() time.Time { return fi.fh.Modified }
func (fi headerFileInfo) Mode() os.FileMode  { return fi.fh.Mode() }
func (fi headerFileInfo) Sys() interface{}   { return fi.fh }

// FileInfoHeader creates a partially-populated FileHeader from an
// os.FileInfo.
// Because os.FileInfo's Name method returns only the base name of
// the file it describes, it may be necessary to modify the Name field
// of the returned header to provide the full path name of the file.
// If compression is desired, callers should update UncompressedSize64 and set the FileHeader.Method
// field; it is unset by default.
func FileInfoHeader(fi os.FileInfo) (*FileHeader, error) {
	size := fi.Size()
	fh := &FileHeader{
		Name:               fi.Name(),
		UncompressedSize64: uint64(size),
		CompressedSize64:   uint64(size),
		Modified:           fi.ModTime(),
	}
	fh.SetMode(fi.Mode())
	return fh, nil
}

// timeToMsDosTime converts a time.Time to an MS-DOS date and time.
// The resolution is 2s.
// See: https://msdn.microsoft.com/en-us/library/ms724274(v=VS.85).aspx
func timeToMsDosTime(t time.Time) (fDate uint16, fTime uint16) {
	fDate = uint16(t.Day() + int(t.Month())<<5 + (t.Year()-1980)<<9)
	fTime = uint16(t.Second()/2 + t.Minute()<<5 + t.Hour()<<11)
	return
}

const (
	// Unix constants. The specification doesn't mention them,
	// but these seem to be the values agreed on by tools.
	s_IFMT   = 0xf000
	s_IFSOCK = 0xc000
	s_IFLNK  = 0xa000
	s_IFREG  = 0x8000
	s_IFBLK  = 0x6000
	s_IFDIR  = 0x4000
	s_IFCHR  = 0x2000
	s_IFIFO  = 0x1000
	s_ISUID  = 0x800
	s_ISGID  = 0x400
	s_ISVTX  = 0x200

	msdosDir      = 0x10
	msdosReadOnly = 0x01
)

// Mode returns the permission and mode bits for the FileHeader.
func (h *FileHeader) Mode() (mode os.FileMode) {
	switch h.CreatorVersion >> 8 {
	case creatorUnix, creatorMacOSX:
		mode = unixModeToFileMode(h.ExternalAttrs >> 16)
	case creatorNTFS, creatorVFAT, creatorFAT:
		mode = msdosModeToFileMode(h.ExternalAttrs)
	}
	if len(h.Name) > 0 && h.Name[len(h.Name)-1] == '/' {
		mode |= os.ModeDir
	}
	return mode
}

// SetMode changes the permission and mode bits for the FileHeader.
func (h *FileHeader) SetMode(mode os.FileMode) {
	h.CreatorVersion = h.CreatorVersion&0xff | creatorUnix<<8
	h.ExternalAttrs = fileModeToUnixMode(mode) << 16

	// set MSDOS attributes too, as the original zip does.
	if mode&os.ModeDir != 0 {
		h.ExternalAttrs |= msdosDir
	}
	if mode&0200 == 0 {
		h.ExternalAttrs |= msdosReadOnly
	}
}

// isZip64 reports whether the file size exceeds the 32 bit limit
func (h *FileHeader) isZip64() bool {
	return h.CompressedSize64 >= uint32max || h.UncompressedSize64 >= uint32max
}

func msdosModeToFileMode(m uint32) (mode os.FileMode) {
	if m&msdosDir != 0 {
		mode = os.ModeDir | 0777
	} else {
		mode = 0666
	}
	if m&msdosReadOnly != 0 {
		mode &^= 0222
	}
	return mode
}

func fileModeToUnixMode(mode os.FileMode) uint32 {
	var m uint32
	switch mode & os.ModeType {
	default:
		m = s_IFREG
	case os.ModeDir:
		m = s_IFDIR
	case os.ModeSymlink:
		m = s_IFLNK
	case os.ModeNamedPipe:
		m = s_IFIFO
	case os.ModeSocket:
		m = s_IFSOCK
	case os.ModeDevice:
		if mode&os.ModeCharDevice != 0 {
			m = s_IFCHR
		} else {
			m = s_IFBLK
		}
	}
	if mode&os.ModeSetuid != 0 {
		m |= s_ISUID
	}
	if mode&os.ModeSetgid != 0 {
		m |= s_ISGID
	}
	if mode&os.ModeSticky != 0 {
		m |= s_ISVTX
	}
	return m | uint32(mode&0777)
}

func unixModeToFileMode(m uint32) os.FileMode {
	mode := os.FileMode(m & 0777)
	switch m & s_IFMT {
	case s_IFBLK:
		mode |= os.ModeDevice
	case s_IFCHR:
		mode |= os.ModeDevice | os.ModeCharDevice
	case s_IFDIR:
		mode |= os.ModeDir
	case s_IFIFO:
		mode |= os.ModeNamedPipe
	case s_IFLNK:
		mode |= os.ModeSymlink
	case s_IFREG:
		// nothing to do
	case s_IFSOCK:
		mode |= os.ModeSocket
	}
	if m&s_ISGID != 0 {
		mode |= os.ModeSetgid
	}
	if m&s_ISUID != 0 {
		mode |= os.ModeSetuid
	}
	if m&s_ISVTX != 0 {
		mode |= os.ModeSticky
	}
	return mode
}
