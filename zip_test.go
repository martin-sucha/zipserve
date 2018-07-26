// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Tests that involve both reading and writing.

package zipserve

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
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
	io.Copy(buf, ar)
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
