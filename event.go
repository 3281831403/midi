package midi

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// NoteOn returns a pointer to a new event of type NoteOn (without the delta timing data)
func NoteOn(channel, key, vel int) *Event {
	return &Event{
		MsgChan:  uint8(channel),
		MsgType:  uint8(EventByteMap["NoteOn"]),
		Note:     uint8(key),
		Velocity: uint8(vel),
	}
}

// NoteOff returns a pointer to a new event of type NoteOff (without the delta timing data)
func NoteOff(channel, key int) *Event {
	return &Event{
		MsgChan:  uint8(channel),
		MsgType:  uint8(EventByteMap["NoteOff"]),
		Note:     uint8(key),
		Velocity: 64,
	}
}

// EndOfTrack indicates the end of the midi track. Note that this event is
// automatically added when encoding a normal track.
func EndOfTrack() *Event {
	return &Event{
		MsgType: uint8(EventByteMap["Meta"]),
		MsgChan: uint8(15),
		Cmd:     MetaByteMap["End of Track"],
	}
}

func TrackName(name string) *Event {
	return &Event{
		MsgType:      uint8(EventByteMap["Meta"]),
		Cmd:          MetaByteMap["Sequence/Track name"],
		SeqTrackName: name,
	}
}

// Aftertouch returns a pointer to a new aftertouch event
func Aftertouch(channel, key, vel int) *Event {
	return &Event{
		MsgChan:  uint8(channel),
		MsgType:  uint8(EventByteMap["AfterTouch"]),
		Note:     uint8(key),
		Velocity: uint8(vel),
	}
}

// ControlChange sets a new value for a given controller
// The controller number is between 0-119.
// The new controller value is between 0-127.
func ControlChange(channel, controller, newVal int) *Event {
	return &Event{
		MsgChan:    uint8(channel),
		MsgType:    uint8(EventByteMap["ControlChange"]),
		Controller: uint8(controller),
		NewValue:   uint8(newVal),
	}
}

// ProgramChange returns a program change event for the given channel and
// program number. A program change carries a single data byte, so the
// controller argument is kept for API compatibility and ignored.
func ProgramChange(channel, controller, newVal int) *Event {
	return &Event{
		MsgChan:    uint8(channel),
		MsgType:    uint8(EventByteMap["ProgramChange"]),
		Controller: uint8(controller),
		NewValue:   uint8(newVal),
		NewProgram: uint8(newVal),
	}
}

// ChannelAfterTouch is a global aftertouch with a value from 0 to 127
func ChannelAfterTouch(channel, vel int) *Event {
	return &Event{
		MsgChan:  uint8(channel),
		MsgType:  uint8(EventByteMap["ChannelAfterTouch"]),
		Pressure: uint8(vel),
	}
}

// PitchWheelChange is sent to indicate a change in the pitch bender.
// The possible value goes between 0 and 16383 where 8192 is the center.
func PitchWheelChange(channel, int, val int) *Event {
	return &Event{
		MsgChan:      uint8(channel),
		MsgType:      uint8(EventByteMap["PitchWheelChange"]),
		AbsPitchBend: uint16(val),
	}
}

// CopyrightEvent returns a copyright event with the passed string in it.
func CopyrightEvent(txt string) *Event {
	return &Event{
		MsgType:   uint8(EventByteMap["Meta"]),
		Cmd:       uint8(MetaByteMap["Copyright"]),
		Copyright: txt,
	}
}

// TempoEvent returns a new tempo event of the passed value.
func TempoEvent(bpmF float64) *Event {
	ms := uint32(60000000 / bpmF)

	// bpm is expressed in in microseconds per MIDI quarter-note
	// This event indicates a tempo change.  Another way of putting
	// "microseconds per quarter-note" is "24ths of a microsecond per MIDI
	// clock".  Representing tempos as time per beat instead of beat per time
	// allows absolutely exact dword-term synchronization with a time-based sync
	// protocol such as SMPTE time code or MIDI time code.  This amount of
	// accuracy provided by this tempo resolution allows a four-minute piece at
	// 120 beats per minute to be accurate within 500 usec at the end of the
	// piece.  Ideally, these events should only occur where MIDI clocks would
	// be located Q this convention is intended to guarantee, or at least
	// increase the likelihood, of compatibility with other synchronization
	// devices so that a time signature/tempo map stored in this format may
	// easily be transferred to another device.
	return &Event{
		MsgType:        uint8(EventByteMap["Meta"]),
		Cmd:            uint8(MetaByteMap["Tempo"]),
		MsPerQuartNote: ms,
	}
}

// TODO
func Meta(channel int) *Event {
	return nil
}

// Event <event> = <MIDI event> | <sysex event> | <meta-event>
// <MIDI event> is any MIDI channel message.
// Running status is used:
// status bytes of MIDI channel messages may be omitted if the preceding
// event is a MIDI channel message with the same status. The first event
// in each MTrk chunk must specifyy status. Delta-time is not
// considered an event itself: it is an integral part of the syntax for
// an MTrk event. Notice that running status occurs across delta-times.
// See http://www.indiana.edu/~emusic/etext/MIDI/chapter3_MIDI4.shtml
type Event struct {
	TimeDelta uint32
	// AbsTicks is the absolute number of ticks since the start of the file.
	AbsTicks     uint64
	MsgType      uint8
	MsgChan      uint8
	Note         uint8
	Velocity     uint8
	Pressure     uint8
	Controller   uint8
	NewValue     uint8
	NewProgram   uint8
	Channel      uint8
	AbsPitchBend uint16
	RelPitchBend int16
	// Meta
	Cmd            uint8
	SeqNum         uint16
	Text           string
	Copyright      string
	SeqTrackName   string
	InstrumentName string
	Lyric          string
	Marker         string
	CuePoint       string
	MsPerQuartNote uint32
	Bpm            uint32
	TimeSignature  *TimeSignature
	// A positive value for the key specifies the number of sharps and a negative value specifies the number of flats.
	Key int32 //-7 to +7
	// A value of 0 for the scale specifies a major key and a value of 1 specifies a minor key.
	Scale uint32 // 0 or 1
	//
	SmpteOffset *SmpteOffset
	// SysExType is 0xF0 or 0xF7 for system exclusive events, 0 otherwise.
	SysExType uint8
	// RawData holds the payload of a meta or sysex event exactly as it was
	// read. When it is set (even to an empty slice), Encode writes it out
	// unchanged, which keeps unknown/unsupported meta events and sysex data
	// intact through a read/write round trip.
	RawData []byte
}

// Copy returns an exact copy of the event
func (e *Event) Copy() *Event {
	newEv := &Event{
		TimeDelta:    e.TimeDelta,
		MsgType:      e.MsgType,
		MsgChan:      e.MsgChan,
		Note:         e.Note,
		Velocity:     e.Velocity,
		Pressure:     e.Pressure,
		Controller:   e.Controller,
		NewValue:     e.NewValue,
		NewProgram:   e.NewProgram,
		Channel:      e.Channel,
		AbsPitchBend: e.AbsPitchBend,
		RelPitchBend: e.RelPitchBend,
		// Meta
		Cmd:            e.Cmd,
		SeqNum:         e.SeqNum,
		Text:           e.Text,
		Copyright:      e.Copyright,
		SeqTrackName:   e.SeqTrackName,
		InstrumentName: e.InstrumentName,
		Lyric:          e.Lyric,
		Marker:         e.Marker,
		CuePoint:       e.CuePoint,
		MsPerQuartNote: e.MsPerQuartNote,
		Bpm:            e.Bpm,
		Key:            e.Key,
		Scale:          e.Scale,
		SysExType:      e.SysExType,
	}
	if e.RawData != nil {
		newEv.RawData = append([]byte(nil), e.RawData...)
	}
	if e.TimeSignature != nil {
		newEv.TimeSignature = &TimeSignature{
			Numerator:                   e.TimeSignature.Numerator,
			Denominator:                 e.TimeSignature.Denominator,
			ClocksPerTick:               e.TimeSignature.ClocksPerTick,
			ThirtySecondNotesPerQuarter: e.TimeSignature.ThirtySecondNotesPerQuarter,
		}
	}
	if e.SmpteOffset != nil {
		newEv.SmpteOffset = &SmpteOffset{
			Hour:  e.SmpteOffset.Hour,
			Min:   e.SmpteOffset.Min,
			Sec:   e.SmpteOffset.Sec,
			Fr:    e.SmpteOffset.Fr,
			SubFr: e.SmpteOffset.SubFr,
		}
	}
	return newEv
}

// String implements the stringer interface
func (e *Event) String() string {
	if e == nil {
		return ""
	}
	var k string
	var ok bool
	if k, ok = EventMap[e.MsgType]; !ok {
		k = fmt.Sprintf("%#X", e.MsgType)
	}
	out := fmt.Sprintf("Ch %d @ %d (%d) \t%s", e.MsgChan, e.TimeDelta, e.AbsTicks, k)
	if e.Velocity > 0 {
		out += fmt.Sprintf(" Vel: %d", e.Velocity)
	}
	if e.MsgType == EventByteMap["NoteOn"] || e.MsgType == EventByteMap["NoteOff"] {
		out += fmt.Sprintf(" Note: %s", NoteToName(int(e.Note)))
	}
	if e.MsgType == EventByteMap["ControlChange"] {
		out += fmt.Sprintf(" - %s - Value: %d", CCNames[e.Controller], e.NewValue)
	}
	if e.Cmd != 0 {
		out = fmt.Sprintf("Ch %d @ %d (%d) \t%s", e.MsgChan, e.TimeDelta, e.AbsTicks, MetaCmdMap[e.Cmd])
		switch e.Cmd {
		case MetaByteMap["Sequence/Track name"]:
			out = fmt.Sprintf("%s -> %s", out, e.SeqTrackName)
		case MetaByteMap["Time Signature"]:
			out = fmt.Sprintf("%s -> %s", out, e.TimeSignature)
		case MetaByteMap["Copyright"]:
			out = fmt.Sprintf("%s -> %s", out, e.Copyright)
		case MetaByteMap["Tempo"]:
			out = fmt.Sprintf("%s -> %d", out, e.Bpm)
		}
	}

	return out
}

// Encode converts an Event into a slice of bytes ready to be written to a file.
//
// Events are always written with a complete status byte (running status is
// never used) and with the shortest possible variable-length quantities. Meta
// and sysex payloads are written exactly as stored in RawData when it is set.
func (e *Event) Encode() ([]byte, error) {
	if e == nil {
		return nil, fmt.Errorf("can't encode a nil event: %w", ErrUnexpectedData)
	}
	buff := bytes.NewBuffer(nil)
	delta, err := encodeDelta(e.TimeDelta)
	if err != nil {
		return nil, err
	}
	if _, err := buff.Write(delta); err != nil {
		return nil, err
	}

	switch {
	case e.SysExType == 0xF0 || e.SysExType == 0xF7:
		return e.encodeSysEx(buff)
	case e.MsgType == EventByteMap["Meta"]:
		return e.encodeMeta(buff)
	}

	// Voice messages: msg type and channel are stored together.
	if _, err := buff.Write([]byte{(e.MsgType << 4) | (e.MsgChan & 0x0F)}); err != nil {
		return nil, err
	}
	switch e.MsgType {
	// Note Off, Note On, Polyphonic Key Pressure
	case 0x8, 0x9, 0xA:
		buff.Write([]byte{e.Note & 0x7F, e.Velocity & 0x7F})
	// Control Change / Channel Mode
	case 0xB:
		buff.Write([]byte{e.Controller & 0x7F, e.NewValue & 0x7F})
	// Program Change: a single data byte.
	case 0xC:
		buff.Write([]byte{e.NewProgram & 0x7F})
	// Channel Pressure (Aftertouch)
	case 0xD:
		buff.Write([]byte{e.Pressure & 0x7F})
	// Pitch Bend Change: LSB then MSB.
	case 0xE:
		buff.Write([]byte{
			byte(e.AbsPitchBend & 0x7F),
			byte((e.AbsPitchBend >> 7) & 0x7F),
		})
	default:
		return nil, fmt.Errorf("didn't encode %#v because didn't know how to: %w", e, ErrUnexpectedData)
	}
	return buff.Bytes(), nil
}

func (e *Event) encodeSysEx(buff *bytes.Buffer) ([]byte, error) {
	buff.Write([]byte{e.SysExType})
	if err := writePayload(buff, e.RawData); err != nil {
		return nil, err
	}
	return buff.Bytes(), nil
}

// encodeMeta writes a meta event. When RawData is set, it is written out
// unchanged, which preserves events the decoder doesn't interpret (unknown
// meta types, sequencer specific data, ...). Otherwise, the payload is built
// from the event fields.
func (e *Event) encodeMeta(buff *bytes.Buffer) ([]byte, error) {
	buff.Write([]byte{0xFF, e.Cmd})
	var payload []byte
	if e.RawData != nil {
		payload = e.RawData
	} else {
		var err error
		if payload, err = e.metaPayload(); err != nil {
			return nil, err
		}
	}
	if err := writePayload(buff, payload); err != nil {
		return nil, err
	}
	return buff.Bytes(), nil
}

// metaPayload builds the payload of a meta event from its typed fields.
func (e *Event) metaPayload() ([]byte, error) {
	switch e.Cmd {
	case 0x00: // Sequence Number
		return []byte{byte(e.SeqNum >> 8), byte(e.SeqNum)}, nil
	case 0x01:
		return []byte(e.Text), nil
	case 0x02:
		return []byte(e.Copyright), nil
	case 0x03:
		return []byte(e.SeqTrackName), nil
	case 0x04:
		return []byte(e.InstrumentName), nil
	case 0x05:
		return []byte(e.Lyric), nil
	case 0x06:
		return []byte(e.Marker), nil
	case 0x07:
		return []byte(e.CuePoint), nil
	case 0x20:
		return []byte{e.Channel}, nil
	case 0x2F: // End of Track
		return nil, nil
	case 0x51:
		return Uint24(e.MsPerQuartNote), nil
	case 0x54:
		if e.SmpteOffset == nil {
			return nil, fmt.Errorf("SMPTE Offset event is missing its offset: %w", ErrUnexpectedData)
		}
		return []byte{e.SmpteOffset.Hour, e.SmpteOffset.Min, e.SmpteOffset.Sec, e.SmpteOffset.Fr, e.SmpteOffset.SubFr}, nil
	case 0x58:
		if e.TimeSignature == nil {
			return nil, fmt.Errorf("Time Signature event is missing its signature: %w", ErrUnexpectedData)
		}
		return []byte{
			e.TimeSignature.Numerator,
			e.TimeSignature.Denominator,
			e.TimeSignature.ClocksPerTick,
			e.TimeSignature.ThirtySecondNotesPerQuarter,
		}, nil
	case 0x59:
		return []byte{byte(int8(e.Key)), byte(e.Scale)}, nil
	}
	return nil, fmt.Errorf("can't encode unsupported meta event cmd %#x without a payload: %w", e.Cmd, ErrUnexpectedData)
}

// writePayload writes a variable length payload: the length as a shortened
// VLQ followed by the data bytes.
func writePayload(buff *bytes.Buffer, payload []byte) error {
	if err := binary.Write(buff, binary.BigEndian, EncodeVarint(uint32(len(payload)))); err != nil {
		return err
	}
	_, err := buff.Write(payload)
	return err
}

// Size represents the byte size to encode the event, delta-time excluded.
func (e *Event) Size() uint32 {
	if e == nil {
		return 0
	}
	if e.SysExType == 0xF0 || e.SysExType == 0xF7 {
		// status byte + length VLQ + payload
		return 1 + uint32(len(EncodeVarint(uint32(len(e.RawData))))+len(e.RawData))
	}
	switch e.MsgType {
	// Note Off, On, aftertouch, control change, pitch bend
	case 0x8, 0x9, 0xA, 0xB, 0xE:
		return 3
	// Program change, channel pressure
	case 0xC, 0xD:
		return 2
	case 0xF:
		payload := e.RawData
		if payload == nil {
			var err error
			if payload, err = e.metaPayload(); err != nil {
				return 0
			}
		}
		// 0xFF + cmd + length VLQ + payload
		return 2 + uint32(len(EncodeVarint(uint32(len(payload))))+len(payload))
	}
	return 0
}
