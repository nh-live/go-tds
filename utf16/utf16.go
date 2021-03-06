// Copyright 2013 Govert Versluis.  All rights reserved.
// Parts copyright 2010 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package utf16 implements encoding and decoding of UTF-16 sequences.
package utf16

const (
	replacementChar = '\uFFFD'     // Unicode replacement character
	maxRune         = '\U0010FFFF' // Maximum valid Unicode code point.
)

const (
	// 0xd800-0xdc00 encodes the high 10 bits of a pair.
	// 0xdc00-0xe000 encodes the low 10 bits of a pair.
	// the value is those 20 bits plus 0x10000.
	surr1 = 0xd800
	surr2 = 0xdc00
	surr3 = 0xe000

	surrSelf = 0x10000
)

// IsSurrogate returns true if the specified Unicode code point
// can appear in a surrogate pair.
func IsSurrogate(r rune) bool {
	return surr1 <= r && r < surr3
}

// DecodeRune returns the UTF-16 decoding of a surrogate pair.
// If the pair is not a valid UTF-16 surrogate pair, DecodeRune returns
// the Unicode replacement code point U+FFFD.
func DecodeRune(r1, r2 rune) rune {
	if surr1 <= r1 && r1 < surr2 && surr2 <= r2 && r2 < surr3 {
		return (rune(r1)-surr1)<<10 | (rune(r2) - surr2) + 0x10000
	}
	return replacementChar
}

// EncodeRune returns the UTF-16 surrogate pair r1, r2 for the given rune.
// If the rune is not a valid Unicode code point or does not need encoding,
// EncodeRune returns U+FFFD, U+FFFD.
func EncodeRune(r rune) (r1, r2 rune) {
	if r < surrSelf || r > maxRune || IsSurrogate(r) {
		return replacementChar, replacementChar
	}
	r -= surrSelf
	return surr1 + (r>>10)&0x3ff, surr2 + r&0x3ff
}

// Encode returns the UTF-16 encoding of the specified string str.
func Encode(s string) []byte {	
	n := len(s)
	for _, v := range s {
		if v >= surrSelf {
			n++
		}
	}

	a := make([]byte, n * 2)
	n = 0
	for _, v := range s {
		switch {
		case v < 0, surr1 <= v && v < surr3, v > maxRune:
			v = replacementChar
			fallthrough
		case v < surrSelf:
			a[n] = byte(v)
			a[n + 1] = byte(v >> 8)
			n += 2
		default:
			r1, r2 := EncodeRune(v)
			a[n] = byte(r1)
			a[n + 1] = byte(r1 >> 8)
			a[n + 2] = byte(r2)
			a[n + 3] = byte(r2 >> 8)
			n += 4
		}
	}
	return a[0:n]
}

// Decode returns the string represented by the UTF-16 encoding s.
func Decode(s []byte) string {
	a := make([]rune, len(s) / 2)
	n := 0
	for i := 0; i < len(s); i+=2 {
		switch r := MakeUint16(s[i], s[i + 1]); {
		case surr1 <= r && r < surr2 && i+3 < len(s) &&
			surr2 <= MakeUint16(s[i+2], s[i+3]) && MakeUint16(s[i+2], s[i+3]) < surr3:
			// valid surrogate sequence
			a[n] = DecodeRune(rune(r), rune(MakeUint16(s[i+2], s[i+3])))
			i++
			n++
		case surr1 <= r && r < surr3:
			// invalid surrogate sequence
			a[n] = replacementChar
			n++
		default:
			// normal rune
			a[n] = rune(r)
			n++
		}
	}
	return string(a[0:n])
}

func MakeUint16(l, h byte) uint16 {
	return uint16(l) + (uint16(h) << 8);
}

func SplitUint16(v uint16) (byte, byte) {
	return byte(v), byte(v >> 8)
}