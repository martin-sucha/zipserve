package zipserve

import (
	"bytes"
	"fmt"
	"io"
	"sort"
)

// multireadseeker is a io.ReadSeeker that is composed of multiple ReadSeekers
type multireadseeker struct {
	parts     []part
	offset    int64 // current offset from start
	partIndex int   // index of the part containing offset or len(parts) if at end
	length    int64 // total size of all parts combined
	seekValid bool  // if false, we need to seek in the current part first
}

type part struct {
	offset  int64
	length  int64
	content io.ReadSeeker
}

type partsBuilder struct {
	parts  []part
	offset int64
}

func (pb *partsBuilder) addReadSeeker(content io.ReadSeeker, length int64) {
	if length == 0 {
		return
	}
	if content == nil {
		panic(fmt.Sprintf("content is nil, but length is %v", length))
	}
	pb.parts = append(pb.parts, part{offset: pb.offset, length: length, content: content})
	pb.offset += length
}

func (pb *partsBuilder) addBytes(data []byte) {
	pb.addReadSeeker(bytes.NewReader(data), int64(len(data)))
}

func (pb *partsBuilder) createReadSeeker() io.ReadSeeker {
	return &multireadseeker{parts: pb.parts, length: pb.offset}
}

func (m *multireadseeker) Read(p []byte) (n int, err error) {
	if m.offset >= m.length {
		return 0, io.EOF
	}
	currentPart := &m.parts[m.partIndex]
	partOffset := m.offset - currentPart.offset
	partRemaining := currentPart.length - partOffset
	toRead := int(len(p))
	if int64(toRead) > partRemaining {
		toRead = int(partRemaining)
	}

	if !m.seekValid {
		_, err = currentPart.content.Seek(partOffset, io.SeekStart)
		if err != nil {
			return
		}
		m.seekValid = true
	}

	n, err = currentPart.content.Read(p[:toRead])
	if err == io.EOF && n < toRead {
		err = io.ErrUnexpectedEOF
	}

	m.offset += int64(n)
	if int64(n) == partRemaining {
		if err == io.EOF && m.partIndex < len(m.parts)-1 {
			// Don't return EOF, we have more parts to go
			err = nil
		}
		m.partIndex += 1
		m.seekValid = false
	}
	return
}

func (m *multireadseeker) Seek(offset int64, whence int) (int64, error) {
	var newOffset int64
	switch whence {
	case io.SeekStart:
		newOffset = offset
	case io.SeekCurrent:
		newOffset = m.offset + offset
	case io.SeekEnd:
		newOffset = m.length + offset
	}
	if newOffset > m.length {
		newOffset = m.length
	}
	if newOffset < 0 {
		return 0, fmt.Errorf("seek offset %d is before start", newOffset)
	}
	m.offset = newOffset

	m.partIndex = sort.Search(len(m.parts), func(i int) bool { return m.parts[i].offset+m.parts[i].length > newOffset })
	m.seekValid = false

	return newOffset, nil
}
