package midi

import (
	"fmt"
)

// parseTrackBody decodes the events of a single MTrk chunk from its body
// (the chunk length bytes are not included in body).
func (d *Decoder) parseTrackBody(body []byte) error {
	track := &Track{Size: uint32(len(body))}
	p := &trackParser{body: body, debug: d.Debug}
	for p.pos < len(body) {
		e, err := p.nextEvent()
		if err != nil {
			return err
		}
		track.currentTicks += uint64(e.TimeDelta)
		e.AbsTicks = track.currentTicks
		track.Events = append(track.Events, e)
	}
	d.Tracks = append(d.Tracks, track)
	return nil
}

// trackParser reads events from the body of a track chunk.
type trackParser struct {
	body  []byte
	pos   int
	debug bool
	// lastStatus is the status byte to reuse for running status. It is reset
	// by meta and sysex events which cancel running status.
	lastStatus byte
}

func (p *trackParser) remaining() int {
	return len(p.body) - p.pos
}

func (p *trackParser) readByte() (byte, error) {
	if p.pos >= len(p.body) {
		return 0, fmt.Errorf("event truncated: %w", ErrTruncatedData)
	}
	b := p.body[p.pos]
	p.pos++
	return b, nil
}

func (p *trackParser) readDataByte() (uint8, error) {
	b, err := p.readByte()
	if err != nil {
		return 0, err
	}
	if b&0x80 != 0 {
		return 0, fmt.Errorf("expected a data byte, got status byte %#x: %w", b, ErrUnexpectedData)
	}
	return b, nil
}

// readVarint reads a MIDI variable-length quantity. Zero-padded values are
// accepted, values longer than 4 bytes are rejected.
func (p *trackParser) readVarint() (uint32, error) {
	var val uint32
	for n := 0; ; n++ {
		if n >= 4 {
			return 0, ErrVarintTooLong
		}
		b, err := p.readByte()
		if err != nil {
			return 0, ErrVarintTruncated
		}
		val = (val << 7) | uint32(b&0x7F)
		if b&0x80 == 0 {
			return val, nil
		}
	}
}

func (p *trackParser) readPayload(length uint32) ([]byte, error) {
	if uint64(length) > uint64(p.remaining()) {
		return nil, fmt.Errorf("event payload of %d bytes exceeds the %d bytes left in the track: %w", length, p.remaining(), ErrTruncatedData)
	}
	end := p.pos + int(length)
	payload := p.body[p.pos:end:end]
	p.pos = end
	return payload, nil
}

// nextEvent reads one delta-time + event pair.
func (p *trackParser) nextEvent() (*Event, error) {
	delta, err := p.readVarint()
	if err != nil {
		return nil, err
	}

	status, err := p.readByte()
	if err != nil {
		return nil, err
	}
	if status&0x80 == 0 {
		// Running status: reuse the previous channel status.
		if p.lastStatus == 0 {
			return nil, fmt.Errorf("data byte %#x without a preceding status byte: %w", status, ErrUnexpectedData)
		}
		status = p.lastStatus
		p.pos--
	}
	e := &Event{TimeDelta: delta}

	switch status {
	case 0xFF:
		p.lastStatus = 0
		if err := p.parseMeta(e); err != nil {
			return nil, err
		}
		return e, nil
	case 0xF0, 0xF7:
		p.lastStatus = 0
		e.MsgType = status >> 4
		e.MsgChan = status & 0x0F
		e.SysExType = status
		length, err := p.readVarint()
		if err != nil {
			return nil, err
		}
		payload, err := p.readPayload(length)
		if err != nil {
			return nil, err
		}
		e.RawData = payload
		return e, nil
	}

	if status < 0x80 || status >= 0xF0 {
		return nil, fmt.Errorf("unsupported status byte %#x: %w", status, ErrUnexpectedData)
	}

	e.MsgType = (status & 0xF0) >> 4
	e.MsgChan = status & 0x0F
	p.lastStatus = status

	switch e.MsgType {
	// Note Off, Note On, Polyphonic Key Pressure
	case 0x8, 0x9, 0xA:
		if e.Note, err = p.readDataByte(); err != nil {
			return nil, err
		}
		if e.Velocity, err = p.readDataByte(); err != nil {
			return nil, err
		}
	// Control Change / Channel Mode
	case 0xB:
		if e.Controller, err = p.readDataByte(); err != nil {
			return nil, err
		}
		if e.NewValue, err = p.readDataByte(); err != nil {
			return nil, err
		}
	// Program Change: a single data byte (the new program).
	case 0xC:
		if e.NewProgram, err = p.readDataByte(); err != nil {
			return nil, err
		}
	// Channel Pressure (Aftertouch)
	case 0xD:
		if e.Pressure, err = p.readDataByte(); err != nil {
			return nil, err
		}
	// Pitch Bend Change: LSB then MSB.
	case 0xE:
		lsb, err := p.readDataByte()
		if err != nil {
			return nil, err
		}
		msb, err := p.readDataByte()
		if err != nil {
			return nil, err
		}
		e.AbsPitchBend = uint16(msb)<<7 | uint16(lsb)
		e.RelPitchBend = int16(e.AbsPitchBend) - 0x2000
	default:
		return nil, fmt.Errorf("unsupported status byte %#x: %w", status, ErrUnexpectedData)
	}
	return e, nil
}

// parseMeta reads the meta event command and payload. The 0xFF status byte has
// already been consumed.
func (p *trackParser) parseMeta(e *Event) error {
	e.MsgType = 0xF
	e.MsgChan = 0xF
	cmd, err := p.readByte()
	if err != nil {
		return err
	}
	e.Cmd = cmd
	length, err := p.readVarint()
	if err != nil {
		return err
	}
	payload, err := p.readPayload(length)
	if err != nil {
		return err
	}

	switch cmd {
	case 0x00: // Sequence Number
		if len(payload) == 0 {
			// An empty sequence number is allowed; keep the payload so the
			// zero length is written back as is.
			e.RawData = []byte{}
			return nil
		}
		if len(payload) != 2 {
			return fmt.Errorf("sequence number payload is %d bytes, expected 2: %w", len(payload), ErrUnexpectedData)
		}
		e.SeqNum = uint16(payload[0])<<8 | uint16(payload[1])
	case 0x01:
		e.Text = string(payload)
	case 0x02:
		e.Copyright = string(payload)
	case 0x03:
		e.SeqTrackName = string(payload)
	case 0x04:
		e.InstrumentName = string(payload)
	case 0x05:
		e.Lyric = string(payload)
	case 0x06:
		e.Marker = string(payload)
	case 0x07:
		e.CuePoint = string(payload)
	case 0x20: // MIDI Channel Prefix
		if len(payload) != 1 {
			return fmt.Errorf("MIDI Channel Prefix payload is %d bytes, expected 1: %w", len(payload), ErrUnexpectedData)
		}
		e.Channel = payload[0]
	case 0x2F: // End of Track
		// Anything following the command but no proper length/payload is
		// reported as unexpected data rather than silently dropped.
		if len(payload) != 0 {
			return fmt.Errorf("End of Track payload is %d bytes, expected 0: %w", len(payload), ErrUnexpectedData)
		}
	case 0x51: // Set Tempo
		if len(payload) != 3 {
			return fmt.Errorf("Set Tempo payload is %d bytes, expected 3: %w", len(payload), ErrUnexpectedData)
		}
		e.MsPerQuartNote = uint32(payload[0])<<16 | uint32(payload[1])<<8 | uint32(payload[2])
		if e.MsPerQuartNote != 0 {
			e.Bpm = 60000000 / e.MsPerQuartNote
		}
	case 0x54: // SMPTE Offset
		if len(payload) != 5 {
			return fmt.Errorf("SMPTE Offset payload is %d bytes, expected 5: %w", len(payload), ErrUnexpectedData)
		}
		e.SmpteOffset = &SmpteOffset{
			Hour:  payload[0],
			Min:   payload[1],
			Sec:   payload[2],
			Fr:    payload[3],
			SubFr: payload[4],
		}
	case 0x58: // Time Signature
		if len(payload) != 4 {
			return fmt.Errorf("Time Signature payload is %d bytes, expected 4: %w", len(payload), ErrUnexpectedData)
		}
		e.TimeSignature = &TimeSignature{
			Numerator:                   payload[0],
			Denominator:                 payload[1],
			ClocksPerTick:               payload[2],
			ThirtySecondNotesPerQuarter: payload[3],
		}
	case 0x59: // Key Signature
		if len(payload) != 2 {
			return fmt.Errorf("Key Signature payload is %d bytes, expected 2: %w", len(payload), ErrUnexpectedData)
		}
		// The key is a signed value: negative for flats, positive for sharps.
		e.Key = int32(int8(payload[0]))
		e.Scale = uint32(payload[1])
	case 0x7F: // Sequencer Specific
		// Kept in RawData and written out untouched.
		e.RawData = payload
	default:
		// Unknown meta events are preserved as is so they survive a round trip.
		e.RawData = payload
		if p.debug {
			fmt.Printf("Kept unknown meta cmd %#X (%d bytes)\n", cmd, len(payload))
		}
	}
	return nil
}
