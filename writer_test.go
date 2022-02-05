// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package zipserve

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"io/ioutil"
	"math/rand"
	"os"
	"strings"
	"testing"
	"time"
)

// TODO(adg): a more sophisticated test suite

type WriteTest struct {
	Name   string
	Data   []byte
	Method uint16
	Mode   os.FileMode
}

var writeTests = []WriteTest{
	{
		Name:   "foo",
		Data:   []byte("Rabbits, guinea pigs, gophers, marsupial rats, and quolls."),
		Method: Store,
		Mode:   0666,
	},
	{
		Name:   "bar",
		Data:   nil, // large data set in the test
		Method: Deflate,
		Mode:   0644,
	},
	{
		Name:   "setuid",
		Data:   []byte("setuid file"),
		Method: Deflate,
		Mode:   0755 | os.ModeSetuid,
	},
	{
		Name:   "setgid",
		Data:   []byte("setgid file"),
		Method: Deflate,
		Mode:   0755 | os.ModeSetgid,
	},
	{
		Name:   "symlink",
		Data:   []byte("../link/target"),
		Method: Deflate,
		Mode:   0755 | os.ModeSymlink,
	},
	{
		Name:   "device",
		Data:   []byte("device file"),
		Method: Deflate,
		Mode:   0755 | os.ModeDevice,
	},
	{
		Name:   "chardevice",
		Data:   []byte("char device file"),
		Method: Deflate,
		Mode:   0755 | os.ModeDevice | os.ModeCharDevice,
	},
}

func TestWriter(t *testing.T) {
	largeData := make([]byte, 1<<17)
	if _, err := rand.Read(largeData); err != nil {
		t.Fatal("rand.Read failed:", err)
	}
	writeTests[1].Data = largeData
	defer func() {
		writeTests[1].Data = nil
	}()

	// write a zip file
	tmpl := &Template{}

	for _, wt := range writeTests {
		tmpl.Entries = append(tmpl.Entries, testCreate(t, &wt))
	}

	ar, err := NewArchive(tmpl)
	if err != nil {
		t.Fatal(err)
	}

	// read it back
	r, err := zip.NewReader(ar, ar.Size())
	if err != nil {
		t.Fatal(err)
	}
	for i, wt := range writeTests {
		testReadFile(t, r.File[i], &wt)
	}
}

// TestWriterComment is test for EOCD comment read/write.
func TestWriterComment(t *testing.T) {
	var tests = []struct {
		comment string
		ok      bool
	}{
		{"hi, hello", true},
		{"hi, こんにちわ", true},
		{strings.Repeat("a", uint16max), true},
		{strings.Repeat("a", uint16max+1), false},
	}

	for _, test := range tests {
		// write a zip file
		tmpl := &Template{
			Comment: test.comment,
		}
		ar, err := NewArchive(tmpl)
		if err != nil {
			if test.ok {
				t.Fatalf("unexpected error %v", err)
			}
			continue
		} else {
			if !test.ok {
				t.Fatalf("unexpected success, want error")
			}
		}

		// skip read test in failure cases
		if !test.ok {
			continue
		}

		// read it back
		r, err := zip.NewReader(ar, ar.Size())
		if err != nil {
			t.Fatal(err)
		}
		if r.Comment != test.comment {
			t.Fatalf("Reader.Comment: got %v, want %v", r.Comment, test.comment)
		}
	}
}

func TestWriterUTF8(t *testing.T) {
	var utf8Tests = []struct {
		name    string
		comment string
		nonUTF8 bool
		flags   uint16
	}{
		{
			name:    "hi, hello",
			comment: "in the world",
			flags:   0x8,
		},
		{
			name:    "hi, こんにちわ",
			comment: "in the world",
			flags:   0x808,
		},
		{
			name:    "hi, こんにちわ",
			comment: "in the world",
			nonUTF8: true,
			flags:   0x8,
		},
		{
			name:    "hi, hello",
			comment: "in the 世界",
			flags:   0x808,
		},
		{
			name:    "hi, こんにちわ",
			comment: "in the 世界",
			flags:   0x808,
		},
		{
			name:    "the replacement rune is �",
			comment: "the replacement rune is �",
			flags:   0x808,
		},
		{
			// Name is Japanese encoded in Shift JIS.
			name:    "\x93\xfa\x96{\x8c\xea.txt",
			comment: "in the 世界",
			flags:   0x008, // UTF-8 must not be set
		},
	}

	// write a zip file
	tmpl := &Template{}

	for _, test := range utf8Tests {
		compressed := deflate([]byte{})
		h := &FileHeader{
			Name:               test.name,
			Comment:            test.comment,
			NonUTF8:            test.nonUTF8,
			Method:             Deflate,
			CompressedSize64:   uint64(len(compressed)),
			UncompressedSize64: 0,
			Content:            bytes.NewReader(compressed),
			CRC32:              crc([]byte{}),
		}
		tmpl.Entries = append(tmpl.Entries, h)
	}

	ar, err := NewArchive(tmpl)
	if err != nil {
		t.Fatal(err)
	}

	// read it back
	r, err := zip.NewReader(ar, ar.Size())
	if err != nil {
		t.Fatal(err)
	}
	for i, test := range utf8Tests {
		flags := r.File[i].Flags
		if flags != test.flags {
			t.Errorf("CreateHeader(name=%q comment=%q nonUTF8=%v): flags=%#x, want %#x", test.name, test.comment, test.nonUTF8, flags, test.flags)
		}
	}
}

func TestWriterTime(t *testing.T) {
	var buf bytes.Buffer
	h := &FileHeader{
		Name:     "test.txt",
		Modified: time.Date(2017, 10, 31, 21, 11, 57, 0, time.FixedZone("", int(-7*time.Hour/time.Second))),
	}
	tmpl := &Template{
		Entries: []*FileHeader{h},
	}
	ar, err := NewArchive(tmpl)
	if err != nil {
		t.Fatalf("unexpected NewArchive error: %v", err)
	}
	_, err = io.Copy(&buf, io.NewSectionReader(ar, 0, ar.Size()))
	if err != nil {
		t.Fatalf("unexpected Copy error: %v", err)
	}

	want, err := ioutil.ReadFile("testdata/time-go.zip")
	if err != nil {
		t.Fatalf("unexpected ReadFile error: %v", err)
	}
	if got := buf.Bytes(); !bytes.Equal(got, want) {
		fmt.Printf("%x\n%x\n", got, want)
		t.Error("contents of time-go.zip differ")
	}
}

func TestWriterMissingData(t *testing.T) {
	tmpl := &Template{
		Entries: []*FileHeader{{
			Name:             "file.txt",
			CompressedSize64: 5,
		}},
	}
	_, err := NewArchive(tmpl)
	if err == nil {
		t.Error("expected an error, got nil")
	}
}

func TestWriterOffset(t *testing.T) {
	largeData := make([]byte, 1<<17)
	if _, err := rand.Read(largeData); err != nil {
		t.Fatal("rand.Read failed:", err)
	}
	writeTests[1].Data = largeData
	defer func() {
		writeTests[1].Data = nil
	}()

	// write a zip file
	existingData := []byte{1, 2, 3, 1, 2, 3, 1, 2, 3}
	tmpl := &Template{
		Prefix:     bytes.NewReader(existingData),
		PrefixSize: int64(len(existingData)),
	}

	for _, wt := range writeTests {
		tmpl.Entries = append(tmpl.Entries, testCreate(t, &wt))
	}

	ar, err := NewArchive(tmpl)
	if err != nil {
		t.Fatal(err)
	}

	// read it back
	checkPrefix := make([]byte, len(existingData))
	n, err := ar.ReadAt(checkPrefix, 0)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(existingData) {
		t.Fatalf("unexpected prefix read: %v", n)
	}
	r, err := zip.NewReader(ar, ar.Size())
	if err != nil {
		t.Fatal(err)
	}
	for i, wt := range writeTests {
		testReadFile(t, r.File[i], &wt)
	}
}

func TestWriterDir(t *testing.T) {
	tmpl := &Template{
		Entries: []*FileHeader{
			{Name: "dir/"},
		},
	}
	_, err := NewArchive(tmpl)
	if err != nil {
		t.Errorf("Directory without content: got %v, want nil", err)
	}

	tmpl2 := &Template{
		Entries: []*FileHeader{
			{Name: "dir/", Content: bytes.NewReader([]byte("hello"))},
		},
	}
	_, err = NewArchive(tmpl2)
	if err == nil {
		t.Errorf("Directory with content: got nil error, want non-nil")
	}
}

func TestWriterDirAttributes(t *testing.T) {
	var buf bytes.Buffer
	tmpl := &Template{
		Entries: []*FileHeader{{
			Name:               "dir/",
			Method:             Deflate,
			CompressedSize64:   1234,
			UncompressedSize64: 5678,
		}},
	}
	ar, err := NewArchive(tmpl)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err = io.Copy(&buf, io.NewSectionReader(ar, 0, ar.Size()))
	if err != nil {
		t.Fatalf("Copy error: %v", err)
	}
	b := buf.Bytes()

	var sig [4]byte
	binary.LittleEndian.PutUint32(sig[:], uint32(fileHeaderSignature))

	idx := bytes.Index(b, sig[:])
	if idx == -1 {
		t.Fatal("file header not found")
	}
	b = b[idx:]

	if !bytes.Equal(b[6:10], []byte{0, 0, 0, 0}) { // FileHeader.Flags: 0, FileHeader.Method: 0
		t.Errorf("unexpected method and flags: %v", b[6:10])
	}

	if !bytes.Equal(b[14:26], make([]byte, 12)) { // FileHeader.{CRC32,CompressSize,UncompressedSize} all zero.
		t.Errorf("unexpected crc, compress and uncompressed size to be 0 was: %v", b[14:26])
	}

	binary.LittleEndian.PutUint32(sig[:], uint32(dataDescriptorSignature))
	if bytes.Index(b, sig[:]) != -1 {
		t.Error("there should be no data descriptor")
	}
}

func testCreate(t *testing.T, wt *WriteTest) *FileHeader {
	header := &FileHeader{
		Name:               wt.Name,
		Method:             wt.Method,
		CRC32:              crc(wt.Data),
		UncompressedSize64: uint64(len(wt.Data)),
	}
	if wt.Mode != 0 {
		header.SetMode(wt.Mode)
	}
	if wt.Method == Deflate {
		compressed := deflate(wt.Data)
		header.CompressedSize64 = uint64(len(compressed))
		header.Content = bytes.NewReader(compressed)
	} else {
		header.CompressedSize64 = uint64(len(wt.Data))
		header.Content = bytes.NewReader(wt.Data)
	}
	return header
}

func deflate(data []byte) []byte {
	var compressedData bytes.Buffer
	comp, _ := flate.NewWriter(&compressedData, 5) // level 5 -> err = nil
	io.Copy(comp, bytes.NewReader(data))           // bytes.Buffer does not return non-nil err
	comp.Close()
	return compressedData.Bytes()
}

func crc(data []byte) uint32 {
	hash := crc32.NewIEEE()
	hash.Write(data) // crc32 does not return non-nil err
	return hash.Sum32()
}

func testFileMode(t *testing.T, f *zip.File, want os.FileMode) {
	mode := f.Mode()
	if want == 0 {
		t.Errorf("%s mode: got %v, want none", f.Name, mode)
	} else if mode != want {
		t.Errorf("%s mode: want %v, got %v", f.Name, want, mode)
	}
}

func testReadFile(t *testing.T, f *zip.File, wt *WriteTest) {
	if f.Name != wt.Name {
		t.Fatalf("File name: got %q, want %q", f.Name, wt.Name)
	}
	testFileMode(t, f, wt.Mode)
	rc, err := f.Open()
	if err != nil {
		t.Fatal("opening:", err)
	}
	b, err := ioutil.ReadAll(rc)
	if err != nil {
		t.Fatal("reading:", err)
	}
	err = rc.Close()
	if err != nil {
		t.Fatal("closing:", err)
	}
	if !bytes.Equal(b, wt.Data) {
		t.Errorf("File contents %q, want %q", b, wt.Data)
	}
}
