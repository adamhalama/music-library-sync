package mediametadata

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type tags struct {
	head, tail []byte
	start, end int64
}

func id3Tags(path string) (tags, error) {
	var result tags
	f, err := os.Open(path)
	if err != nil {
		return result, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return result, err
	}
	result.end = info.Size()
	var head [10]byte
	if info.Size() >= 10 {
		_, err = f.ReadAt(head[:], 0)
		if err != nil {
			return result, err
		}
		if string(head[:3]) == "ID3" {
			if head[3] < 2 || head[3] > 4 {
				return result, fmt.Errorf("unsupported ID3 version")
			}
			n := int64(0)
			for _, b := range head[6:] {
				if b&128 != 0 {
					return result, fmt.Errorf("invalid ID3 length")
				}
				n = (n << 7) | int64(b)
			}
			n += 10
			if head[3] == 4 && head[5]&16 != 0 {
				n += 10
			}
			if n > info.Size() || n > maxMetadata {
				return result, fmt.Errorf("invalid ID3 size")
			}
			result.head = make([]byte, n)
			if _, err = f.ReadAt(result.head, 0); err != nil {
				return result, err
			}
			result.start = n
		}
	}
	if result.end-result.start >= 128 {
		var tail [128]byte
		_, err = f.ReadAt(tail[:], result.end-128)
		if err != nil {
			return result, err
		}
		if string(tail[:3]) == "TAG" {
			result.tail = append([]byte(nil), tail[:]...)
			result.end -= 128
		}
	}
	if result.end-result.start >= 32 {
		var footer [32]byte
		_, err = f.ReadAt(footer[:], result.end-32)
		if err != nil {
			return result, err
		}
		if string(footer[:8]) == "APETAGEX" {
			n := int64(binary.LittleEndian.Uint32(footer[12:16]))
			flags := binary.LittleEndian.Uint32(footer[20:24])
			if flags&0x80000000 != 0 {
				n += 32
			}
			if n < 32 || n > maxMetadata || n > result.end-result.start {
				return result, fmt.Errorf("invalid APE tag size")
			}
			ape := make([]byte, n)
			if _, err = f.ReadAt(ape, result.end-n); err != nil {
				return result, err
			}
			result.tail = append(ape, result.tail...)
			result.end -= n
		}
	}
	return result, nil
}
func preserveID3(original, stage string) error {
	source, err := id3Tags(original)
	if err != nil {
		return err
	}
	dest, err := id3Tags(stage)
	if err != nil {
		return err
	}
	if err = rewrite(stage, source.head, dest.start, dest.end, source.tail); err != nil {
		return err
	}
	got, err := id3Tags(stage)
	if err != nil {
		return err
	}
	if !bytes.Equal(got.head, source.head) || !bytes.Equal(got.tail, source.tail) {
		return fmt.Errorf("complete ID3/APE metadata verification failed")
	}
	return nil
}
func rewrite(path string, head []byte, start, end int64, tail []byte) error {
	input, err := os.Open(path)
	if err != nil {
		return err
	}
	defer input.Close()
	out, err := os.CreateTemp(filepath.Dir(path), ".udl-metadata-*")
	if err != nil {
		return err
	}
	defer os.Remove(out.Name())
	defer out.Close()
	if _, err = out.Write(head); err != nil {
		return err
	}
	if _, err = io.Copy(out, io.NewSectionReader(input, start, end-start)); err != nil {
		return err
	}
	if _, err = out.Write(tail); err != nil {
		return err
	}
	if err = out.Sync(); err != nil {
		return err
	}
	if err = out.Close(); err != nil {
		return err
	}
	return os.Rename(out.Name(), path)
}
