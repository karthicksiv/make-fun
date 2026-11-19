package main

// Reading packs: whole objects of every size, both kinds of delta, and streams that are damaged in
// each way the reader checks for.

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/binary"
	"math/rand"
	"strings"
	"testing"
)

// packEntry is one entry of a hand-built pack: its header bytes and the content its stream holds.
type packEntry struct {
	head    []byte
	content []byte
}

func wholeEntry(t ObjType, data []byte) packEntry {
	return packEntry{packEntryHeader(nil, t, len(data)), data}
}

// refDelta is a REF_DELTA entry against the object with the given id.
func refDelta(base SHA, delta []byte) packEntry {
	return packEntry{append(packEntryHeader(nil, packRefDelta, len(delta)), base[:]...), delta}
}

// ofsDelta is an OFS_DELTA entry whose base starts `back` bytes before it.
func ofsDelta(back int, delta []byte) packEntry {
	var digits []byte
	digits = append(digits, byte(back&0x7f))
	for back >>= 7; back > 0; back >>= 7 {
		back--
		digits = append([]byte{0x80 | byte(back&0x7f)}, digits...)
	}
	return packEntry{append(packEntryHeader(nil, packOfsDelta, len(delta)), digits...), delta}
}

// seal puts a pack's signature, version and count in front of body and its SHA-1 behind.
func seal(count int, body []byte) []byte {
	out := []byte("PACK")
	out = binary.BigEndian.AppendUint32(out, 2)
	out = binary.BigEndian.AppendUint32(out, uint32(count))
	out = append(out, body...)
	sum := sha1.Sum(out)
	return append(out, sum[:]...)
}

func zlibOf(data []byte) []byte {
	var b bytes.Buffer
	zw := zlib.NewWriter(&b)
	zw.Write(data)
	zw.Close()
	return b.Bytes()
}

// buildPack lays entries out and returns the pack with the offset of each entry in it.
func buildPack(entries ...packEntry) (pack []byte, offsets []int) {
	var body []byte
	for _, e := range entries {
		offsets = append(offsets, 12+len(body))
		body = append(body, e.head...)
		body = append(body, zlibOf(e.content)...)
	}
	return seal(len(entries), body), offsets
}

// deltaOf writes delta instructions that rebuild result from base: a copy of base[from:to] and then
// the bytes of tail inserted.
func deltaOf(base []byte, from, to int, tail []byte) []byte {
	size := func(dst []byte, n int) []byte {
		for ; n >= 0x80; n >>= 7 {
			dst = append(dst, byte(n)|0x80)
		}
		return append(dst, byte(n))
	}
	out := size(nil, len(base))
	out = size(out, to-from+len(tail))
	if to > from {
		// One copy instruction, its offset and size given in full.
		out = append(out, 0x80|0x0f|0x70, byte(from), byte(from>>8), byte(from>>16), byte(from>>24), byte(to-from), byte((to-from)>>8), byte((to-from)>>16))
	}
	for len(tail) > 0 {
		n := min(len(tail), 127)
		out = append(out, byte(n))
		out = append(out, tail[:n]...)
		tail = tail[n:]
	}
	return out
}

func TestParsePackWholeObjects(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	noise := make([]byte, 70_000)
	rng.Read(noise)
	contents := [][]byte{
		{},
		[]byte("x"),
		[]byte(strings.Repeat("a line of text that compresses well\n", 40)),
		[]byte(strings.Repeat("0123456789abcdef", 20_000)), // several DEFLATE blocks
		noise, // incompressible: stored blocks
	}
	var entries []packEntry
	for _, data := range contents {
		entries = append(entries, wholeEntry(ObjBlob, data))
	}
	pack, _ := buildPack(entries...)
	objects, err := parsePack(pack, nil)
	if err != nil {
		t.Fatalf("parsePack: %v", err)
	}
	if len(objects) != len(contents) {
		t.Fatalf("%d objects, want %d", len(objects), len(contents))
	}
	for i, o := range objects {
		if o.Type != ObjBlob || !bytes.Equal(o.Data, contents[i]) || o.ID != hashObject(ObjBlob, contents[i]) {
			t.Fatalf("object %d did not come back as it went in (%d bytes, want %d)", i, len(o.Data), len(contents[i]))
		}
	}
	// What the forge itself writes, it reads back.
	again, err := parsePack(writePack(objects), nil)
	if err != nil || len(again) != len(objects) {
		t.Fatalf("reading back a written pack: %v", err)
	}
	for i := range again {
		if again[i].ID != objects[i].ID {
			t.Fatalf("object %d changed id across a write and a read", i)
		}
	}
}

func TestParsePackDeltas(t *testing.T) {
	base := []byte(strings.Repeat("the base object's text, line after line\n", 300))
	second := append(append([]byte(nil), base[100:9000]...), []byte("and a new ending\n")...)
	third := append(append([]byte(nil), second[:500]...), []byte(strings.Repeat("grown ", 100))...)
	outside := []byte(strings.Repeat("an object the pack leaves out\n", 50))
	thin := append(append([]byte(nil), outside[30:]...), []byte("thin\n")...)

	baseEntry := wholeEntry(ObjBlob, base)
	secondDelta := deltaOf(base, 100, 9000, []byte("and a new ending\n"))
	thirdDelta := deltaOf(second, 0, 500, []byte(strings.Repeat("grown ", 100)))
	thinDelta := deltaOf(outside, 30, len(outside), []byte("thin\n"))

	// An OFS_DELTA names its base by the distance back to it, which is the length of what lies
	// between: each entry is sized before the next one is made.
	length := func(e packEntry) int { return len(e.head) + len(zlibOf(e.content)) }
	secondEntry := ofsDelta(length(baseEntry), secondDelta)
	thirdEntry := ofsDelta(length(secondEntry), thirdDelta) // a delta of a delta
	pack, _ := buildPack(
		baseEntry,
		secondEntry,
		thirdEntry,
		refDelta(hashObject(ObjBlob, outside), thinDelta),
		refDelta(hashObject(ObjBlob, base), secondDelta),
	)
	external := func(id SHA) *Object {
		if id == hashObject(ObjBlob, outside) {
			return &Object{Type: ObjBlob, ID: id, Data: outside}
		}
		return nil
	}
	objects, err := parsePack(pack, external)
	if err != nil {
		t.Fatalf("parsePack: %v", err)
	}
	want := [][]byte{base, second, third, thin, second}
	if len(objects) != len(want) {
		t.Fatalf("%d objects, want %d", len(objects), len(want))
	}
	for i, o := range objects {
		if o.Type != ObjBlob || !bytes.Equal(o.Data, want[i]) || o.ID != hashObject(ObjBlob, want[i]) {
			t.Fatalf("object %d was not rebuilt correctly (%d bytes, want %d)", i, len(o.Data), len(want[i]))
		}
	}
	// Without the outside base the thin pack cannot be completed.
	if _, err := parsePack(pack, nil); err == nil {
		t.Fatalf("a thin pack was read without its base")
	}
	// A second pack read straight after must not see anything of the first one's spare room.
	if again, err := parsePack(pack, external); err != nil || !bytes.Equal(again[2].Data, third) {
		t.Fatalf("reading the pack a second time: %v", err)
	}
}

func TestParsePackRefusesDamagedStreams(t *testing.T) {
	data := []byte(strings.Repeat("content of the damaged object\n", 20))
	stream := zlibOf(data)
	head := func(size int) []byte { return packEntryHeader(nil, ObjBlob, size) }
	with := func(change func(s []byte) []byte) []byte {
		return change(append([]byte(nil), stream...))
	}
	cases := map[string][]byte{
		"checksum off by one":     append(head(len(data)), with(func(s []byte) []byte { s[len(s)-1] ^= 1; return s })...),
		"content bit flipped":     append(head(len(data)), with(func(s []byte) []byte { s[len(s)/2] ^= 0x10; return s })...),
		"not deflate":             append(head(len(data)), with(func(s []byte) []byte { s[0] = 0x79; return s })...),
		"header not a multiple":   append(head(len(data)), with(func(s []byte) []byte { s[1] ^= 1; return s })...),
		"preset dictionary":       append(head(len(data)), with(func(s []byte) []byte { s[1] = 0xbb; return s })...),
		"checksum cut off":        append(head(len(data)), stream[:len(stream)-3]...),
		"stream cut short":        append(head(len(data)), stream[:len(stream)/2]...),
		"nothing after header":    head(len(data)),
		"size larger than stream": append(head(len(data)+5), stream...),
		"size smaller":            append(head(len(data)-5), stream...),
		"absurd size":             append(head(1<<30), stream...),
	}
	for name, body := range cases {
		if objects, err := parsePack(seal(1, body), nil); err == nil {
			t.Errorf("%s: the pack was read (%d objects)", name, len(objects))
		}
	}
	// The undamaged entry, for contrast, and trailing bytes after the last entry are not an error
	// the reader looks for.
	if objects, err := parsePack(seal(1, append(head(len(data)), stream...)), nil); err != nil || !bytes.Equal(objects[0].Data, data) {
		t.Fatalf("the undamaged pack: %v", err)
	}
}

func BenchmarkParsePushPack(b *testing.B) {
	bodies, _ := pushChain(64)
	packs := make([][]byte, len(bodies))
	for i, body := range bodies {
		packs[i] = body[bytes.Index(body, []byte("PACK")):]
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := parsePack(packs[i%len(packs)], nil); err != nil {
			b.Fatal(err)
		}
	}
}
