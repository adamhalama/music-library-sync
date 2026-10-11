package mediametadata

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// RIFF metadata is copied opaquely, including INFO, ID3, BWF and unknown chunks.
func wavChunks(path string) ([]byte, []byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	var head [12]byte
	if _, err = f.ReadAt(head[:], 0); err != nil {
		return nil, nil, err
	}
	if string(head[:4]) != "RIFF" || string(head[8:]) != "WAVE" {
		return nil, nil, fmt.Errorf("unsupported WAV container (requires RIFF/WAVE)")
	}
	end := int64(binary.LittleEndian.Uint32(head[4:8])) + 8
	if end != info.Size() {
		return nil, nil, fmt.Errorf("invalid RIFF size")
	}
	var metadata []byte
	var structural []byte
	for offset := int64(12); offset < end; {
		var chunk [8]byte
		if end-offset < 8 {
			return nil, nil, fmt.Errorf("truncated RIFF header")
		}
		if _, err = f.ReadAt(chunk[:], offset); err != nil {
			return nil, nil, err
		}
		n := int64(binary.LittleEndian.Uint32(chunk[4:]))
		length := 8 + n + (n % 2)
		if length > end-offset {
			return nil, nil, fmt.Errorf("invalid RIFF chunk")
		}
		kind := string(chunk[:4])
		if kind != "data" {
			if length > maxMetadata {
				return nil, nil, fmt.Errorf("oversized RIFF metadata")
			}
			raw := make([]byte, length)
			if _, err = f.ReadAt(raw, offset); err != nil {
				return nil, nil, err
			}
			if kind == "fmt " || kind == "fact" {
				structural = append(structural, raw...)
			} else {
				metadata = append(metadata, raw...)
				if len(metadata) > maxMetadata {
					return nil, nil, fmt.Errorf("oversized RIFF metadata")
				}
			}
		}
		offset += length
	}
	return metadata, structural, nil
}
func preserveWAV(original, stage string) error {
	metadata, _, err := wavChunks(original)
	if err != nil {
		return err
	}
	_, structural, err := wavChunks(stage)
	if err != nil {
		return err
	}
	f, err := os.Open(stage)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	out, err := os.CreateTemp(filepath.Dir(stage), ".udl-metadata-*")
	if err != nil {
		return err
	}
	defer os.Remove(out.Name())
	defer out.Close()
	head := []byte("RIFF\x00\x00\x00\x00WAVE")
	if _, err = out.Write(head); err != nil {
		return err
	}
	if _, err = out.Write(structural); err != nil {
		return err
	}
	for offset := int64(12); offset < info.Size(); {
		var chunk [8]byte
		if _, err = f.ReadAt(chunk[:], offset); err != nil {
			return err
		}
		n := int64(binary.LittleEndian.Uint32(chunk[4:]))
		length := 8 + n + n%2
		if string(chunk[:4]) == "data" {
			if _, err = io.Copy(out, io.NewSectionReader(f, offset, length)); err != nil {
				return err
			}
		}
		offset += length
	}
	if _, err = out.Write(metadata); err != nil {
		return err
	}
	size, err := out.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	if size-8 > int64(^uint32(0)) {
		return fmt.Errorf("RIFF size exceeds 4 GiB")
	}
	binary.LittleEndian.PutUint32(head[4:8], uint32(size-8))
	if _, err = out.WriteAt(head, 0); err != nil {
		return err
	}
	if err = out.Sync(); err != nil {
		return err
	}
	if err = out.Close(); err != nil {
		return err
	}
	if err = os.Rename(out.Name(), stage); err != nil {
		return err
	}
	got, _, err := wavChunks(stage)
	if err != nil {
		return err
	}
	if !bytes.Equal(got, metadata) {
		return fmt.Errorf("complete WAV metadata verification failed")
	}
	return nil
}
