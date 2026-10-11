package mediametadata

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func box(kind string, payload []byte) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint32(b, uint32(len(payload)+8))
	copy(b[4:], kind)
	return append(b, payload...)
}
func writeFixture(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0600); err != nil {
		t.Fatal(err)
	}
	return p
}
func TestMP4OpaqueMetadataKeepsFastStartAnd64BitChunkOffsets(t *testing.T) {
	metadata := box("udta", box("meta", append([]byte{0, 0, 0, 0}, box("ilst", box("----", []byte("opaque freeform\x00binary\xff")))...)))
	original := writeFixture(t, "original.m4a", box("moov", metadata))
	for _, fastStart := range []bool{false, true} {
		t.Run(map[bool]string{true: "faststart", false: "tail-moov"}[fastStart], func(t *testing.T) {
			offsets := make([]byte, 16)
			binary.BigEndian.PutUint32(offsets[4:], 1)
			binary.BigEndian.PutUint64(offsets[8:], 0x100000123)
			track := box("trak", box("mdia", box("minf", box("stbl", box("co64", offsets)))))
			moov := box("moov", append(track, box("udta", []byte("old tags"))...))
			media := box("mdat", []byte("encoded audio is immutable"))
			raw := append(media, moov...)
			if fastStart {
				raw = append(moov, media...)
			}
			stage := writeFixture(t, "stage.m4a", raw)
			if err := preserveMP4(original, stage); err != nil {
				t.Fatal(err)
			}
			got, err := mp4Metadata(stage)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, metadata) {
				t.Fatal("opaque tags changed")
			}
			result, err := os.ReadFile(stage)
			if err != nil {
				t.Fatal(err)
			}
			// Every original byte, except the old moov's four-character type, retains its offset.
			expected := append([]byte(nil), raw...)
			offset := len(media) + 4
			if fastStart {
				offset = 4
			}
			copy(expected[offset:], "free")
			if !bytes.Equal(result[:len(raw)], expected) {
				t.Fatal("existing media/chunk offsets changed")
			}
			if !bytes.Contains(result[len(raw):], track) {
				t.Fatal("64-bit chunk table changed")
			}
		})
	}
}
func TestMP4RejectsMalformedAndFragmentedStage(t *testing.T) {
	original := writeFixture(t, "original.m4a", box("moov", nil))
	for name, data := range map[string][]byte{"truncated": {0, 0, 0}, "bad-size": {0, 0, 0, 3, 'm', 'o', 'o', 'v'}, "fragmented": append(box("moov", nil), box("moof", nil)...), "no-moov": box("mdat", nil), "duplicate-moov": append(box("moov", nil), box("moov", nil)...)} {
		t.Run(name, func(t *testing.T) {
			stage := writeFixture(t, "stage.m4a", data)
			if err := preserveMP4(original, stage); err == nil {
				t.Fatal("expected rejection")
			}
			after, _ := os.ReadFile(stage)
			if !bytes.Equal(after, data) {
				t.Fatal("invalid stage modified")
			}
		})
	}
}
func TestMP4RejectsUnsupportedOriginalMetadataBeforeChangingStage(t *testing.T) {
	xmp := box("uuid", append([]byte{0xbe, 0x7a, 0xcf, 0xcb, 0x97, 0xa9, 0x42, 0xe8, 0x9c, 0x71, 0x99, 0x94, 0x91, 0xe3, 0xaf, 0xac}, []byte("<x:xmpmeta>private library annotation</x:xmpmeta>")...))
	moov := box("moov", nil)
	openEnded := box("mdat", []byte("old audio"))
	binary.BigEndian.PutUint32(openEnded, 0)
	nested := box("stbl", nil)
	for i := 0; i < 16; i++ {
		nested = box("mdia", nested)
	}
	for name, raw := range map[string][]byte{
		"adobe-xmp":           append(append([]byte(nil), moov...), xmp...),
		"top-level-meta":      append(append([]byte(nil), moov...), box("meta", nil)...),
		"unknown-top-level":   append(append([]byte(nil), moov...), box("custom", nil)...),
		"moov-uuid":           box("moov", xmp),
		"track-udta":          box("moov", box("trak", box("udta", []byte("private tags")))),
		"nested-track-meta":   box("moov", box("trak", box("mdia", box("meta", nil)))),
		"track-uuid":          box("moov", box("trak", xmp)),
		"unknown-track-atom":  box("moov", box("trak", box("cust", []byte("private metadata")))),
		"excessive-nesting":   box("moov", box("trak", nested)),
		"fragmented":          append(append([]byte(nil), moov...), box("moof", nil)...),
		"fragmented-moov":     box("moov", box("mvex", nil)),
		"open-ended-original": append(append([]byte(nil), moov...), openEnded...),
	} {
		t.Run(name, func(t *testing.T) {
			original := writeFixture(t, "original.m4a", raw)
			staged := append(box("mdat", []byte("new audio")), moov...)
			stage := writeFixture(t, "stage.m4a", staged)
			if err := preserveMP4(original, stage); err == nil {
				t.Fatal("accepted unsupported original layout")
			}
			got, err := os.ReadFile(stage)
			if err != nil || !bytes.Equal(got, staged) {
				t.Fatal("stage changed before unsupported original rejection")
			}
			got, err = os.ReadFile(original)
			if err != nil || !bytes.Equal(got, raw) {
				t.Fatal("original changed")
			}
		})
	}
}
func id3(payload []byte) []byte {
	header := []byte{'I', 'D', '3', 4, 0, 0, 0, 0, 0, 0}
	n := len(payload)
	for i := 9; i >= 6; i-- {
		header[i] = byte(n & 127)
		n >>= 7
	}
	return append(header, payload...)
}
func TestID3PreservesOpaqueFramesAPEv2AndID3v1(t *testing.T) {
	ape := make([]byte, 32)
	copy(ape, "APETAGEX")
	binary.LittleEndian.PutUint32(ape[8:], 2000)
	binary.LittleEndian.PutUint32(ape[12:], 32)
	v1 := make([]byte, 128)
	copy(v1, "TAGexact original title")
	tags := id3([]byte("private binary\x00APIC\xffTXXX"))
	original := writeFixture(t, "original.mp3", append(append(append(tags, []byte("old audio")...), ape...), v1...))
	audio := []byte("new encoded audio")
	stage := writeFixture(t, "stage.mp3", append(id3([]byte("ffmpeg tags")), audio...))
	if err := preserveID3(original, stage); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(stage)
	expected := append(append(append(append([]byte(nil), tags...), audio...), ape...), v1...)
	if !bytes.Equal(got, expected) {
		t.Fatal("opaque tags or encoded audio changed")
	}
}
func riff(kind string, data []byte) []byte {
	raw := make([]byte, 8)
	copy(raw, kind)
	binary.LittleEndian.PutUint32(raw[4:], uint32(len(data)))
	raw = append(raw, data...)
	if len(data)%2 == 1 {
		raw = append(raw, 0)
	}
	return raw
}
func wav(chunks ...[]byte) []byte {
	raw := []byte("RIFF\x00\x00\x00\x00WAVE")
	for _, chunk := range chunks {
		raw = append(raw, chunk...)
	}
	binary.LittleEndian.PutUint32(raw[4:], uint32(len(raw)-8))
	return raw
}
func TestWAVPreservesBWFUnknownChunksWithoutReplacingNewAudioFormat(t *testing.T) {
	tags := append(riff("bext", []byte("opaque BWF")), riff("iXML", []byte("<unknown/>"))...)
	original := writeFixture(t, "original.wav", wav(riff("fmt ", []byte("old fmt")), riff("data", []byte("old audio")), tags))
	structural := riff("fmt ", []byte("new fmt"))
	audio := riff("data", []byte("upgraded PCM"))
	stage := writeFixture(t, "stage.wav", wav(structural, riff("LIST", []byte("ffmpeg")), audio))
	if err := preserveWAV(original, stage); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(stage)
	if !bytes.Equal(got, wav(structural, audio, tags)) {
		t.Fatal("RIFF metadata/audio mismatch")
	}
}
func TestID3RejectsTruncatedTags(t *testing.T) {
	stage := writeFixture(t, "stage.mp3", []byte("ID3\x04\x00\x00\x00\x00\x7f\x7f"))
	if _, err := id3Tags(stage); err == nil {
		t.Fatal("accepted oversized ID3")
	}
}
