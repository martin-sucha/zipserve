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

Deprecated fields present in archive/zip (32 bit sizes and DOS time) were removed in this package.

Documentation
-------------

- Package documentation: https://godoc.org/github.com/martin-sucha/zipserve
- ZIP format: https://support.pkware.com/display/PKZIP/APPNOTE

License
-------

Three clause BSD (same as Go) for files in this package (see [LICENSE](LICENSE)),
Apache 2.0 for vendored go4.org packages (see [vendor/go4.org/LICENSE](vendor/go4.org/LICENSE)).

Alternatives
------------

- [archive/zip](https://golang.org/pkg/archive/zip/) if you only want to stream archives
- [mod_zip](https://github.com/evanmiller/mod_zip) module for nginx