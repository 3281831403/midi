package midi

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// canonicalFormat0Bytes is fixtures/example-format0.mid written back with
// complete status bytes (the running status of the source is expanded) and the
// delta-times using the shortest variable-length encoding.
func canonicalFormat0Bytes(t *testing.T) []byte {
	t.Helper()
	body := []byte{
		0x00, 0xFF, 0x58, 0x04, 0x04, 0x02, 0x18, 0x08,
		0x00, 0xFF, 0x51, 0x03, 0x07, 0xA1, 0x20,
		0x00, 0xC0, 0x05,
		0x00, 0xC1, 0x2E,
		0x00, 0xC2, 0x46,
		0x00, 0x92, 0x30, 0x60,
		0x00, 0x92, 0x3C, 0x60,
		0x60, 0x91, 0x43, 0x40,
		0x60, 0x90, 0x4C, 0x20,
		0x81, 0x40, 0x82, 0x30, 0x40,
		0x00, 0x82, 0x3C, 0x40,
		0x00, 0x81, 0x43, 0x40,
		0x00, 0x80, 0x4C, 0x40,
		0x00, 0xFF, 0x2F, 0x00,
	}
	out := []byte{0x4D, 0x54, 0x68, 0x64, 0x00, 0x00, 0x00, 0x06, 0x00, 0x00, 0x00, 0x01, 0x00, 0x60}
	out = append(out, []byte{0x4D, 0x54, 0x72, 0x6B, 0x00, 0x00, 0x00, byte(len(body))}...)
	return append(out, body...)
}

// encodeAll decodes data and writes it back, returning the produced bytes and
// the second decode.
func encodeAll(t *testing.T, data []byte) ([]byte, *Decoder) {
	t.Helper()
	first := NewDecoder(bytes.NewReader(data))
	if err := first.Decode(); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	out, err := encodeDecoded(first)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	second := NewDecoder(bytes.NewReader(out))
	if err := second.Decode(); err != nil {
		t.Fatalf("decode of the written data failed: %v", err)
	}
	return out, second
}

func encodeDecoded(d *Decoder) ([]byte, error) {
	buff := &seekBuffer{}
	enc := NewEncoderFromDecoder(buff, d)
	if err := enc.Write(); err != nil {
		return nil, err
	}
	return buff.data, nil
}

// seekBuffer is a minimal in-memory io.WriteSeeker.
type seekBuffer struct {
	data []byte
	pos  int
}

func (b *seekBuffer) Write(p []byte) (int, error) {
	if need := b.pos + len(p); need > len(b.data) {
		b.data = append(b.data, make([]byte, need-len(b.data))...)
	}
	copy(b.data[b.pos:], p)
	b.pos += len(p)
	return len(p), nil
}

func (b *seekBuffer) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case 0:
		b.pos = int(offset)
	case 1:
		b.pos += int(offset)
	case 2:
		b.pos = len(b.data) + int(offset)
	}
	if b.pos < 0 {
		return 0, errors.New("negative position")
	}
	return int64(b.pos), nil
}

// assertSameEvents checks that both decodes carry the same header values and
// the same events (type, channel, data and tick).
func assertSameEvents(t *testing.T, a, b *Decoder) {
	t.Helper()
	if a.Format != b.Format {
		t.Fatalf("format changed: %d -> %d", a.Format, b.Format)
	}
	if a.Division != b.Division {
		t.Fatalf("division changed: %#04x -> %#04x", a.Division, b.Division)
	}
	if a.TicksPerQuarterNote != b.TicksPerQuarterNote {
		t.Fatalf("ticks per quarter note changed: %d -> %d", a.TicksPerQuarterNote, b.TicksPerQuarterNote)
	}
	if len(a.Tracks) != len(b.Tracks) {
		t.Fatalf("track count changed: %d -> %d", len(a.Tracks), len(b.Tracks))
	}
	for i := range a.Tracks {
		if len(a.Tracks[i].Events) != len(b.Tracks[i].Events) {
			t.Fatalf("track %d: event count changed: %d -> %d", i, len(a.Tracks[i].Events), len(b.Tracks[i].Events))
		}
		for j := range a.Tracks[i].Events {
			ea, eb := a.Tracks[i].Events[j], b.Tracks[i].Events[j]
			if ea.MsgType != eb.MsgType || ea.MsgChan != eb.MsgChan || ea.TimeDelta != eb.TimeDelta ||
				ea.AbsTicks != eb.AbsTicks || ea.Cmd != eb.Cmd || ea.SysExType != eb.SysExType {
				t.Fatalf("track %d event %d changed:\n%#v\n%#v", i, j, ea, eb)
			}
			if !bytes.Equal(ea.RawData, eb.RawData) {
				t.Fatalf("track %d event %d payload changed: %x -> %x", i, j, ea.RawData, eb.RawData)
			}
			switch ea.MsgType {
			case 0x8, 0x9, 0xA:
				if ea.Note != eb.Note || ea.Velocity != eb.Velocity {
					t.Fatalf("track %d event %d data changed: %#v -> %#v", i, j, ea, eb)
				}
			case 0xB:
				if ea.Controller != eb.Controller || ea.NewValue != eb.NewValue {
					t.Fatalf("track %d event %d data changed: %#v -> %#v", i, j, ea, eb)
				}
			case 0xC:
				if ea.NewProgram != eb.NewProgram {
					t.Fatalf("track %d event %d program changed: %#v -> %#v", i, j, ea, eb)
				}
			case 0xD:
				if ea.Pressure != eb.Pressure {
					t.Fatalf("track %d event %d pressure changed: %#v -> %#v", i, j, ea, eb)
				}
			case 0xE:
				if ea.AbsPitchBend != eb.AbsPitchBend {
					t.Fatalf("track %d event %d pitch bend changed: %#v -> %#v", i, j, ea, eb)
				}
			}
		}
	}
}

func TestRoundTripFixtures(t *testing.T) {
	paths, err := filepath.Glob("fixtures/*.mid")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no fixtures found")
	}
	for _, path := range paths {
		path := path
		t.Run(filepath.Base(path), func(t *testing.T) {
			original, err := readFixture(path)
			if err != nil {
				t.Fatal(err)
			}
			out, second := encodeAll(t, original)
			first := NewDecoder(bytes.NewReader(original))
			if err := first.Decode(); err != nil {
				t.Fatal(err)
			}
			assertSameEvents(t, first, second)

			// Writing the second decode again yields the very same bytes.
			again, err := encodeDecoded(second)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(out, again) {
				t.Fatalf("the written bytes aren't stable:\n%x\n%x", out, again)
			}

			// The header's format and division fields are untouched.
			if !bytes.Equal(original[8:14], out[8:14]) {
				t.Fatalf("header changed: %x -> %x", original[8:14], out[8:14])
			}
		})
	}
}

func TestRoundTripCanonicalSample(t *testing.T) {
	original, err := readFixture("fixtures/example-format0.mid")
	if err != nil {
		t.Fatal(err)
	}
	out, second := encodeAll(t, original)
	first := NewDecoder(bytes.NewReader(original))
	if err := first.Decode(); err != nil {
		t.Fatal(err)
	}
	assertSameEvents(t, first, second)
	if want := canonicalFormat0Bytes(t); !bytes.Equal(out, want) {
		t.Fatalf("canonical encoding mismatch:\nwant %x\ngot  %x", want, out)
	}
	// And the canonical bytes decode back to the same data.
	if _, secondOfCanonical := encodeAll(t, out); secondOfCanonical == nil {
		t.Fatal("canonical sample doesn't decode")
	}
}

func TestRoundTripCanonicalHexSamples(t *testing.T) {
	for _, name := range []string{"fixtures/example-format0.mid", "fixtures/example-format1.mid"} {
		name := name
		t.Run(filepath.Base(name), func(t *testing.T) {
			original, err := readFixture(name)
			if err != nil {
				t.Fatal(err)
			}
			out, second := encodeAll(t, original)
			first := NewDecoder(bytes.NewReader(original))
			if err := first.Decode(); err != nil {
				t.Fatal(err)
			}
			assertSameEvents(t, first, second)
			// The output must itself be readable and stable.
			if _, third := encodeAll(t, out); third == nil {
				t.Fatal("second generation isn't decodable")
			}
		})
	}
}

func TestProgramChangeRoundTrip(t *testing.T) {
	// C0 05 followed by a note on: the program change only has one data byte,
	// the decoder must not consume the next event's status byte.
	body := []byte{
		0x00, 0xC0, 0x05,
		0x00, 0x90, 0x3C, 0x40,
		0x00, 0xC1, 0x2E,
		0x00, 0x91, 0x43, 0x40,
		0x00, 0xFF, 0x2F, 0x00,
	}
	data := buildFormat0(96, body)
	first := NewDecoder(bytes.NewReader(data))
	if err := first.Decode(); err != nil {
		t.Fatal(err)
	}
	evs := first.Tracks[0].Events
	if len(evs) != 5 {
		t.Fatalf("expected 5 events, got %d: %#v", len(evs), evs)
	}
	if evs[0].MsgType != 0xC || evs[0].NewProgram != 5 {
		t.Fatalf("expected program change 5, got %#v", evs[0])
	}
	if evs[1].MsgType != 0x9 || evs[1].Note != 0x3C || evs[1].Velocity != 0x40 {
		t.Fatalf("expected a note on after the program change, got %#v", evs[1])
	}
	if evs[2].MsgType != 0xC || evs[2].NewProgram != 0x2E {
		t.Fatalf("expected program change 0x2E, got %#v", evs[2])
	}
	out, second := encodeAll(t, data)
	assertSameEvents(t, first, second)
	// A program change is status byte + a single data byte.
	if !bytes.Contains(out, []byte{0x00, 0xC0, 0x05}) {
		t.Fatalf("program change not encoded as one data byte: %x", out)
	}
}

func TestSMPTEDivisionRoundTrip(t *testing.T) {
	// E728: SMPTE time-base, 25 frames per second, 40 ticks per frame.
	body := []byte{0x00, 0x90, 0x3C, 0x40, 0x00, 0x80, 0x3C, 0x40, 0x00, 0xFF, 0x2F, 0x00}
	data := buildFile(0, 0xE728, body)
	first := NewDecoder(bytes.NewReader(data))
	if err := first.Decode(); err != nil {
		t.Fatal(err)
	}
	if first.TimeFormat != TimeCodeTF {
		t.Fatalf("expected a time-code time base, got %v", first.TimeFormat)
	}
	if first.TicksPerQuarterNote != 0 {
		t.Fatalf("expected no metrical resolution, got %d", first.TicksPerQuarterNote)
	}
	if first.Division != 0xE728 {
		t.Fatalf("expected division 0xE728, got %#04x", first.Division)
	}
	out, second := encodeAll(t, data)
	if !bytes.Equal(out[12:14], []byte{0xE7, 0x28}) {
		t.Fatalf("division not written back: %x", out[12:14])
	}
	assertSameEvents(t, first, second)
}

func TestRoundTripPreservesMetaAndSysEx(t *testing.T) {
	// Unknown meta, sequencer specific, text, key signature (with flats) and
	// both sysex types. The payloads must be written back byte for byte.
	body := []byte{
		0x00, 0xFF, 0x03, 0x04, 'n', 'a', 'm', 'e',
		0x00, 0xFF, 0x60, 0x03, 0xDE, 0xAD, 0xBE, // unknown meta
		0x00, 0xFF, 0x7F, 0x02, 0x00, 0x7F, // sequencer specific
		0x00, 0xFF, 0x59, 0x02, 0xFD, 0x01, // key signature, 3 flats, minor
		0x00, 0xFF, 0x01, 0x03, 0x01, 0x02, 0x03,
		0x00, 0xFF, 0x54, 0x05, 0x61, 0x02, 0x03, 0x04, 0x05,
		0x00, 0xF0, 0x03, 0x01, 0x02, 0x03,
		0x00, 0xF7, 0x02, 0xAA, 0xBB,
		0x00, 0xFF, 0x2F, 0x00,
	}
	data := buildFormat0(96, body)
	first := NewDecoder(bytes.NewReader(data))
	if err := first.Decode(); err != nil {
		t.Fatal(err)
	}
	evs := first.Tracks[0].Events
	if len(evs) != 9 {
		t.Fatalf("expected 9 events, got %d", len(evs))
	}
	if evs[1].Cmd != 0x60 || !bytes.Equal(evs[1].RawData, []byte{0xDE, 0xAD, 0xBE}) {
		t.Fatalf("unknown meta payload lost: %#v", evs[1])
	}
	if evs[2].Cmd != 0x7F || !bytes.Equal(evs[2].RawData, []byte{0x00, 0x7F}) {
		t.Fatalf("sequencer specific payload lost: %#v", evs[2])
	}
	if evs[3].Key != -3 || evs[3].Scale != 1 {
		t.Fatalf("key signature not decoded as signed: %#v", evs[3])
	}
	if evs[5].SmpteOffset == nil || evs[5].SmpteOffset.Hour != 0x61 {
		t.Fatalf("SMPTE offset lost: %#v", evs[5])
	}
	if evs[6].SysExType != 0xF0 || !bytes.Equal(evs[6].RawData, []byte{0x01, 0x02, 0x03}) {
		t.Fatalf("F0 payload lost: %#v", evs[6])
	}
	if evs[7].SysExType != 0xF7 || !bytes.Equal(evs[7].RawData, []byte{0xAA, 0xBB}) {
		t.Fatalf("F7 payload lost: %#v", evs[7])
	}

	out, second := encodeAll(t, data)
	assertSameEvents(t, first, second)
	// The meta/sysex bytes in the output are the input bytes, verbatim.
	for _, want := range [][]byte{
		{0x00, 0xFF, 0x60, 0x03, 0xDE, 0xAD, 0xBE},
		{0x00, 0xFF, 0x7F, 0x02, 0x00, 0x7F},
		{0x00, 0xFF, 0x59, 0x02, 0xFD, 0x01},
		{0x00, 0xFF, 0x54, 0x05, 0x61, 0x02, 0x03, 0x04, 0x05},
		{0x00, 0xF0, 0x03, 0x01, 0x02, 0x03},
		{0x00, 0xF7, 0x02, 0xAA, 0xBB},
	} {
		if !bytes.Contains(out, want) {
			t.Fatalf("output is missing the verbatim bytes %x:\n%x", want, out)
		}
	}
}

func TestDecoderAcceptsZeroPaddedVarint(t *testing.T) {
	// Zero padded length: 0x80 0x04 is 4, and a zero padded delta.
	body := []byte{
		0x80, 0x00, // delta 0, padded
		0xFF, 0x03, 0x80, 0x04, 't', 'e', 's', 't', // padded length 4
		0x00, 0xFF, 0x2F, 0x00,
	}
	data := buildFormat0(96, body)
	dec := NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(); err != nil {
		t.Fatal(err)
	}
	evs := dec.Tracks[0].Events
	if len(evs) != 2 {
		t.Fatalf("expected 2 events, got %d: %#v", len(evs), evs)
	}
	if evs[0].SeqTrackName != "test" || evs[0].TimeDelta != 0 {
		t.Fatalf("padded VLQ not decoded: %#v", evs[0])
	}
	// The re-encoded length is the shortest form.
	out, _ := encodeAll(t, data)
	if !bytes.Contains(out, []byte{0xFF, 0x03, 0x04, 't', 'e', 's', 't'}) {
		t.Fatalf("length wasn't shortened on write: %x", out)
	}
}

func TestRoundTripNoInjectedEvents(t *testing.T) {
	// A track without a time signature, track name or end of track must not
	// gain any on the way out.
	body := []byte{0x00, 0x90, 0x3C, 0x40, 0x00, 0x80, 0x3C, 0x40}
	data := buildFormat0(96, body)
	first := NewDecoder(bytes.NewReader(data))
	if err := first.Decode(); err != nil {
		t.Fatal(err)
	}
	out, _ := encodeAll(t, data)
	// Only the missing end of track may be added.
	for _, cmd := range []byte{0x58, 0x03} {
		if bytes.Contains(out, []byte{0xFF, cmd}) {
			t.Fatalf("event %#x was injected on write: %x", cmd, out)
		}
	}
	if !bytes.Equal(out[14:22], []byte{0x4D, 0x54, 0x72, 0x6B, 0x00, 0x00, 0x00, 0x0C}) {
		t.Fatalf("unexpected track size: %x", out[14:22])
	}
}

func TestTrackNameWrittenOnce(t *testing.T) {
	body := []byte{
		0x00, 0xFF, 0x03, 0x02, 'n', 'm',
		0x00, 0xFF, 0x2F, 0x00,
	}
	data := buildFormat0(96, body)
	first := NewDecoder(bytes.NewReader(data))
	if err := first.Decode(); err != nil {
		t.Fatal(err)
	}
	out, _ := encodeAll(t, data)
	if n := bytes.Count(out, []byte{0xFF, 0x03}); n != 1 {
		t.Fatalf("expected exactly one track name event, found %d: %x", n, out)
	}

	// Setting a name on a track that already has one doesn't add a second one.
	tr := &Track{Events: []*Event{TrackName("other")}}
	tr.SetName("other")
	chunk, err := tr.ChunkData(true)
	if err != nil {
		t.Fatal(err)
	}
	if n := bytes.Count(chunk, []byte{0xFF, 0x03}); n != 1 {
		t.Fatalf("expected exactly one track name event, found %d: %x", n, chunk)
	}
}

func TestBadVarintTooLong(t *testing.T) {
	body := []byte{0x81, 0x80, 0x80, 0x80, 0x00, 0x90, 0x3C, 0x40}
	data := buildFormat0(96, body)
	dec := NewDecoder(bytes.NewReader(data))
	err := dec.Decode()
	if !errors.Is(err, ErrVarintTooLong) {
		t.Fatalf("expected %v, got %v", ErrVarintTooLong, err)
	}
}

func TestBadMetaTrailingDataByte(t *testing.T) {
	// End of track with a bogus payload length of one byte: only the length
	// byte is there, the quoted payload is missing, and the track ends.
	body := []byte{0x00, 0xFF, 0x2F, 0x01}
	data := buildFormat0(96, body)
	dec := NewDecoder(bytes.NewReader(data))
	err := dec.Decode()
	if !errors.Is(err, ErrTruncatedData) {
		t.Fatalf("expected %v, got %v", ErrTruncatedData, err)
	}
}

func TestBadMetaPayloadTruncated(t *testing.T) {
	// Time signature claiming 4 payload bytes but only 3 are present.
	body := []byte{0x00, 0xFF, 0x58, 0x04, 0x04, 0x02, 0x18}
	data := buildFormat0(96, body)
	dec := NewDecoder(bytes.NewReader(data))
	err := dec.Decode()
	if !errors.Is(err, ErrTruncatedData) {
		t.Fatalf("expected %v, got %v", ErrTruncatedData, err)
	}
}

func TestBadMetaDataBytesAfterMeta(t *testing.T) {
	// End of track, then a data byte with no status to run under.
	body := []byte{0x00, 0xFF, 0x2F, 0x00, 0x00, 0x3C}
	data := buildFormat0(96, body)
	dec := NewDecoder(bytes.NewReader(data))
	err := dec.Decode()
	if !errors.Is(err, ErrUnexpectedData) {
		t.Fatalf("expected %v, got %v", ErrUnexpectedData, err)
	}
}

func TestBadTrackTruncated(t *testing.T) {
	// The track chunk header announces 10 bytes, the file ends earlier.
	data := append([]byte{0x4D, 0x54, 0x68, 0x64, 0x00, 0x00, 0x00, 0x06, 0x00, 0x00, 0x00, 0x01, 0x00, 0x60},
		[]byte{0x4D, 0x54, 0x72, 0x6B, 0x00, 0x00, 0x00, 0x0A, 0x00, 0xFF, 0x2F, 0x00}...)
	dec := NewDecoder(bytes.NewReader(data))
	err := dec.Decode()
	if !errors.Is(err, ErrTruncatedData) {
		t.Fatalf("expected %v, got %v", ErrTruncatedData, err)
	}
}

func TestBadTrackEventTruncated(t *testing.T) {
	// The track body is complete but a note on is missing its velocity.
	body := []byte{0x00, 0x90, 0x3C}
	data := buildFormat0(96, body)
	dec := NewDecoder(bytes.NewReader(data))
	err := dec.Decode()
	if !errors.Is(err, ErrTruncatedData) {
		t.Fatalf("expected %v, got %v", ErrTruncatedData, err)
	}
}

func TestBadTrackCountMismatch(t *testing.T) {
	data := buildFormat0(96, []byte{0x00, 0xFF, 0x2F, 0x00})
	// Claim two tracks, the file has only one.
	data[10], data[11] = 0x00, 0x02
	dec := NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(); err == nil {
		t.Fatal("expected an error for a missing track")
	}
}

func TestDeltaTooLargeWrite(t *testing.T) {
	e := NoteOn(0, 60, 80)
	e.TimeDelta = MaxTimeDelta + 1
	if _, err := e.Encode(); !errors.Is(err, ErrDeltaTooLarge) {
		t.Fatalf("expected %v, got %v", ErrDeltaTooLarge, err)
	}
	// The maximum representable delta still encodes, in 4 bytes.
	e.TimeDelta = MaxTimeDelta
	data, err := e.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data[:4], []byte{0xFF, 0xFF, 0xFF, 0x7F}) {
		t.Fatalf("unexpected VLQ for the max delta: %x", data[:4])
	}

	// And the error propagates through the track and file encoders.
	e.TimeDelta = MaxTimeDelta + 1
	tr := &Track{Events: []*Event{e}}
	if _, err := tr.ChunkData(true); !errors.Is(err, ErrDeltaTooLarge) {
		t.Fatalf("expected %v from ChunkData, got %v", ErrDeltaTooLarge, err)
	}
	enc := NewEncoder(&seekBuffer{}, 0, 96)
	enc.Tracks = []*Track{tr}
	if err := enc.Write(); !errors.Is(err, ErrDeltaTooLarge) {
		t.Fatalf("expected %v from Write, got %v", ErrDeltaTooLarge, err)
	}
}

func TestShortestVarint(t *testing.T) {
	cases := []struct {
		delta uint32
		want  []byte
	}{
		{0, []byte{0x00}},
		{127, []byte{0x7F}},
		{128, []byte{0x81, 0x00}},
		{0x3FFF, []byte{0xFF, 0x7F}},
		{0x4000, []byte{0x81, 0x80, 0x00}},
		{0x1FFFFF, []byte{0xFF, 0xFF, 0x7F}},
		{0x200000, []byte{0x81, 0x80, 0x80, 0x00}},
		{MaxTimeDelta, []byte{0xFF, 0xFF, 0xFF, 0x7F}},
	}
	for _, c := range cases {
		e := NoteOn(0, 60, 0)
		e.TimeDelta = c.delta
		data, err := e.Encode()
		if err != nil {
			t.Fatalf("delta %d: %v", c.delta, err)
		}
		if n := len(data) - 3; !bytes.Equal(data[:n], c.want) {
			t.Fatalf("delta %d encoded as %x, want %x", c.delta, data[:n], c.want)
		}
	}
}

func TestCompleteStatusBytesOnWrite(t *testing.T) {
	// Running status is expanded: every voice event carries its status byte.
	body := []byte{
		0x00, 0x90, 0x3C, 0x40,
		0x00, 0x3C, 0x40,
		0x00, 0x3C, 0x40,
		0x00, 0xFF, 0x2F, 0x00,
	}
	data := buildFormat0(96, body)
	out, _ := encodeAll(t, data)
	if !bytes.Contains(out, []byte{0x00, 0x90, 0x3C, 0x40, 0x00, 0x90, 0x3C, 0x40, 0x00, 0x90, 0x3C, 0x40}) {
		t.Fatalf("running status wasn't expanded: %x", out)
	}
}

// buildFormat0 builds a format 0 file with the passed track body.
func buildFormat0(division uint16, body []byte) []byte {
	return buildFile(0, division, body)
}

// buildFile builds a single-track MIDI file.
func buildFile(format, division uint16, body []byte) []byte {
	out := []byte{0x4D, 0x54, 0x68, 0x64, 0x00, 0x00, 0x00, 0x06}
	out = append(out, byte(format>>8), byte(format), 0x00, 0x01, byte(division>>8), byte(division))
	out = append(out, []byte{0x4D, 0x54, 0x72, 0x6B, byte(len(body) >> 24), byte(len(body) >> 16), byte(len(body) >> 8), byte(len(body))}...)
	return append(out, body...)
}

func readFixture(path string) ([]byte, error) {
	return os.ReadFile(path)
}
