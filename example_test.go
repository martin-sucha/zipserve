package zipserve_test

import (
	"net/http"
	"log"
	"path/filepath"
	"os"
	"hash/crc32"
	"io"
	"github.com/martin-sucha/zipserve"
)

func templateFromDir(root string) (*zipserve.Template, error) {
	t := &zipserve.Template{}

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == root || !(info.Mode().IsRegular() || info.Mode().IsDir()){
			return nil
		}
		header, err := zipserve.FileInfoHeader(info)
		if err != nil {
			return err
		}

		relpath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		header.Name = relpath

		if info.Mode().IsRegular() {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			header.Content = file
			crc := crc32.NewIEEE()
			_, err = io.Copy(crc, file)
			if err != nil {
				return err
			}
			header.CRC32 = crc.Sum32()
		} else {
			header.Name = header.Name + "/"
		}

		t.Entries = append(t.Entries, header)

		return nil
	})
	if err != nil {
		return nil, err
	}
	return t, nil
}

func Example() {
	cwd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	t, err := templateFromDir(cwd)
	if err != nil {
		log.Fatal(err)
	}
	archive, err := zipserve.NewArchive(t)
	if err != nil {
		log.Fatal(err)
	}
	http.Handle("/", archive)
	log.Fatal(http.ListenAndServe(":8080", nil))
}