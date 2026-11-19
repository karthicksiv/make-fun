package main

// The git pack format: reading a pack a client pushes (whole objects, OFS_DELTA and REF_DELTA, thin
// packs resolved against the repository) and writing the packs upload-pack answers with.

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
)

const (
	packOfsDelta = 6
	packRefDelta = 7
)

var zlibReaderPool sync.Pool

// inflate decompresses one zlib stream from the front of src into a buffer of exactly `size` bytes
// and reports how many source bytes the stream occupied.
func inflate(src []byte, size int) ([]byte, int, error) {
	br := bytes.NewReader(src)
	var zr io.ReadCloser
	if pooled := zlibReaderPool.Get(); pooled != nil {
		zr = pooled.(io.ReadCloser)
		if err := zr.(zlib.Resetter).Reset(br, nil); err != nil {
			return nil, 0, err
		}
	} else {
		fresh, err := zlib.NewReader(br)
		if err != nil {
			return nil, 0, err
		}
		zr = fresh
	}
	out := make([]byte, size)
	if _, err := io.ReadFull(zr, out); err != nil {
		return nil, 0, fmt.Errorf("inflate: %w", err)
	}
	// Read to the end of the stream so the checksum is consumed and verified.
	var tail [1]byte
	n, err := zr.Read(tail[:])
	if n != 0 {
		return nil, 0, errors.New("inflate: the stream is longer than its declared size")
	}
	if err != nil && err != io.EOF {
		return nil, 0, fmt.Errorf("inflate: %w", err)
	}
	zlibReaderPool.Put(zr)
	return out, len(src) - br.Len(), nil
}

var zlibWriterPool sync.Pool

// deflate compresses data as one zlib stream appended to dst.
func deflate(dst *bytes.Buffer, data []byte) {
	var zw *zlib.Writer
	if pooled := zlibWriterPool.Get(); pooled != nil {
		zw = pooled.(*zlib.Writer)
		zw.Reset(dst)
	} else {
		zw, _ = zlib.NewWriterLevel(dst, zlib.BestSpeed)
	}
	zw.Write(data)
	zw.Close()
	zlibWriterPool.Put(zw)
}

// packEntryHeader appends a pack entry's type-and-size header.
func packEntryHeader(dst []byte, t ObjType, size int) []byte {
	b := byte(t)<<4 | byte(size&0x0f)
	size >>= 4
	for size > 0 {
		dst = append(dst, b|0x80)
		b = byte(size & 0x7f)
		size >>= 7
	}
	return append(dst, b)
}

// PackEntry returns the object as a whole (undeltified) pack entry, building it once.
func (o *Object) PackEntry() []byte {
	o.packMu.Lock()
	if o.packed == nil {
		var buf bytes.Buffer
		buf.Grow(len(o.Data)/2 + 32)
		var head [16]byte
		buf.Write(packEntryHeader(head[:0], o.Type, len(o.Data)))
		deflate(&buf, o.Data)
		o.packed = buf.Bytes()
	}
	packed := o.packed
	o.packMu.Unlock()
	return packed
}

// writePack serialises objects as a version 2 pack.
func writePack(objects []*Object) []byte {
	total := 12 + 20
	entries := make([][]byte, len(objects))
	for i, o := range objects {
		entries[i] = o.PackEntry()
		total += len(entries[i])
	}
	out := make([]byte, 0, total)
	out = append(out, 'P', 'A', 'C', 'K', 0, 0, 0, 2)
	out = binary.BigEndian.AppendUint32(out, uint32(len(objects)))
	for _, e := range entries {
		out = append(out, e...)
	}
	sum := sha1.Sum(out)
	return append(out, sum[:]...)
}

// rawEntry is one entry of an incoming pack before deltas are resolved.
type rawEntry struct {
	offset  int
	kind    byte
	data    []byte // inflated content: the object, or the delta instructions
	baseOfs int    // OFS_DELTA: the base entry's offset in the pack
	baseRef SHA    // REF_DELTA: the base object's id
	raw     []byte // the entry's bytes in the pack, for whole objects
	obj     *Object
}

// parsePack reads a pack and returns its objects, deltas applied. `external` finds a base that a thin
// pack leaves out. The pack's trailing checksum is verified.
func parsePack(pack []byte, external func(SHA) *Object) ([]*Object, error) {
	if len(pack) < 12+20 {
		return nil, errors.New("pack is truncated")
	}
	if string(pack[:4]) != "PACK" {
		return nil, errors.New("pack has a bad signature")
	}
	version := binary.BigEndian.Uint32(pack[4:8])
	if version != 2 && version != 3 {
		return nil, fmt.Errorf("pack version %d is not supported", version)
	}
	count := int(binary.BigEndian.Uint32(pack[8:12]))
	body := pack[:len(pack)-20]
	sum := sha1.Sum(body)
	if !bytes.Equal(sum[:], pack[len(pack)-20:]) {
		return nil, errors.New("pack checksum mismatch")
	}

	entries := make([]rawEntry, 0, count)
	byOffset := make(map[int]int, count)
	pos := 12
	for i := 0; i < count; i++ {
		if pos >= len(body) {
			return nil, errors.New("pack ends before its last object")
		}
		start := pos
		c := body[pos]
		pos++
		kind := (c >> 4) & 7
		size := int(c & 0x0f)
		shift := uint(4)
		for c&0x80 != 0 {
			if pos >= len(body) {
				return nil, errors.New("pack entry header is truncated")
			}
			c = body[pos]
			pos++
			size |= int(c&0x7f) << shift
			shift += 7
		}
		e := rawEntry{offset: start, kind: kind}
		switch kind {
		case byte(ObjCommit), byte(ObjTree), byte(ObjBlob), byte(ObjTag):
		case packOfsDelta:
			if pos >= len(body) {
				return nil, errors.New("pack delta offset is truncated")
			}
			c = body[pos]
			pos++
			ofs := int(c & 0x7f)
			for c&0x80 != 0 {
				if pos >= len(body) {
					return nil, errors.New("pack delta offset is truncated")
				}
				c = body[pos]
				pos++
				ofs = ((ofs + 1) << 7) | int(c&0x7f)
			}
			e.baseOfs = start - ofs
		case packRefDelta:
			if pos+20 > len(body) {
				return nil, errors.New("pack delta base is truncated")
			}
			copy(e.baseRef[:], body[pos:pos+20])
			pos += 20
		default:
			return nil, fmt.Errorf("pack entry has unknown type %d", kind)
		}
		data, used, err := inflate(body[pos:], size)
		if err != nil {
			return nil, err
		}
		pos += used
		e.data = data
		if kind <= byte(ObjTag) {
			e.raw = pack[start:pos]
		}
		byOffset[start] = len(entries)
		entries = append(entries, e)
	}

	// Whole objects first, then deltas until none is left that a resolved base can serve.
	byID := make(map[SHA]*Object, count)
	pending := 0
	for i := range entries {
		e := &entries[i]
		if e.kind <= byte(ObjTag) {
			o := &Object{Type: ObjType(e.kind), Data: e.data}
			o.ID = hashObject(o.Type, o.Data)
			o.packed = append([]byte(nil), e.raw...)
			e.obj = o
			byID[o.ID] = o
		} else {
			pending++
		}
	}
	for pending > 0 {
		progressed := false
		for i := range entries {
			e := &entries[i]
			if e.obj != nil {
				continue
			}
			var base *Object
			if e.kind == packOfsDelta {
				idx, ok := byOffset[e.baseOfs]
				if !ok {
					return nil, errors.New("pack delta names a base offset that is no entry")
				}
				base = entries[idx].obj
			} else {
				base = byID[e.baseRef]
				if base == nil && external != nil {
					base = external(e.baseRef)
				}
			}
			if base == nil {
				continue
			}
			data, err := applyDelta(base.Data, e.data)
			if err != nil {
				return nil, err
			}
			o := &Object{Type: base.Type, Data: data}
			o.ID = hashObject(o.Type, o.Data)
			e.obj = o
			byID[o.ID] = o
			pending--
			progressed = true
		}
		if !progressed {
			return nil, errors.New("pack holds a delta whose base is missing")
		}
	}
	out := make([]*Object, len(entries))
	for i := range entries {
		out[i] = entries[i].obj
	}
	return out, nil
}

// applyDelta rebuilds an object from its base and git delta instructions.
func applyDelta(base, delta []byte) ([]byte, error) {
	pos := 0
	readSize := func() (int, error) {
		size, shift := 0, uint(0)
		for {
			if pos >= len(delta) {
				return 0, errors.New("delta header is truncated")
			}
			c := delta[pos]
			pos++
			size |= int(c&0x7f) << shift
			shift += 7
			if c&0x80 == 0 {
				return size, nil
			}
		}
	}
	baseSize, err := readSize()
	if err != nil {
		return nil, err
	}
	if baseSize != len(base) {
		return nil, errors.New("delta was made against a base of another size")
	}
	resultSize, err := readSize()
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, resultSize)
	for pos < len(delta) {
		op := delta[pos]
		pos++
		if op&0x80 != 0 {
			var offset, size int
			for i := uint(0); i < 4; i++ {
				if op&(1<<i) != 0 {
					if pos >= len(delta) {
						return nil, errors.New("delta copy is truncated")
					}
					offset |= int(delta[pos]) << (8 * i)
					pos++
				}
			}
			for i := uint(0); i < 3; i++ {
				if op&(0x10<<i) != 0 {
					if pos >= len(delta) {
						return nil, errors.New("delta copy is truncated")
					}
					size |= int(delta[pos]) << (8 * i)
					pos++
				}
			}
			if size == 0 {
				size = 0x10000
			}
			if offset < 0 || offset+size > len(base) {
				return nil, errors.New("delta copies past the end of its base")
			}
			out = append(out, base[offset:offset+size]...)
		} else if op != 0 {
			n := int(op)
			if pos+n > len(delta) {
				return nil, errors.New("delta insert is truncated")
			}
			out = append(out, delta[pos:pos+n]...)
			pos += n
		} else {
			return nil, errors.New("delta holds a reserved instruction")
		}
	}
	if len(out) != resultSize {
		return nil, errors.New("delta produced an object of the wrong size")
	}
	return out, nil
}
