package zipserve

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
)

type testCheckContext struct {
	r io.ReaderAt
	f func(ctx context.Context)
}
func (a testCheckContext) ReadAtContext(ctx context.Context, p []byte, off int64) (n int, err error) {
	a.f(ctx)
	return a.r.ReadAt(p, off)
}

func TestMultiReaderAt_ReadAtContext(t *testing.T) {
	tests := []struct {
		name string
		parts []string
		offset int64
		size int64
		expectedResult string
		expectedError string
	}{
		{
			name: "empty",
			parts: nil,
			offset: 0,
			size: 0,
			expectedResult: "",
		},
		{
			name: "empty size out of bounds",
			parts: nil,
			offset: 0,
			size: 1,
			expectedResult: "",
			expectedError: "EOF",
		},
		{
			name: "empty offset out of bounds",
			parts: nil,
			offset: 1,
			size: 1,
			expectedResult: "",
			expectedError: "EOF",
		},
		{
			name: "single part full",
			parts: []string{"abcdefgh"},
			offset: 0,
			size: 8,
			expectedResult: "abcdefgh",
		},
		{
			name: "single part start",
			parts: []string{"abcdefgh"},
			offset: 0,
			size: 3,
			expectedResult: "abc",
		},
		{
			name: "single part middle",
			parts: []string{"abcdefgh"},
			offset: 3,
			size: 3,
			expectedResult: "def",
		},
		{
			name: "single part end",
			parts: []string{"abcdefgh"},
			offset: 4,
			size: 4,
			expectedResult: "efgh",
		},
		{
			name: "single part size out of bounds",
			parts: []string{"abcdefgh"},
			offset: 4,
			size: 10,
			expectedResult: "efgh",
			expectedError: "EOF",
		},
		{
			name: "single part offset out of bounds",
			parts: []string{"abcdefgh"},
			offset: 4,
			size: 10,
			expectedResult: "efgh",
			expectedError: "EOF",
		},
		{
			name: "single part empty",
			parts: []string{"abcdefgh"},
			offset: 0,
			size: 0,
			expectedResult: "",
		},
		{
			name: "multiple parts full",
			parts: []string{"abcdefgh", "ijklm", "nopqrs"},
			offset: 0,
			size: 19,
			expectedResult: "abcdefghijklmnopqrs",
		},
		{
			name: "multiple parts beginning",
			parts: []string{"abcdefgh", "ijklm", "nopqrs"},
			offset: 0,
			size: 4,
			expectedResult: "abcd",
		},
		{
			name: "multiple parts beginning 2",
			parts: []string{"abcdefgh", "ijklm", "nopqrs"},
			offset: 0,
			size: 10,
			expectedResult: "abcdefghij",
		},
		{
			name: "multiple parts middle 1",
			parts: []string{"abcdefgh", "ijklm", "nopqrs"},
			offset: 9,
			size: 3,
			expectedResult: "jkl",
		},
		{
			name: "multiple parts middle 2",
			parts: []string{"abcdefgh", "ijklm", "nopqrs"},
			offset: 6,
			size: 4,
			expectedResult: "ghij",
		},
		{
			name: "multiple parts middle 3",
			parts: []string{"abcdefgh", "ijklm", "nopqrs"},
			offset: 6,
			size: 10,
			expectedResult: "ghijklmnop",
		},
		{
			name: "multiple parts end",
			parts: []string{"abcdefgh", "ijklm", "nopqrs"},
			offset: 6,
			size: 13,
			expectedResult: "ghijklmnopqrs",
		},
		{
			name: "multiple parts end 2",
			parts: []string{"abcdefgh", "ijklm", "nopqrs"},
			offset: 15,
			size: 4,
			expectedResult: "pqrs",
		},
		{
			name: "multiple parts size out of bounds",
			parts: []string{"abcdefgh", "ijklm", "nopqrs"},
			offset: 6,
			size: 30,
			expectedResult: "ghijklmnopqrs",
			expectedError: "EOF",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			type testContextKey struct{}
			ctx := context.WithValue(context.Background(), testContextKey{}, test.name)

			var mcr multiReaderAt
			for i := range test.parts {
				reader := testCheckContext{
					r: bytes.NewReader([]byte(test.parts[i])),
					f: func(ctx context.Context) {
						v := ctx.Value(testContextKey{})
						if v != test.name {
							t.Logf("expected context value to be propagated, got %v", v)
							t.Fail()
						}
					},
				}
				mcr.add(reader, int64(len(test.parts[i])))
			}
			p := make([]byte, test.size)
			n, err := mcr.ReadAtContext(ctx, p, test.offset)
			if n < 0 || n > len(p) {
				t.Log("n out of bounds")
				t.Fail()
			} else {
				result := string(p[:n])
				if test.expectedResult != result {
					t.Logf("expected read %q, but got %q", test.expectedResult, result)
					t.Fail()
				}
				if n < len(p) && err == nil {
					t.Log("short read without error")
					t.Fail()
				}
			}
			if test.expectedError == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			} else {
				switch {
				case err == nil:
					t.Fatalf("expected error %q, but got nil", test.expectedError)
				case err.Error() != test.expectedError:
					t.Fatalf("expected error %q, but got %q", test.expectedError, err.Error())
				}
			}
		})
	}
}

type readWithError struct {
	data []byte
	err error
}

func (r readWithError) ReadAtContext(ctx context.Context, p []byte, off int64) (n int, err error) {
	return copy(p, r.data), r.err
}

func TestMultiReaderAt_ReadAtContextError(t *testing.T) {
	myError := errors.New("my error")
	var mcr multiReaderAt
	mcr.add(ignoreContext{r: bytes.NewReader([]byte("abc"))}, 3)
	mcr.add(readWithError{data: []byte("def"), err: myError}, 10)
	mcr.add(ignoreContext{r: bytes.NewReader([]byte("opqrst"))}, 6)
	p := make([]byte, 10)
	n, err := mcr.ReadAtContext(context.Background(), p, 1)
	if n != 5 {
		t.Logf("expected n=5, got %v", n)
		t.Fail()
	}
	if !errors.Is(err, myError) {
		t.Logf("expected err=%v, got %v", myError, err)
		t.Fail()
	}
}
