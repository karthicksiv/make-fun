package main

// pkt-line framing, the unit of every git smart-HTTP exchange, and the side-band multiplexing that
// carries a pack and its progress in one stream.

import (
	"errors"
)

const hexDigits = "0123456789abcdef"

// pktLine appends one data packet: a four-hex-digit length (the four digits included) and the payload.
func pktLine(dst []byte, payload string) []byte {
	n := len(payload) + 4
	dst = append(dst, hexDigits[(n>>12)&15], hexDigits[(n>>8)&15], hexDigits[(n>>4)&15], hexDigits[n&15])
	return append(dst, payload...)
}

func pktBytes(dst []byte, payload []byte) []byte {
	n := len(payload) + 4
	dst = append(dst, hexDigits[(n>>12)&15], hexDigits[(n>>8)&15], hexDigits[(n>>4)&15], hexDigits[n&15])
	return append(dst, payload...)
}

// pktFlush appends a flush packet, pktDelim a delimiter (protocol v2), pktEnd a response end.
func pktFlush(dst []byte) []byte { return append(dst, '0', '0', '0', '0') }
func pktDelim(dst []byte) []byte { return append(dst, '0', '0', '0', '1') }

// Packet kinds a reader meets.
const (
	pktData = iota
	pktFlushKind
	pktDelimKind
	pktEndKind
)

type packet struct {
	kind    int
	payload []byte
}

func hexValue(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

// readPacket reads one packet from the front of buf and returns what follows it.
func readPacket(buf []byte) (packet, []byte, error) {
	if len(buf) < 4 {
		return packet{}, nil, errors.New("pkt-line: truncated length")
	}
	n := 0
	for i := 0; i < 4; i++ {
		v := hexValue(buf[i])
		if v < 0 {
			return packet{}, nil, errors.New("pkt-line: malformed length")
		}
		n = n<<4 | v
	}
	switch n {
	case 0:
		return packet{kind: pktFlushKind}, buf[4:], nil
	case 1:
		return packet{kind: pktDelimKind}, buf[4:], nil
	case 2:
		return packet{kind: pktEndKind}, buf[4:], nil
	case 3:
		return packet{}, nil, errors.New("pkt-line: invalid length 3")
	}
	if n > len(buf) {
		return packet{}, nil, errors.New("pkt-line: truncated payload")
	}
	return packet{kind: pktData, payload: buf[4:n]}, buf[n:], nil
}

// trimLF drops one trailing newline, which git writes on text packets and readers ignore.
func trimLF(b []byte) string {
	if n := len(b); n > 0 && b[n-1] == '\n' {
		return string(b[:n-1])
	}
	return string(b)
}

// sideBand appends data on a side-band channel, split into packets no longer than the limit the
// negotiated capability allows (side-band-64k: 65520 bytes a packet; side-band: 1000).
func sideBand(dst []byte, channel byte, data []byte, large bool) []byte {
	max := 999 - 4
	if large {
		max = 65519 - 4
	}
	for len(data) > 0 {
		chunk := data
		if len(chunk) > max {
			chunk = chunk[:max]
		}
		n := len(chunk) + 5
		dst = append(dst, hexDigits[(n>>12)&15], hexDigits[(n>>8)&15], hexDigits[(n>>4)&15], hexDigits[n&15], channel)
		dst = append(dst, chunk...)
		data = data[len(chunk):]
	}
	return dst
}
