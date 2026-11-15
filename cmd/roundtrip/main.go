// Command roundtrip decodes a MIDI file, writes it back out, and verifies that
// the data survived the trip.
//
// It exits with 0 when the round trip is faithful and with 1 (printing the
// reason on stderr) when the input is a bad file or the round trip doesn't
// hold.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/go-audio/midi"
	"github.com/mattetti/filebuffer"
)

var (
	inFlag  = flag.String("in", "", "path to the MIDI file to read")
	outFlag = flag.String("out", "", "path to write the verified MIDI file to (defaults to <in>.roundtrip.mid)")
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s -in file.mid [-out file.mid]\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	if err := run(*inFlag, *outFlag); err != nil {
		fmt.Fprintf(os.Stderr, "roundtrip: %v\n", err)
		os.Exit(1)
	}
}

func run(in, out string) error {
	if in == "" {
		return fmt.Errorf("no input file provided, use -in")
	}
	if out == "" {
		out = in + ".roundtrip.mid"
	}

	original, err := os.ReadFile(in)
	if err != nil {
		return fmt.Errorf("can't read input: %w", err)
	}
	first := midi.NewDecoder(bytes.NewReader(original))
	if err := first.Decode(); err != nil {
		return fmt.Errorf("bad file: %w", err)
	}

	// Write the decoded file out with the same header values.
	written, err := encode(first)
	if err != nil {
		return fmt.Errorf("can't write the decoded file: %w", err)
	}

	// Read the output back and make sure the events are unchanged. A track
	// that ended without an end-of-track event gets one on write, so the
	// written file is compared to the input decoded with that event added.
	second := midi.NewDecoder(bytes.NewReader(written))
	if err := second.Decode(); err != nil {
		return fmt.Errorf("the written file can't be read back: %w", err)
	}
	if err := compare(first, second); err != nil {
		return fmt.Errorf("the written file doesn't match the input: %w", err)
	}

	// Write the second decode once more; the bytes must be stable.
	again, err := encode(second)
	if err != nil {
		return fmt.Errorf("can't write the file a second time: %w", err)
	}
	if !bytes.Equal(written, again) {
		return fmt.Errorf("writing the file twice produced different bytes")
	}

	if err := os.WriteFile(out, written, 0644); err != nil {
		return fmt.Errorf("can't write output: %w", err)
	}

	summary := fmt.Sprintf("ok: %d track(s), format %d, division %#04x, %d byte(s)",
		len(first.Tracks), first.Format, first.Division, len(written))
	if bytes.Equal(original, written) {
		summary += ", byte-identical to the input"
	} else {
		summary += " (saved as " + out + ")"
	}
	fmt.Println(summary)
	return nil
}

// encode writes the decoded file to an in-memory seekable buffer and returns
// its bytes.
func encode(d *midi.Decoder) ([]byte, error) {
	buff := filebuffer.New(nil)
	enc := midi.NewEncoderFromDecoder(buff, d)
	if err := enc.Write(); err != nil {
		return nil, err
	}
	if _, err := buff.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	return buff.Bytes(), nil
}

// compare checks that both decodes carry the same header information and the
// same events, including their type, channel, data and tick.
func compare(a, b *midi.Decoder) error {
	if a.Format != b.Format {
		return fmt.Errorf("format changed from %d to %d", a.Format, b.Format)
	}
	if a.Division != b.Division {
		return fmt.Errorf("division changed from %#04x to %#04x", a.Division, b.Division)
	}
	if a.TicksPerQuarterNote != b.TicksPerQuarterNote {
		return fmt.Errorf("ticks per quarter note changed from %d to %d", a.TicksPerQuarterNote, b.TicksPerQuarterNote)
	}
	if a.TimeFormat != b.TimeFormat {
		return fmt.Errorf("time format changed from %d to %d", a.TimeFormat, b.TimeFormat)
	}
	if len(a.Tracks) != len(b.Tracks) {
		return fmt.Errorf("number of tracks changed from %d to %d", len(a.Tracks), len(b.Tracks))
	}
	for i := range a.Tracks {
		// The encoder adds a trailing end-of-track event when the input track
		// doesn't have one. That is the only event the writer may add.
		addedEnd := len(b.Tracks[i].Events) == len(a.Tracks[i].Events)+1 && isEndOfTrack(b.Tracks[i].Events[len(b.Tracks[i].Events)-1])
		if len(a.Tracks[i].Events) != len(b.Tracks[i].Events) && !addedEnd {
			return fmt.Errorf("track %d: number of events changed from %d to %d", i, len(a.Tracks[i].Events), len(b.Tracks[i].Events))
		}
		for j := range a.Tracks[i].Events {
			ea, eb := a.Tracks[i].Events[j], b.Tracks[i].Events[j]
			if ea.MsgType != eb.MsgType || ea.MsgChan != eb.MsgChan || ea.TimeDelta != eb.TimeDelta || ea.AbsTicks != eb.AbsTicks {
				return fmt.Errorf("track %d: event %d changed type/channel/tick (%#v -> %#v)", i, j, ea, eb)
			}
			if !samePayload(ea, eb) {
				return fmt.Errorf("track %d: event %d payload changed (%#v -> %#v)", i, j, ea, eb)
			}
		}
	}
	return nil
}

func isEndOfTrack(e *midi.Event) bool {
	return e != nil && e.MsgType == 0xF && e.Cmd == 0x2F
}

// samePayload compares the data carried by two events of the same type.
func samePayload(a, b *midi.Event) bool {
	if a.Cmd != b.Cmd || a.SysExType != b.SysExType {
		return false
	}
	if !bytes.Equal(a.RawData, b.RawData) {
		return false
	}
	switch a.MsgType {
	case 0x8, 0x9, 0xA:
		return a.Note == b.Note && a.Velocity == b.Velocity
	case 0xB:
		return a.Controller == b.Controller && a.NewValue == b.NewValue
	case 0xC:
		return a.NewProgram == b.NewProgram
	case 0xD:
		return a.Pressure == b.Pressure
	case 0xE:
		return a.AbsPitchBend == b.AbsPitchBend
	}
	return true
}
