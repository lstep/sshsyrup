package main

import (
	"archive/zip"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"math/bits"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type zeroSizefileInfo struct {
	fi os.FileInfo
}

var (
	zipFile   string
	dir       string
	stripData bool
	skip      string
	inputFile string
)

const (
	MTIME = 1
	ATIME = 2
	CTIME = 4
)

func (z zeroSizefileInfo) Sys() interface{}   { return z.fi.Sys() }
func (z zeroSizefileInfo) Size() int64        { return 0 }
func (z zeroSizefileInfo) IsDir() bool        { return z.fi.IsDir() }
func (z zeroSizefileInfo) Name() string       { return z.fi.Name() }
func (z zeroSizefileInfo) Mode() os.FileMode  { return z.fi.Mode() }
func (z zeroSizefileInfo) ModTime() time.Time { return z.fi.ModTime() }

func init() {
	flag.StringVar(&zipFile, "o", "", "Output zip file")
	flag.StringVar(&dir, "p", "", "Starting position for the import path")
	flag.BoolVar(&stripData, "b", true, "Strip file content, if set to true the program will only read metadata from filesystem and skip actual file content in archive.")
	flag.StringVar(&skip, "k", "", "Paths to be skipped for indexing, separated by semicolons")
	flag.StringVar(&inputFile, "i", "", "Input file for building the image")
}

func writeExtraUnixInfo(uid, gid uint32, atime, mtime, ctime int64) (b []byte) {
	b = make([]byte, 15)
	binary.LittleEndian.PutUint16(b, 0x7875)
	binary.LittleEndian.PutUint16(b[2:], 11)
	b[4] = 1
	b[5] = 4
	binary.LittleEndian.PutUint32(b[6:], uid)
	b[10] = 4
	binary.LittleEndian.PutUint32(b[11:], gid)
	var flag uint
	if mtime != 0 {
		flag |= MTIME
	}
	if atime != 0 {
		flag |= ATIME
	}
	if ctime != 0 {
		flag |= CTIME
	}
	if flag > 0 {
		bLen := bits.OnesCount(flag) * 4
		tb := make([]byte, bLen+5)
		binary.LittleEndian.PutUint16(tb, 0x5455)
		binary.LittleEndian.PutUint16(tb[2:], uint16(bLen+1))
		tb[4] = byte(flag)
		pos := 5
		// For compatibility we are going to use uint32 instead of uint64.
		// To be fixed in 2038 :)
		if flag&MTIME != 0 {
			binary.LittleEndian.PutUint32(tb[pos:], uint32(mtime))
			pos += 4
		}
		if flag&ATIME != 0 {
			binary.LittleEndian.PutUint32(tb[pos:], uint32(atime))
			pos += 4
		}
		if flag&CTIME != 0 {
			binary.LittleEndian.PutUint32(tb[pos:], uint32(ctime))
		}
		b = append(b, tb...)
	}
	return
}

func main() {
	flag.Parse()
	if len(dir) == 0 {
		fmt.Println("Missing parameter -p. See -help")
		return
	}
	if len(zipFile) == 0 {
		fmt.Println("Missing parameter -o. See -help")
		return
	}
	skipPath := []string{}
	if len(skip) > 0 {
		skipPath = strings.Split(skip, ";")
	}
	f, err := os.OpenFile(zipFile, os.O_CREATE|os.O_WRONLY, os.ModeExclusive)
	if err != nil {
		if os.IsExist(err) {
			fmt.Println("File already exists")
		} else {
			fmt.Printf("Cannot create file. Reason:%v", err)
		}
		return
	}
	defer f.Close()
	archive := zip.NewWriter(f)
	defer archive.Close()

	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if path == dir {
			return nil
		}
		for _, v := range skipPath {
			if strings.HasPrefix(path, v) {
				return nil
			}
		}
		if info == nil {
			fmt.Printf("Skipping %v for nil FileInfo\n", path)
			return nil
		}
		fmt.Printf("Writing %v (%v)\n", path, info.Name())
		if err != nil {
			fmt.Println(err)
			return nil
		}
		if stripData && !info.IsDir() {
			info = zeroSizefileInfo{fi: info}
		}
		header, err := zip.FileInfoHeader(info)
		header.Name = strings.TrimPrefix(path, dir+"/")
		header.Name = strings.TrimPrefix(path, "/")
		header.Extra = writeExtraUnixInfo(getExtraInfo(info))
		fmt.Printf("Filename to be written:%v\n", header.Name)
		if err != nil {
			fmt.Println(err)
			return nil
		}
		if info.IsDir() {
			header.Name += "/"
		} else if info.Size() > 0 || info.Mode()&os.ModeSymlink != 0 {
			header.Method = zip.Deflate
		}

		filepath.Base(dir)
		w, err := archive.CreateHeader(header)

		if err != nil {
			fmt.Println(err)
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			dst, err := os.Readlink(path)
			if err != nil {
				return nil
			}
			_, err = w.Write([]byte(dst))
			if err != nil {
				return nil
			}
		} else if !stripData {
			file, err := os.Open(path)
			if err != nil {
				fmt.Println(err)
				return nil
			}
			defer file.Close()
			_, err = io.Copy(w, file)
			fmt.Println(err)
			return nil
		}

		return nil
	})
	err = archive.Close()
	if err != nil {
		fmt.Println(err)
	}
}
