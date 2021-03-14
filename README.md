[![Build Status](https://github.com/martin-sucha/zipserve/actions/workflows/build.yaml/badge.svg?branch=master)](https://github.com/martin-sucha/zipserve/actions/workflows/build.yaml?query=branch%3Amaster)
[![Go Report Card](https://goreportcard.com/badge/github.com/martin-sucha/zipserve)](https://goreportcard.com/report/github.com/martin-sucha/zipserve)
[![Go Reference](https://pkg.go.dev/badge/github.com/martin-sucha/zipserve.svg)](https://pkg.go.dev/github.com/martin-sucha/zipserve)

zipserve
========

Package zipserve implements serving virtual zip archives over HTTP,
with support for range queries and resumable downloads. Zipserve keeps only the
archive headers in memory (similar to archive/zip when streaming).
Zipserve fetches file data on demand from user-provided `io.ReaderAt` or `zipserve.ReaderAt`,
so the file data can be fetched from a remote location.
`zipserve.ReaderAt` supports passing request context to the backing store.

The user has to provide CRC32 of the uncompressed data, compressed and uncompressed size of files in advance.
These can be computed for example during file uploads.

Differences to archive/zip
--------------------------

- Deprecated FileHeader fields present in archive/zip (`CompressedSize`, `UncompressedSize`, `ModifiedTime`,
  `ModifiedDate`) were removed in this package. This means the extended time information (unix timestamp) is always
  emitted. If you use `Modified` in archive/zip, the generated file should be identical.

Documentation
-------------

- Package documentation: https://godoc.org/github.com/martin-sucha/zipserve
- ZIP format: https://support.pkware.com/display/PKZIP/APPNOTE

Status of the project
---------------------

The module is stable and supports writing almost everything as archive/zip (see `Differences to archive/zip` above),
so there aren't many commits. I update the module when a new version of Go is released or on request.

License
-------

Three clause BSD (same as Go), see [LICENSE](LICENSE).

Alternatives
------------

- [archive/zip](https://golang.org/pkg/archive/zip/) if you only want to stream archives
- [mod_zip](https://github.com/evanmiller/mod_zip) module for nginx
