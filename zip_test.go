// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Tests that involve both reading and writing.

package zipserve

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"go4.org/readerutil"
	"hash/crc32"
	"io"
	"io/ioutil"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestOver65kFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	buf := new(bytes.Buffer)

	const nFiles = (1 << 16) + 42
	tmpl := &Template{
		Entries: make([]*FileHeader, nFiles),
	}
	for i := 0; i < nFiles; i++ {
		tmpl.Entries[i] = &FileHeader{
			Name:    fmt.Sprintf("%d.dat", i),
			Method:  Store, // avoid Issue 6136 and Issue 6138
			Content: bytes.NewReader([]byte(nil)),
		}
	}
	ar, err := NewArchive(tmpl)
	if err != nil {
		t.Fatalf("NewArchive: %v", err)
	}
	io.Copy(buf, io.NewSectionReader(ar, 0, ar.Size()))
	s := buf.String()
	zr, err := zip.NewReader(strings.NewReader(s), int64(len(s)))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	if got := len(zr.File); got != nFiles {
		t.Fatalf("File contains %d files, want %d", got, nFiles)
	}
	for i := 0; i < nFiles; i++ {
		want := fmt.Sprintf("%d.dat", i)
		if zr.File[i].Name != want {
			t.Fatalf("File(%d) = %q, want %q", i, zr.File[i].Name, want)
		}
	}
}

type repeatedByte struct {
	off int64
	b   byte
	n   int64
}

// rleBuffer is a run-length-encoded byte buffer.
// It's an io.Writer (like a bytes.Buffer) and also an io.ReaderAt,
// allowing random-access reads.
type rleBuffer struct {
	buf []repeatedByte
}

func (r *rleBuffer) Size() int64 {
	if len(r.buf) == 0 {
		return 0
	}
	last := &r.buf[len(r.buf)-1]
	return last.off + last.n
}

func (r *rleBuffer) Write(p []byte) (n int, err error) {
	var rp *repeatedByte
	if len(r.buf) > 0 {
		rp = &r.buf[len(r.buf)-1]
		// Fast path, if p is entirely the same byte repeated.
		if lastByte := rp.b; len(p) > 0 && p[0] == lastByte {
			if bytes.Count(p, []byte{lastByte}) == len(p) {
				rp.n += int64(len(p))
				return len(p), nil
			}
		}
	}

	for _, b := range p {
		if rp == nil || rp.b != b {
			r.buf = append(r.buf, repeatedByte{r.Size(), b, 1})
			rp = &r.buf[len(r.buf)-1]
		} else {
			rp.n++
		}
	}
	return len(p), nil
}

func min(x, y int64) int64 {
	if x < y {
		return x
	}
	return y
}

func memset(a []byte, b byte) {
	if len(a) == 0 {
		return
	}
	// Double, until we reach power of 2 >= len(a), same as bytes.Repeat,
	// but without allocation.
	a[0] = b
	for i, l := 1, len(a); i < l; i *= 2 {
		copy(a[i:], a[:i])
	}
}

func (r *rleBuffer) ReadAt(p []byte, off int64) (n int, err error) {
	if len(p) == 0 {
		return
	}
	skipParts := sort.Search(len(r.buf), func(i int) bool {
		part := &r.buf[i]
		return part.off+part.n > off
	})
	parts := r.buf[skipParts:]
	if len(parts) > 0 {
		skipBytes := off - parts[0].off
		for _, part := range parts {
			repeat := int(min(part.n-skipBytes, int64(len(p)-n)))
			memset(p[n:n+repeat], part.b)
			n += repeat
			if n == len(p) {
				return
			}
			skipBytes = 0
		}
	}
	if n != len(p) {
		err = io.ErrUnexpectedEOF
	}
	return
}

// Just testing the rleBuffer used in the Zip64 test above. Not used by the zip code.
func TestRLEBuffer(t *testing.T) {
	b := new(rleBuffer)
	var all []byte
	writes := []string{"abcdeee", "eeeeeee", "eeeefghaaiii"}
	for _, w := range writes {
		b.Write([]byte(w))
		all = append(all, w...)
	}
	if len(b.buf) != 10 {
		t.Fatalf("len(b.buf) = %d; want 10", len(b.buf))
	}

	for i := 0; i < len(all); i++ {
		for j := 0; j < len(all)-i; j++ {
			buf := make([]byte, j)
			n, err := b.ReadAt(buf, int64(i))
			if err != nil || n != len(buf) {
				t.Errorf("ReadAt(%d, %d) = %d, %v; want %d, nil", i, j, n, err, len(buf))
			}
			if !bytes.Equal(buf, all[i:i+j]) {
				t.Errorf("ReadAt(%d, %d) = %q; want %q", i, j, buf, all[i:i+j])
			}
		}
	}
}

func TestZip64(t *testing.T) {
	if testing.Short() {
		t.Skip("slow test; skipping")
	}
	t.Parallel()
	const size = 1 << 32 // before the "END\n" part
	buf := testZip64(t, size)
	testZip64DirectoryRecordLength(buf, t)
}

func TestZip64EdgeCase(t *testing.T) {
	if testing.Short() {
		t.Skip("slow test; skipping")
	}
	t.Parallel()
	// Test a zip file with uncompressed size 0xFFFFFFFF.
	// That's the magic marker for a 64-bit file, so even though
	// it fits in a 32-bit field we must use the 64-bit field.
	// Go 1.5 and earlier got this wrong,
	// writing an invalid zip file.
	const size = 1<<32 - 1 - int64(len("END\n")) // before the "END\n" part
	buf := testZip64(t, size)
	testZip64DirectoryRecordLength(buf, t)
}

// Tests that we generate a zip64 file if the directory at offset
// 0xFFFFFFFF, but not before.
func TestZip64DirectoryOffset(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	t.Parallel()
	const filename = "huge.txt"
	gen := func(wantOff uint64) *Archive {
		tmpl := &Template{}

		testHookCloseSizeOffset := func(size, off uint64) {
			if off != wantOff {
				t.Errorf("central directory offset = %d (%x); want %d", off, off, wantOff)
			}
		}

		size := wantOff - fileHeaderLen - uint64(len(filename)) - dataDescriptorLen - extTimeExtraLen

		tmpl.Entries = append(tmpl.Entries, &FileHeader{
			Name:               filename,
			Method:             Store,
			UncompressedSize64: size,
			CompressedSize64:   size,
			Content:            io.NewSectionReader(&sameBytes{b: 0}, 0, int64(size)),
		})

		archive, err := newArchive(tmpl, bufferView, testHookCloseSizeOffset)
		if err != nil {
			t.Fatal(err)
		}

		return archive
	}
	t.Run("uint32max-2_NoZip64", func(t *testing.T) {
		t.Parallel()
		if suffixIsZip64(t, gen(0xfffffffe)) {
			t.Error("unexpected zip64")
		}
	})
	t.Run("uint32max-1_Zip64", func(t *testing.T) {
		t.Parallel()
		if !suffixIsZip64(t, gen(0xffffffff)) {
			t.Error("expected zip64")
		}
	})
}

// At 16k records, we need to generate a zip64 file.
func TestZip64ManyRecords(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	t.Parallel()
	gen := func(numRec int) *Archive {
		tmpl := &Template{}
		for i := 0; i < numRec; i++ {
			tmpl.Entries = append(tmpl.Entries, &FileHeader{
				Name:   "a.txt",
				Method: Store,
			})
		}
		archive, err := NewArchive(tmpl)
		if err != nil {
			t.Fatal(err)
		}
		return archive
	}
	// 16k-1 records shouldn't make a zip64:
	t.Run("uint16max-1_NoZip64", func(t *testing.T) {
		t.Parallel()
		if suffixIsZip64(t, gen(0xfffe)) {
			t.Error("unexpected zip64")
		}
	})
	// 16k records should make a zip64:
	t.Run("uint16max_Zip64", func(t *testing.T) {
		t.Parallel()
		if !suffixIsZip64(t, gen(0xffff)) {
			t.Error("expected zip64")
		}
	})
}

// suffixSaver is an io.Writer & io.ReaderAt that remembers the last 0
// to 'keep' bytes of data written to it. Call Suffix to get the
// suffix bytes.
type suffixSaver struct {
	keep  int
	buf   []byte
	start int
	size  int64
}

func (ss *suffixSaver) Size() int64 { return ss.size }

var errDiscardedBytes = errors.New("ReadAt of discarded bytes")

func (ss *suffixSaver) ReadAt(p []byte, off int64) (n int, err error) {
	back := ss.size - off
	if back > int64(ss.keep) {
		return 0, errDiscardedBytes
	}
	suf := ss.Suffix()
	n = copy(p, suf[len(suf)-int(back):])
	if n != len(p) {
		err = io.EOF
	}
	return
}

func (ss *suffixSaver) Suffix() []byte {
	if len(ss.buf) < ss.keep {
		return ss.buf
	}
	buf := make([]byte, ss.keep)
	n := copy(buf, ss.buf[ss.start:])
	copy(buf[n:], ss.buf[:])
	return buf
}

func (ss *suffixSaver) Write(p []byte) (n int, err error) {
	n = len(p)
	ss.size += int64(len(p))
	if len(ss.buf) < ss.keep {
		space := ss.keep - len(ss.buf)
		add := len(p)
		if add > space {
			add = space
		}
		ss.buf = append(ss.buf, p[:add]...)
		p = p[add:]
	}
	for len(p) > 0 {
		n := copy(ss.buf[ss.start:], p)
		p = p[n:]
		ss.start += n
		if ss.start == ss.keep {
			ss.start = 0
		}
	}
	return
}

type sizedReaderAt interface {
	io.ReaderAt
	Size() int64
}

func suffixIsZip64(t *testing.T, zip sizedReaderAt) bool {
	d := make([]byte, 1024)
	if _, err := zip.ReadAt(d, zip.Size()-int64(len(d))); err != nil {
		t.Fatalf("ReadAt: %v", err)
	}

	sigOff := findSignatureInBlock(d)
	if sigOff == -1 {
		t.Errorf("failed to find signature in block")
		return false
	}

	dirOff, err := findDirectory64End(zip, zip.Size()-int64(len(d))+int64(sigOff))
	if err != nil {
		t.Fatalf("findDirectory64End: %v", err)
	}
	if dirOff == -1 {
		return false
	}

	d = make([]byte, directory64EndLen)
	if _, err := zip.ReadAt(d, dirOff); err != nil {
		t.Fatalf("ReadAt(off=%d): %v", dirOff, err)
	}

	b := readBuf(d)
	if sig := b.uint32(); sig != directory64EndSignature {
		return false
	}

	size := b.uint64()
	if size != directory64EndLen-12 {
		t.Errorf("expected length of %d, got %d", directory64EndLen-12, size)
	}
	return true
}

func rleView(content func(w io.Writer) error) (readerutil.SizeReaderAt, error) {
	buf := new(rleBuffer)
	err := content(buf)
	if err != nil {
		return nil, err
	}
	return buf, nil
}

// Zip64 is required if the total size of the records is uint32max.
func TestZip64LargeDirectory(t *testing.T) {
	if runtime.GOARCH == "wasm" {
		t.Skip("too slow on wasm")
	}
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	t.Parallel()

	// gen returns an Archive with a wantLen bytes of central directory.
	gen := func(wantLen int64) *Archive {
		testHookCloseSizeOffset := func(size, off uint64) {
			if size != uint64(wantLen) {
				t.Errorf("Close central directory size = %d; want %d", size, wantLen)
			}
		}

		tmpl := &Template{}

		uint16string := strings.Repeat(".", uint16max)
		remain := wantLen
		for remain > 0 {
			commentLen := int(uint16max) - directoryHeaderLen - extTimeExtraLen - 1
			thisRecLen := directoryHeaderLen + extTimeExtraLen + int(uint16max) + commentLen
			if int64(thisRecLen) > remain {
				remove := thisRecLen - int(remain)
				commentLen -= remove
				thisRecLen -= remove
			}
			remain -= int64(thisRecLen)
			tmpl.Entries = append(tmpl.Entries, &FileHeader{
				Name:    uint16string,
				Comment: uint16string[:commentLen],
			})
		}

		archive, err := newArchive(tmpl, rleView, testHookCloseSizeOffset)
		if err != nil {
			t.Fatalf("newArchive: %v", err)
		}
		return archive
	}

	t.Run("uint32max-1_NoZip64", func(t *testing.T) {
		t.Parallel()
		if suffixIsZip64(t, gen(uint32max-1)) {
			t.Error("unexpected zip64")
		}
	})
	t.Run("uint32max_HasZip64", func(t *testing.T) {
		t.Parallel()
		if !suffixIsZip64(t, gen(uint32max)) {
			t.Error("expected zip64")
		}
	})
}

// sizeWithEnd returns a sized ReaderAt of size dots plus "END\n" appended
func sizeWithEnd(size int64) sizedReaderAt {
	return readerutil.NewMultiReaderAt(
		io.NewSectionReader(&sameBytes{b: '.'}, 0, size),
		bytes.NewReader([]byte("END\n")))
}

func testZip64(t testing.TB, size int64) sizedReaderAt {
	const chunkSize = 1024
	chunks := int(size / chunkSize)
	// write size bytes plus "END\n" to a zip file
	data := sizeWithEnd(size)
	crc := crc32.NewIEEE()
	_, err := io.Copy(crc, io.NewSectionReader(data, 0, data.Size()))
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &Template{
		Entries: []*FileHeader{
			{
				Name:               "huge.txt",
				Method:             Store,
				UncompressedSize64: uint64(data.Size()),
				CompressedSize64:   uint64(data.Size()),
				Content:            data,
				CRC32:              crc.Sum32(),
			},
		},
	}

	archive, err := NewArchive(tmpl)
	if err != nil {
		t.Fatal(err)
	}

	// read back zip file and check that we get to the end of it
	r, err := zip.NewReader(archive, archive.Size())
	if err != nil {
		t.Fatal("reader:", err)
	}
	f0 := r.File[0]
	rc, err := f0.Open()
	if err != nil {
		t.Fatal("opening:", err)
	}
	//rc.(*checksumReader).hash = fakeHash32{}
	chunk := make([]byte, chunkSize)
	for i := 0; i < chunks; i++ {
		_, err := io.ReadFull(rc, chunk)
		if err != nil {
			t.Fatal("read:", err)
		}
	}
	if frag := int(size % chunkSize); frag > 0 {
		_, err := io.ReadFull(rc, chunk[:frag])
		if err != nil {
			t.Fatal("read:", err)
		}
	}
	gotEnd, err := ioutil.ReadAll(rc)
	if err != nil {
		t.Fatal("read end:", err)
	}
	end := []byte("END\n")
	if !bytes.Equal(gotEnd, end) {
		t.Errorf("End of zip64 archive %q, want %q", gotEnd, end)
	}
	err = rc.Close()
	if err != nil {
		t.Fatal("closing:", err)
	}
	if size+int64(len("END\n")) >= 1<<32-1 {
		if got, want := f0.UncompressedSize, uint32(uint32max); got != want {
			t.Errorf("UncompressedSize %#x, want %#x", got, want)
		}
	}

	if got, want := f0.UncompressedSize64, uint64(size)+uint64(len(end)); got != want {
		t.Errorf("UncompressedSize64 %#x, want %#x", got, want)
	}

	return archive
}

// Issue 9857
func testZip64DirectoryRecordLength(buf sizedReaderAt, t *testing.T) {
	if !suffixIsZip64(t, buf) {
		t.Fatal("not a zip64")
	}
}

func testValidHeader(h *FileHeader, t *testing.T) {
	data := deflate([]byte("hi"))
	h.Content = bytes.NewReader(data)
	h.CompressedSize64 = uint64(len(data))
	h.UncompressedSize64 = 2
	tmpl := &Template{
		Entries: []*FileHeader{h},
	}

	archive, err := NewArchive(tmpl)
	if err != nil {
		t.Fatalf("got %v, expected nil", err)
	}

	zf, err := zip.NewReader(archive, archive.Size())
	if err != nil {
		t.Fatalf("got %v, expected nil", err)
	}
	zh := zf.File[0].FileHeader
	if zh.Name != h.Name || zh.Method != h.Method || zh.UncompressedSize64 != uint64(len("hi")) {
		t.Fatalf("got %q/%d/%d expected %q/%d/%d", zh.Name, zh.Method, zh.UncompressedSize64, h.Name, h.Method, len("hi"))
	}
}

// Issue 4302.
func TestHeaderInvalidTagAndSize(t *testing.T) {
	const timeFormat = "20060102T150405.000.txt"

	ts := time.Now()
	filename := ts.Format(timeFormat)

	h := FileHeader{
		Name:     filename,
		Method:   Deflate,
		Extra:    []byte(ts.Format(time.RFC3339Nano)), // missing tag and len, but Extra is best-effort parsing
		Modified: ts,
	}

	testValidHeader(&h, t)
}

func TestHeaderTooShort(t *testing.T) {
	h := FileHeader{
		Name:   "foo.txt",
		Method: Deflate,
		Extra:  []byte{zip64ExtraID}, // missing size and second half of tag, but Extra is best-effort parsing
	}
	testValidHeader(&h, t)
}

func TestHeaderTooLongErr(t *testing.T) {
	var headerTests = []struct {
		name    string
		extra   []byte
		wanterr error
	}{
		{
			name:    strings.Repeat("x", 1<<16),
			extra:   []byte{},
			wanterr: errLongName,
		},
		{
			name:    "long_extra",
			extra:   bytes.Repeat([]byte{0xff}, 1<<16),
			wanterr: errLongExtra,
		},
	}

	for _, test := range headerTests {
		h := &FileHeader{
			Name:  test.name,
			Extra: test.extra,
		}
		tmpl := &Template{
			Entries: []*FileHeader{h},
		}
		_, err := NewArchive(tmpl)
		if err != test.wanterr {
			t.Errorf("error=%v, want %v", err, test.wanterr)
		}
	}
}

func TestHeaderIgnoredSize(t *testing.T) {
	h := FileHeader{
		Name:   "foo.txt",
		Method: Deflate,
		Extra:  []byte{zip64ExtraID & 0xFF, zip64ExtraID >> 8, 24, 0, 1, 2, 3, 4, 5, 6, 7, 8, 1, 2, 3, 4, 5, 6, 7, 8, 1, 2, 3, 4, 5, 6, 7, 8, 1, 2, 3, 4, 5, 6, 7, 8}, // bad size but shouldn't be consulted
	}
	testValidHeader(&h, t)
}

// Issue 4393. It is valid to have an extra data header
// which contains no body.
func TestZeroLengthHeader(t *testing.T) {
	h := FileHeader{
		Name:   "extadata.txt",
		Method: Deflate,
		Extra: []byte{
			85, 84, 5, 0, 3, 154, 144, 195, 77, // tag 21589 size 5
			85, 120, 0, 0, // tag 30805 size 0
		},
	}
	testValidHeader(&h, t)
}

// Just benchmarking how fast the Zip64 test above is. Not related to
// our zip performance, since the test above disabled CRC32 and flate.
func BenchmarkZip64Test(b *testing.B) {
	for i := 0; i < b.N; i++ {
		testZip64(b, 1<<26)
	}
}

func BenchmarkZip64TestSizes(b *testing.B) {
	for _, size := range []int64{1 << 12, 1 << 20, 1 << 26} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					testZip64(b, size)
				}
			})
		})
	}
}

func TestSuffixSaver(t *testing.T) {
	const keep = 10
	ss := &suffixSaver{keep: keep}
	ss.Write([]byte("abc"))
	if got := string(ss.Suffix()); got != "abc" {
		t.Errorf("got = %q; want abc", got)
	}
	ss.Write([]byte("defghijklmno"))
	if got := string(ss.Suffix()); got != "fghijklmno" {
		t.Errorf("got = %q; want fghijklmno", got)
	}
	if got, want := ss.Size(), int64(len("abc")+len("defghijklmno")); got != want {
		t.Errorf("Size = %d; want %d", got, want)
	}
	buf := make([]byte, ss.Size())
	for off := int64(0); off < ss.Size(); off++ {
		for size := 1; size <= int(ss.Size()-off); size++ {
			readBuf := buf[:size]
			n, err := ss.ReadAt(readBuf, off)
			if off < ss.Size()-keep {
				if err != errDiscardedBytes {
					t.Errorf("off %d, size %d = %v, %v (%q); want errDiscardedBytes", off, size, n, err, readBuf[:n])
				}
				continue
			}
			want := "abcdefghijklmno"[off : off+int64(size)]
			got := string(readBuf[:n])
			if err != nil || got != want {
				t.Errorf("off %d, size %d = %v, %v (%q); want %q", off, size, n, err, got, want)
			}
		}
	}

}

type sameBytes struct {
	b byte
}

func (s *sameBytes) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = s.b
	}
	return len(p), nil
}

func (s *sameBytes) ReadAt(p []byte, off int64) (int, error) {
	for i := range p {
		p[i] = s.b
	}
	return len(p), nil
}

func findSignatureInBlock(b []byte) int {
	for i := len(b) - directoryEndLen; i >= 0; i-- {
		// defined from directoryEndSignature in struct.go
		if b[i] == 'P' && b[i+1] == 'K' && b[i+2] == 0x05 && b[i+3] == 0x06 {
			// n is length of comment
			n := int(b[i+directoryEndLen-2]) | int(b[i+directoryEndLen-1])<<8
			if n+directoryEndLen+i <= len(b) {
				return i
			}
		}
	}
	return -1
}

// findDirectory64End tries to read the zip64 locator just before the
// directory end and returns the offset of the zip64 directory end if
// found.
func findDirectory64End(r io.ReaderAt, directoryEndOffset int64) (int64, error) {
	locOffset := directoryEndOffset - directory64LocLen
	if locOffset < 0 {
		return -1, nil // no need to look for a header outside the file
	}
	buf := make([]byte, directory64LocLen)
	if _, err := r.ReadAt(buf, locOffset); err != nil {
		return -1, err
	}
	b := readBuf(buf)
	if sig := b.uint32(); sig != directory64LocSignature {
		return -1, nil
	}
	if b.uint32() != 0 { // number of the disk with the start of the zip64 end of central directory
		return -1, nil // the file is not a valid zip64-file
	}
	p := b.uint64()      // relative offset of the zip64 end of central directory record
	if b.uint32() != 1 { // total number of disks
		return -1, nil // the file is not a valid zip64-file
	}
	return int64(p), nil
}

type readBuf []byte

func (b *readBuf) uint8() uint8 {
	v := (*b)[0]
	*b = (*b)[1:]
	return v
}

func (b *readBuf) uint16() uint16 {
	v := binary.LittleEndian.Uint16(*b)
	*b = (*b)[2:]
	return v
}

func (b *readBuf) uint32() uint32 {
	v := binary.LittleEndian.Uint32(*b)
	*b = (*b)[4:]
	return v
}

func (b *readBuf) uint64() uint64 {
	v := binary.LittleEndian.Uint64(*b)
	*b = (*b)[8:]
	return v
}

func (b *readBuf) sub(n int) readBuf {
	b2 := (*b)[:n]
	*b = (*b)[n:]
	return b2
}
