package midi

import "errors"

const (
	// MaxVarint is the largest value that can be represented by a MIDI
	// variable-length quantity (4 bytes, 7 bits each).
	MaxVarint = 0x0FFFFFFF
	// MaxTimeDelta is the largest value a delta-time can hold.
	MaxTimeDelta = MaxVarint
)

var (
	// ErrVarintTooLong reports a variable-length quantity made of more than 4
	// bytes.
	ErrVarintTooLong = errors.New("variable-length quantity exceeds 4 bytes")
	// ErrVarintTruncated reports a variable-length quantity that ends before
	// its terminating byte.
	ErrVarintTruncated = errors.New("truncated variable-length quantity")
	// ErrDeltaTooLarge reports a delta-time that can't be represented as a
	// 4 byte variable-length quantity.
	ErrDeltaTooLarge = errors.New("delta-time exceeds the 4 byte VLQ limit")
	// ErrTruncatedData reports an event or chunk which ends before all of its
	// declared bytes were read.
	ErrTruncatedData = errors.New("truncated data")
)

// EncodeVarint returns the shortened varint encoding of x.
func EncodeVarint(x uint32) []byte {
	if x>>7 == 0 {
		return []byte{
			byte(x),
		}
	}

	if x>>14 == 0 {
		return []byte{
			byte(0x80 | x>>7),
			byte(127 & x),
		}
	}

	if x>>21 == 0 {
		return []byte{
			byte(0x80 | x>>14),
			byte(0x80 | x>>7),
			byte(127 & x),
		}
	}

	return []byte{
		byte(0x80 | x>>21),
		byte(0x80 | x>>14),
		byte(0x80 | x>>7),
		byte(127 & x),
	}
}

// DecodeVarint reads a varint-encoded integer from the slice.
// It returns the integer and the number of bytes consumed, or
// zero if there is not enough.
//
// This is the lenient variant of the decoder: it accepts zero-padded values
// (for instance 0x80 0x00 for 0) and, like the historical API, returns the
// value accumulated so far if the buffer ends before the terminating byte.
// Callers that need to reject malformed input should use decodeVarint.
func DecodeVarint(buf []byte) (x uint32, n int) {
	if len(buf) < 1 {
		return 0, 0
	}

	for _, b := range buf {
		x = x << 7
		x |= uint32(b) & 0x7F
		n++
		if b&0x80 == 0 {
			return x, n
		}
	}

	return x, n
}

// decodeVarint is the strict variant used by the decoder. It accepts
// zero-padded values but rejects values that use more than 4 bytes and values
// that aren't terminated before the end of buf.
func decodeVarint(buf []byte) (x uint32, n int, err error) {
	for n < len(buf) {
		if n >= 4 {
			return 0, n, ErrVarintTooLong
		}
		b := buf[n]
		n++
		x = (x << 7) | (uint32(b) & 0x7F)
		if b&0x80 == 0 {
			return x, n, nil
		}
	}
	return 0, n, ErrVarintTruncated
}

// encodeDelta encodes a delta-time, reporting values that don't fit in the
// MIDI 4 byte variable-length quantity.
func encodeDelta(x uint32) ([]byte, error) {
	if x > MaxTimeDelta {
		return nil, ErrDeltaTooLarge
	}
	return EncodeVarint(x), nil
}
