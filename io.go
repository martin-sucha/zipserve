package zipserve

import (
	"context"
	"fmt"
	"io"
	"sort"
)

// ReaderAt is like io.ReaderAt, but also takes context.
type ReaderAt interface {
	// ReadAtContext has same semantics as ReadAt from io.ReaderAt, but takes context.
	ReadAtContext(ctx context.Context, p []byte, off int64) (n int, err error)
}

type sizeReaderAt interface {
	io.ReaderAt
	Size() int64
}

type offsetAndData struct {
	offset int64
	data   ReaderAt
}

// multiReaderAt is a ReaderAt that joins multiple ReaderAt sequentially together.
type multiReaderAt struct {
	parts []offsetAndData
	size  int64
}

// add a part to the multiContextReader.
// add can be used only before the reader is read from.
func (mcr *multiReaderAt) add(data ReaderAt, size int64) {
	switch {
	case size < 0:
		panic(fmt.Sprintf("size cannot be negative: %v", size))
	case size == 0:
		return
	}
	mcr.parts = append(mcr.parts, offsetAndData{
		offset: mcr.size,
		data:   data,
	})
	mcr.size += size
}

// addSizeReaderAt is like add, but takes sizeReaderAt
func (mcr *multiReaderAt) addSizeReaderAt(r sizeReaderAt) {
	mcr.add(ignoreContext{r: r}, r.Size())
}

// endOffset is offset where the given part ends.
func (mcr *multiReaderAt) endOffset(partIndex int) int64 {
	if partIndex == len(mcr.parts)-1 {
		return mcr.size
	}
	return mcr.parts[partIndex+1].offset
}

func (mcr *multiReaderAt) ReadAtContext(ctx context.Context, p []byte, off int64) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}
	if off >= mcr.size {
		return 0, io.EOF
	}
	// find first part that has data for p
	firstPartIndex := sort.Search(len(mcr.parts), func(i int) bool {
		return mcr.endOffset(i) > off
	})
	for partIndex := firstPartIndex; partIndex < len(mcr.parts) && len(p) > 0; partIndex++ {
		if partIndex > firstPartIndex {
			off = mcr.parts[partIndex].offset
		}
		partRemainingBytes := mcr.endOffset(partIndex) - off
		sizeToRead := int64(len(p))
		if sizeToRead > partRemainingBytes {
			sizeToRead = partRemainingBytes
		}
		n2, err2 := mcr.parts[partIndex].data.ReadAtContext(ctx, p[0:sizeToRead], off-mcr.parts[partIndex].offset)
		n += n2
		if err2 != nil {
			return n, err2
		}
		p = p[n2:]
	}
	if len(p) > 0 {
		// tried reading beyond size
		return n, io.EOF
	}
	return n, nil
}

func (mcr *multiReaderAt) ReadAt(p []byte, off int64) (n int, err error) {
	return mcr.ReadAtContext(context.TODO(), p, off)
}

func (mcr *multiReaderAt) Size() int64 {
	return mcr.size
}

// ignoreContext converts io.ReaderAt to ReaderAt
type ignoreContext struct {
	r io.ReaderAt
}

func (a ignoreContext) ReadAtContext(_ context.Context, p []byte, off int64) (n int, err error) {
	return a.r.ReadAt(p, off)
}

// withContext converts ReaderAt to io.ReaderAt.
//
// While usually we shouldn't store context in a structure, we ensure that withContext lives only within single
// request.
type withContext struct {
	ctx context.Context
	r   ReaderAt
}

func (w withContext) ReadAt(p []byte, off int64) (n int, err error) {
	return w.r.ReadAtContext(w.ctx, p, off)
}
