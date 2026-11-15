package midi

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
)

type timeFormat int

const (
	MetricalTF timeFormat = iota + 1
	TimeCodeTF
)

/*
Decoder Format documented there: http://www.music.mcgill.ca/~ich/classes/mumt306/midiformat.pdf

	  <Header Chunk> = <chunk type><length><format><ntrks><division>


					Division, specifies the meaning of the delta-times.
					It has two formats, one for metrical time, and one for time-code-based
					time:
					 +---+-----------------------------------------+
					 | 0 | ticks per quarter-note                  |
					 ==============================================|
					 | 1 | negative SMPTE format  | ticks per frame|
					 +---+-----------------------+-----------------+
					 |15 |14                    8 |7             0 |
					If bit 15 of <division> is zero, the bits 14 thru 0 represent the number
					of delta time "ticks" which make up a quarter-note. For instance, if
					division is 96, then a time interval of an eighth-note between two
					events in the file would be 48.
			    If bit 15 of <division> is a one, delta times in a file correspond
			    to subdivisions of a second, in a way consistent with SMPTE and MIDI
			    Time Code. Bits 14 thru 8 contain one of the four values -24, -25, -29,
			    or -30, corresponding to the four standard SMPTE and MIDI Time Code
			    formats (-29 corresponds to 30 drop frome), and represents the
			    number of frames per second. These negative numbers are stored in
			    two's compliment form. The second byte (stored positive) is the
			    resolution within a frame: typical values may be 4 (MIDI Time Code
			    resolution), 8, 10, 80 (bit resolution), or 100. This stream allows
			    exact specifications of time-code-based tracks, but also allows
			    milisecond-based tracks by specifying 25|frames/sec and a
			    resolution of 40 units per frame. If the events in a file are stored
			    with a bit resolution of thirty-framel time code, the division word
			    would be E250 hex.
*/
type Decoder struct {
	r     *bufio.Reader
	Debug bool

	Ch chan *Track
	/*
	   Format describes the tracks format

	   0	-	single-track
	   Format 0 file has a header chunk followed by one track chunk. It
	   is the most interchangable representation of data. It is very useful
	   for a simple single-track player in a program which needs to make
	   synthesizers make sounds, but which is primarily concerened with
	   something else such as mixers or sound effect boxes. It is very
	   desirable to be able to produce such a format, even if your program
	   is track-based, in order to work with these simple programs. On the
	   other hand, perhaps someone will write a format conversion from
	   format 1 to format 0 which might be so easy to use in some setting
	   that it would save you the trouble of putting it into your program.


	   Synchronous multiple tracks means that the tracks will all be vertically synchronous, or in other words,
	    they all start at the same time, and so can represent different parts in one song.
	    1	-	multiple tracks, synchronous
	    Asynchronous multiple tracks do not necessarily start at the same time, and can be completely asynchronous.
	    2	-	multiple tracks, asynchronous
	*/
	Format uint16

	// NumTracks represents the number of tracks in the midi file
	NumTracks uint16

	// TicksPerQuarterNote is the metrical resolution. It is zero for
	// time-code based files, use Division for those.
	TicksPerQuarterNote uint16

	// Division is the raw division word from the header. It keeps the SMPTE
	// time-base (the high bit set) intact so it can be written back unchanged.
	Division uint16

	TimeFormat timeFormat
	Tracks     []*Track
}

// CurrentTrack returns the current track
func (d *Decoder) CurrentTrack() *Track {
	if d == nil || len(d.Tracks) == 0 {
		return nil
	}
	return d.Tracks[len(d.Tracks)-1]
}

// Parse is a deprecated API, Decode() should be used instead.
func (d *Decoder) Parse() error {
	return d.Decode()
}

// Decode decodes the MIDI file into a structure available from the decoder.
func (d *Decoder) Decode() error {
	var code [4]byte
	if _, err := io.ReadFull(d.r, code[:]); err != nil {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return fmt.Errorf("header chunk missing: %w", ErrTruncatedData)
		}
		return err
	}
	if code != headerChunkID {
		return fmt.Errorf("%v: %w", code, ErrFmtNotSupported)
	}
	var headerSize uint32
	if err := binary.Read(d.r, binary.BigEndian, &headerSize); err != nil {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return fmt.Errorf("header chunk truncated: %w", ErrTruncatedData)
		}
		return err
	}
	if headerSize != 6 {
		return fmt.Errorf("expected header size to be 6, was %d: %w", headerSize, ErrFmtNotSupported)
	}

	var header [6]byte
	if _, err := io.ReadFull(d.r, header[:]); err != nil {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return fmt.Errorf("header chunk truncated: %w", ErrTruncatedData)
		}
		return err
	}
	d.Format = binary.BigEndian.Uint16(header[0:2])
	d.NumTracks = binary.BigEndian.Uint16(header[2:4])
	division := binary.BigEndian.Uint16(header[4:6])
	d.Division = division

	// If bit 15 of <division> is zero, the bits 14 thru 0 represent the number
	// of delta time "ticks" which make up a quarter-note. For instance, if
	// division is 96, then a time interval of an eighth-note between two
	// events in the file would be 48.
	if (division & 0x8000) == 0 {
		d.TicksPerQuarterNote = division & 0x7FFF
		d.TimeFormat = MetricalTF
	} else {
		/*
			If bit 15 of <division> is a one, delta times in a file correspond
			to subdivisions of a second, in a way consistent with SMPTE and MIDI
			Time Code. Bits 14 thru 8 contain one of the four values -24, -25, -29,
			or -30, corresponding to the four standard SMPTE and MIDI Time Code
			formats (-29 corresponds to 30 drop frome), and represents the
			number of frames per second. These negative numbers are stored in
			two's compliment form. The second byte (stored positive) is the
			resolution within a frame: typical values may be 4 (MIDI Time Code
			resolution), 8, 10, 80 (bit resolution), or 100. This stream allows
			exact specifications of time-code-based tracks, but also allows
			milisecond-based tracks by specifying 25|frames/sec and a
			resolution of 40 units per frame. If the events in a file are stored
			with a bit resolution of thirty-frames time code, the division word
			would be E250 hex.
		*/
		d.TimeFormat = TimeCodeTF
	}

	for {
		id, size, err := d.IDnSize()
		if err == io.EOF {
			break
		}
		if err == io.ErrUnexpectedEOF {
			return fmt.Errorf("chunk header truncated: %w", ErrTruncatedData)
		}
		if err != nil {
			return err
		}
		if id == trackChunkID {
			body := make([]byte, size)
			if _, err := io.ReadFull(d.r, body); err != nil {
				return fmt.Errorf("track chunk truncated, expected %d bytes: %w", size, ErrTruncatedData)
			}
			if err := d.parseTrackBody(body); err != nil {
				return fmt.Errorf("track %d: %w", len(d.Tracks), err)
			}
			continue
		}
		// Unknown chunk types are skipped, as allowed by the SMF spec.
		if size > 0 {
			if _, err := io.CopyN(io.Discard, d.r, int64(size)); err != nil {
				return fmt.Errorf("chunk %s truncated, expected %d bytes: %w", id, size, ErrTruncatedData)
			}
		}
	}

	if uint16(len(d.Tracks)) != d.NumTracks {
		return fmt.Errorf("header declares %d track(s) but the file contains %d: %w", d.NumTracks, len(d.Tracks), ErrTruncatedData)
	}
	return nil
}

// IDnSize returns the next ID + block size
func (d *Decoder) IDnSize() ([4]byte, uint32, error) {
	var ID [4]byte
	var blockSize uint32
	if _, err := io.ReadFull(d.r, ID[:]); err != nil {
		return ID, blockSize, err
	}
	if err := binary.Read(d.r, binary.BigEndian, &blockSize); err != nil {
		return ID, blockSize, err
	}
	return ID, blockSize, nil
}

// VarLen returns the variable length value at the exact parser location.
func (d *Decoder) VarLen() (val uint32, readBytes uint32, err error) {
	var buf []byte
	for {
		if len(buf) >= 4 {
			return 0, uint32(len(buf)), ErrVarintTooLong
		}
		b, err := d.ReadByte()
		if err != nil {
			if err == io.EOF {
				return 0, uint32(len(buf)), ErrVarintTruncated
			}
			return 0, uint32(len(buf)), err
		}
		buf = append(buf, b)
		if b&0x80 == 0 {
			break
		}
	}
	val, _, err = decodeVarint(buf)
	return val, uint32(len(buf)), err
}

// VarLenTxt Returns a variable length text string as well as the amount of
// bytes read
func (d *Decoder) VarLenTxt() (string, uint32, error) {
	var l uint32
	var err error
	var n uint32

	if l, n, err = d.VarLen(); err != nil {
		return "", n, err
	}
	buf := make([]byte, l)
	m, err := io.ReadFull(d.r, buf)
	if err != nil {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return "", n + uint32(m), fmt.Errorf("variable length data truncated, expected %d bytes: %w", l, ErrTruncatedData)
		}
		return "", n + uint32(m), err
	}
	return string(buf), n + uint32(m), nil
}

// ReadByte returns one byte from the decoder
func (d *Decoder) ReadByte() (byte, error) {
	var b byte
	err := binary.Read(d.r, binary.BigEndian, &b)
	return b, err
}

// Read reads n bytes from the parser's reader and stores them into the provided dst,
// which must be a pointer to a fixed-size value.
func (d *Decoder) Read(dst interface{}) error {
	return binary.Read(d.r, binary.BigEndian, dst)
}

// Uint7 reads a byte and converts the first 7 bits into an uint8
func (d *Decoder) Uint7() (uint8, error) {
	b, err := d.ReadByte()
	if err != nil {
		return 0, err
	}
	if b&0x80 != 0 {
		return 0, fmt.Errorf("expected a 7 bit data byte, got %#x: %w", b, ErrUnexpectedData)
	}
	return (b & 0x7f), nil
}

// Uint24 reads 3 bytes and convert them into a uint32
func (d *Decoder) Uint24() (uint32, error) {
	bytes := make([]byte, 3)
	if err := d.Read(bytes); err != nil {
		return 0, err
	}

	var output uint32
	output |= uint32(bytes[2]) << 0
	output |= uint32(bytes[1]) << 8
	output |= uint32(bytes[0]) << 16

	return output, nil
}

// DecodeUint24 converts 3 bytes to an uint32
func DecodeUint24(bytes []byte) (uint32, error) {
	if len(bytes) != 3 {
		return 0, fmt.Errorf("bytes length was %d", len(bytes))
	}
	var output uint32
	output |= uint32(bytes[2]) << 0
	output |= uint32(bytes[1]) << 8
	output |= uint32(bytes[0]) << 16
	return output, nil
}
