[![Build Status](https://travis-ci.com/martin-sucha/zipserve.svg?branch=master)](https://travis-ci.com/martin-sucha/zipserve)
[![Go Report Card](https://goreportcard.com/badge/github.com/martin-sucha/zipserve)](https://goreportcard.com/report/github.com/martin-sucha/zipserve)
[![GoDoc](https://godoc.org/github.com/root-gg/plik?status.svg)](https://godoc.org/github.com/martin-sucha/zipserve)

zipserve
========

Package zipserve provides support for serving zip archives over HTTP,
allowing range queries and resumable downloads. To be able to do that, it requires
to know CRC32 of the uncompressed data, compressed and uncompressed size of files in advance, which must be
supplied by the user. The actual file data is fetched on demand from user-provided
ReaderAt allowing it to be fetched remotely.

Differences to archive/zip:

- Deprecated FileHeader fields present in archive/zip (`CompressedSize`, `UncompressedSize`, `ModifiedTime`,
  `ModifiedDate`) were removed in this package. This means the extended time information (unix timestamp) is always
  emitted. If you use `Modified` in archive/zip, the generated file should be identical.

Documentation
-------------

- Package documentation: https://godoc.org/github.com/martin-sucha/zipserve
- ZIP format: https://support.pkware.com/display/PKZIP/APPNOTE

License
-------

Three clause BSD (same as Go) for files in this package (see [LICENSE](LICENSE)),
Apache 2.0 for readerutil package from go4.org which is used as a dependency.

Alternatives
------------

- [archive/zip](https://golang.org/pkg/archive/zip/) if you only want to stream archives
- [mod_zip](https://github.com/evanmiller/mod_zip) module for nginx
