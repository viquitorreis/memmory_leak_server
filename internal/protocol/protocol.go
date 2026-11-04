package protocol

import (
	"encoding/binary"
	"errors"
	"io"
)

const (
	headerSize   = 5        // 1 byte cmd + 4 bytes about payload size
	maxFrameSize = 16 << 20 // 16 MiB
)

type CMD uint8

const (
	CmdGet CMD = iota + 1
	CmdValue
)

var (
	ErrInvalidCmd    = errors.New("invalid command")
	ErrFrameTooLarge = errors.New("frame too large")
)

type Message struct {
	Cmd     CMD
	Payload []byte
}

func (c CMD) IsValid() bool {
	switch c {
	case CmdGet, CmdValue:
		return true
	}
	return false
}

// marshall creates the entire frame on a single buffer, it only needs one Write
func (m Message) Marshal() ([]byte, error) {
	if !m.Cmd.IsValid() {
		return nil, ErrInvalidCmd
	}

	if len(m.Payload) > maxFrameSize {
		return nil, ErrFrameTooLarge
	}

	buf := make([]byte, headerSize+len(m.Payload))
	buf[0] = byte(m.Cmd)
	binary.BigEndian.PutUint32(buf[1:headerSize], uint32(len(m.Payload)))
	copy(buf[headerSize:], m.Payload)

	return buf, nil
}

// Decoder stores hdr inside itself, one per connection, reusing the entire frame
type Decoder struct {
	r   io.Reader
	hdr [headerSize]byte
}

func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{
		r: r,
	}
}

func (d *Decoder) Decode() (Message, error) {
	if _, err := io.ReadFull(d.r, d.hdr[:]); err != nil {
		return Message{}, err
	}

	cmd := CMD(d.hdr[0])
	if !cmd.IsValid() {
		return Message{}, ErrInvalidCmd
	}

	size := binary.BigEndian.Uint32(d.hdr[1:headerSize])
	if size > maxFrameSize {
		return Message{}, ErrFrameTooLarge
	}

	payload := make([]byte, size)
	if _, err := io.ReadFull(d.r, payload); err != nil {
		return Message{}, err
	}

	return Message{
		Cmd:     cmd,
		Payload: payload,
	}, nil
}
