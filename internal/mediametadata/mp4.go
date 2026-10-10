package mediametadata

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
)

type atom struct {
	kind                 string
	offset, size, header int64
}

func atoms(f *os.File, start, end int64) ([]atom, error) {
	var result []atom
	for offset := start; offset < end; {
		if len(result) >= 65536 {
			return nil, fmt.Errorf("too many MP4 atoms")
		}
		if end-offset < 8 {
			return nil, fmt.Errorf("truncated MP4 atom header")
		}
		var head [16]byte
		if _, err := f.ReadAt(head[:8], offset); err != nil {
			return nil, err
		}
		size := int64(binary.BigEndian.Uint32(head[:4]))
		header := int64(8)
		if size == 1 {
			if end-offset < 16 {
				return nil, fmt.Errorf("truncated extended atom")
			}
			if _, err := f.ReadAt(head[8:], offset+8); err != nil {
				return nil, err
			}
			u := binary.BigEndian.Uint64(head[8:])
			if u > uint64(end-offset) {
				return nil, fmt.Errorf("oversized atom")
			}
			size = int64(u)
			header = 16
		}
		if size == 0 {
			size = end - offset
		}
		if size < header || size > end-offset {
			return nil, fmt.Errorf("invalid MP4 atom size")
		}
		result = append(result, atom{string(head[4:8]), offset, size, header})
		offset += size
	}
	return result, nil
}
func readAtom(f *os.File, a atom) ([]byte, error) {
	if a.size > maxMetadata {
		return nil, fmt.Errorf("metadata atom exceeds %d bytes", maxMetadata)
	}
	data := make([]byte, int(a.size))
	_, err := f.ReadAt(data, a.offset)
	return data, err
}
func mp4Metadata(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	top, err := atoms(f, 0, info.Size())
	if err != nil {
		return nil, err
	}
	var found bool
	var metadata []byte
	for _, a := range top {
		if a.kind != "moov" {
			continue
		}
		if found {
			return nil, fmt.Errorf("multiple moov atoms")
		}
		found = true
		children, err := atoms(f, a.offset+a.header, a.offset+a.size)
		if err != nil {
			return nil, err
		}
		for _, child := range children {
			if child.kind == "udta" || child.kind == "meta" {
				raw, err := readAtom(f, child)
				if err != nil {
					return nil, err
				}
				metadata = append(metadata, raw...)
				if len(metadata) > maxMetadata {
					return nil, fmt.Errorf("oversized metadata")
				}
			}
		}
	}
	if !found {
		return nil, fmt.Errorf("missing moov atom")
	}
	return metadata, nil
}
func preserveMP4(original, stage string) error {
	metadata, err := mp4Metadata(original)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(stage, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	top, err := atoms(f, 0, info.Size())
	if err != nil {
		return err
	}
	var moov *atom
	for i := range top {
		if top[i].kind == "moof" {
			return fmt.Errorf("fragmented MP4 is unsupported")
		}
		if top[i].size == info.Size()-top[i].offset {
			var size [4]byte
			_, _ = f.ReadAt(size[:], top[i].offset)
			if binary.BigEndian.Uint32(size[:]) == 0 {
				return fmt.Errorf("open-ended MP4 atom is unsupported")
			}
		}
		if top[i].kind == "moov" {
			if moov != nil {
				return fmt.Errorf("multiple staged moov atoms")
			}
			moov = &top[i]
		}
	}
	if moov == nil {
		return fmt.Errorf("missing staged moov")
	}
	children, err := atoms(f, moov.offset+moov.header, moov.offset+moov.size)
	if err != nil {
		return err
	}
	rebuilt := make([]byte, 8)
	copy(rebuilt[4:], "moov")
	for _, child := range children {
		if child.kind == "udta" || child.kind == "meta" {
			continue
		}
		raw, err := readAtom(f, child)
		if err != nil {
			return err
		}
		if len(rebuilt)+len(raw)+len(metadata) > maxMetadata {
			return fmt.Errorf("oversized rebuilt moov")
		}
		rebuilt = append(rebuilt, raw...)
	}
	rebuilt = append(rebuilt, metadata...)
	if len(rebuilt) > maxMetadata {
		return fmt.Errorf("oversized rebuilt moov")
	}
	binary.BigEndian.PutUint32(rebuilt[:4], uint32(len(rebuilt)))
	// Leave all media/chunk offsets unchanged, including fast-start and co64 files:
	// replace the old moov with equal-sized free space and append the rebuilt moov.
	if _, err = f.WriteAt([]byte("free"), moov.offset+4); err != nil {
		return err
	}
	if _, err = f.WriteAt(rebuilt, info.Size()); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	got, err := mp4Metadata(stage)
	if err != nil {
		return err
	}
	if !bytes.Equal(got, metadata) {
		return fmt.Errorf("complete MP4 metadata verification failed")
	}
	return nil
}
