package zipserve

import (
	"bytes"
	"io"
	"io/ioutil"
	"testing"
)

func TestMultireadseeker_Read(t *testing.T) {
	var pb partsBuilder

	pb.addBytes([]byte{1, 2, 3})
	pb.addBytes([]byte{4, 5, 6, 7, 8, 9, 10})
	pb.addBytes([]byte{11, 12, 13, 14, 15, 16, 17})

	mrs := pb.createReadSeeker()

	read, err := ioutil.ReadAll(mrs)
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}

	expected := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17}
	if bytes.Compare(read, expected) != 0 {
		t.Errorf("Result does not match, read: %v, expected: %v", read, expected)
	}
}

func TestMultireadseeker_Seek_Start(t *testing.T) {
	var pb partsBuilder

	pb.addBytes([]byte{1, 2, 3})
	pb.addBytes([]byte{4, 5, 6, 7, 8, 9, 10})
	pb.addBytes([]byte{11, 12, 13, 14, 15, 16, 17})

	mrs := pb.createReadSeeker()
	mrs.Seek(8, io.SeekStart)

	read, err := ioutil.ReadAll(mrs)
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}

	expected := []byte{9, 10, 11, 12, 13, 14, 15, 16, 17}
	if bytes.Compare(read, expected) != 0 {
		t.Errorf("Result does not match, read: %v, expected: %v", read, expected)
	}
}

func TestMultireadseeker_Seek_Start2(t *testing.T) {
	var pb partsBuilder

	pb.addBytes([]byte{1, 2, 3})
	pb.addBytes([]byte{4, 5, 6, 7, 8, 9, 10})
	pb.addBytes([]byte{11, 12, 13, 14, 15, 16, 17})

	mrs := pb.createReadSeeker()
	// Seek to the boundary between parts
	mrs.Seek(10, io.SeekStart)

	read, err := ioutil.ReadAll(mrs)
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}

	expected := []byte{11, 12, 13, 14, 15, 16, 17}
	if bytes.Compare(read, expected) != 0 {
		t.Errorf("Result does not match, read: %v, expected: %v", read, expected)
	}
}

func TestMultireadseeker_Seek_Start3(t *testing.T) {
	var pb partsBuilder

	pb.addBytes([]byte{1, 2, 3})
	pb.addBytes([]byte{4, 5, 6, 7, 8, 9, 10})
	pb.addBytes([]byte{11, 12, 13, 14, 15, 16, 17})

	mrs := pb.createReadSeeker()
	// Seek to the beginning
	mrs.Seek(0, io.SeekStart)

	read, err := ioutil.ReadAll(mrs)
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}

	expected := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17}
	if bytes.Compare(read, expected) != 0 {
		t.Errorf("Result does not match, read: %v, expected: %v", read, expected)
	}
}

func TestMultireadseeker_Seek_Start4(t *testing.T) {
	var pb partsBuilder

	pb.addBytes([]byte{1, 2, 3})
	pb.addBytes([]byte{4, 5, 6, 7, 8, 9, 10})
	pb.addBytes([]byte{11, 12, 13, 14, 15, 16, 17})

	mrs := pb.createReadSeeker()
	// Seek to the end
	mrs.Seek(17, io.SeekStart)

	read, err := ioutil.ReadAll(mrs)
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}

	expected := []byte(nil)
	if bytes.Compare(read, expected) != 0 {
		t.Errorf("Result does not match, read: %v, expected: %v", read, expected)
	}
}

func TestMultireadseeker_Seek_Start5(t *testing.T) {
	var pb partsBuilder

	pb.addBytes([]byte{1, 2, 3})
	pb.addBytes([]byte{4, 5, 6, 7, 8, 9, 10})
	pb.addBytes([]byte{11, 12, 13, 14, 15, 16, 17})

	mrs := pb.createReadSeeker()
	// Seek to the end
	mrs.Seek(17, io.SeekStart)

	read, err := ioutil.ReadAll(mrs)
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}

	expected := []byte(nil)
	if bytes.Compare(read, expected) != 0 {
		t.Errorf("Result does not match, read: %v, expected: %v", read, expected)
	}
}

func TestMultireadseeker_Seek_End(t *testing.T) {
	var pb partsBuilder

	pb.addBytes([]byte{1, 2, 3})
	pb.addBytes([]byte{4, 5, 6, 7, 8, 9, 10})
	pb.addBytes([]byte{11, 12, 13, 14, 15, 16, 17})

	mrs := pb.createReadSeeker()
	// Seek to the end
	mrs.Seek(0, io.SeekEnd)

	read, err := ioutil.ReadAll(mrs)
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}

	expected := []byte(nil)
	if bytes.Compare(read, expected) != 0 {
		t.Errorf("Result does not match, read: %v, expected: %v", read, expected)
	}
}

func TestMultireadseeker_Seek_End2(t *testing.T) {
	var pb partsBuilder

	pb.addBytes([]byte{1, 2, 3})
	pb.addBytes([]byte{4, 5, 6, 7, 8, 9, 10})
	pb.addBytes([]byte{11, 12, 13, 14, 15, 16, 17})

	mrs := pb.createReadSeeker()
	// Seek to the end
	mrs.Seek(-3, io.SeekEnd)

	read, err := ioutil.ReadAll(mrs)
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}

	expected := []byte{15, 16, 17}
	if bytes.Compare(read, expected) != 0 {
		t.Errorf("Result does not match, read: %v, expected: %v", read, expected)
	}
}

func TestMultireadseeker_Seek_Current(t *testing.T) {
	var pb partsBuilder

	pb.addBytes([]byte{1, 2, 3})
	pb.addBytes([]byte{4, 5, 6, 7, 8, 9, 10})
	pb.addBytes([]byte{11, 12, 13, 14, 15, 16, 17})

	mrs := pb.createReadSeeker()
	mrs.Seek(5, io.SeekCurrent)
	mrs.Seek(-2, io.SeekCurrent)
	mrs.Seek(4, io.SeekCurrent)

	read, err := ioutil.ReadAll(mrs)
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}

	expected := []byte{8, 9, 10, 11, 12, 13, 14, 15, 16, 17}
	if bytes.Compare(read, expected) != 0 {
		t.Errorf("Result does not match, read: %v, expected: %v", read, expected)
	}
}
