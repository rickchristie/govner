// Package vmproto defines the bounded protocol between the Cooper VM host and
// guest processes. It does not contain transport or policy decisions.
package vmproto

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	Magic           = "COOPER-VM"
	Version         = 1
	MaxHeaderSize   = 64 * 1024
	MaxFrameSize    = 1024 * 1024
	MaxForwardPorts = 4096
)

const (
	ServiceHello      = "hello"
	ServiceExec       = "exec"
	ServiceHealth     = "health"
	ServiceDoctor     = "doctor"
	ServiceReload     = "reload"
	ServiceShutdown   = "shutdown"
	ServiceDiagnostic = "diagnostic"
	ServiceProxy      = "proxy"
	ServiceBridge     = "bridge"
)

// Header starts each logical stream.
type Header struct {
	Magic        string   `json:"magic"`
	Version      int      `json:"version"`
	Service      string   `json:"service"`
	RequestID    string   `json:"request_id"`
	Nonce        string   `json:"nonce,omitempty"`
	Command      []string `json:"command,omitempty"`
	Environment  []string `json:"environment,omitempty"`
	Interactive  bool     `json:"interactive,omitempty"`
	Rows         uint16   `json:"rows,omitempty"`
	Columns      uint16   `json:"columns,omitempty"`
	ForwardPorts []int    `json:"forward_ports,omitempty"`
}

// NewHeader returns a header with the current protocol identity.
func NewHeader(service, requestID string) Header {
	return Header{Magic: Magic, Version: Version, Service: service, RequestID: requestID}
}

// Validate checks the common header fields. Service-specific code must also
// validate its own fields.
func (h Header) Validate() error {
	if h.Magic != Magic {
		return fmt.Errorf("invalid protocol magic %q", h.Magic)
	}
	if h.Version != Version {
		return fmt.Errorf("unsupported protocol version %d", h.Version)
	}
	if strings.TrimSpace(h.Service) == "" || len(h.Service) > 128 || strings.ContainsAny(h.Service, "\x00\r\n") {
		return errors.New("protocol service is required")
	}
	if strings.TrimSpace(h.RequestID) == "" || len(h.RequestID) > 128 || strings.ContainsAny(h.RequestID, "\x00\r\n") {
		return errors.New("protocol request ID is required")
	}
	if len(h.Nonce) > 128 || strings.ContainsAny(h.Nonce, "\x00\r\n") {
		return errors.New("protocol nonce is invalid")
	}
	if len(h.Command) > 256 {
		return errors.New("protocol command has too many arguments")
	}
	if len(h.Environment) > 512 {
		return errors.New("protocol environment has too many values")
	}
	for _, argument := range h.Command {
		if strings.ContainsRune(argument, '\x00') {
			return errors.New("protocol command contains a null byte")
		}
	}
	for _, environment := range h.Environment {
		if strings.ContainsRune(environment, '\x00') {
			return errors.New("protocol environment contains a null byte")
		}
	}
	if h.Interactive && (h.Rows == 0 || h.Columns == 0) {
		return errors.New("interactive protocol terminal size is required")
	}
	if len(h.ForwardPorts) > MaxForwardPorts {
		return fmt.Errorf("protocol has %d forward ports; maximum is %d", len(h.ForwardPorts), MaxForwardPorts)
	}
	seenPorts := make(map[int]bool, len(h.ForwardPorts))
	for _, port := range h.ForwardPorts {
		if port < 1 || port > 65535 {
			return fmt.Errorf("protocol forward port %d is invalid", port)
		}
		if seenPorts[port] {
			return fmt.Errorf("protocol forward port %d is duplicated", port)
		}
		seenPorts[port] = true
	}
	return nil
}

// WriteHeader writes a length-prefixed JSON header.
func WriteHeader(w io.Writer, header Header) error {
	if err := header.Validate(); err != nil {
		return err
	}
	data, err := json.Marshal(header)
	if err != nil {
		return fmt.Errorf("encode protocol header: %w", err)
	}
	if len(data) > MaxHeaderSize {
		return fmt.Errorf("protocol header is %d bytes; maximum is %d", len(data), MaxHeaderSize)
	}
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(data)))
	if err := writeAll(w, size[:]); err != nil {
		return fmt.Errorf("write protocol header size: %w", err)
	}
	if err := writeAll(w, data); err != nil {
		return fmt.Errorf("write protocol header: %w", err)
	}
	return nil
}

// ReadHeader reads and validates one length-prefixed JSON header.
func ReadHeader(r io.Reader) (Header, error) {
	var sizeData [4]byte
	if _, err := io.ReadFull(r, sizeData[:]); err != nil {
		return Header{}, fmt.Errorf("read protocol header size: %w", err)
	}
	size := binary.BigEndian.Uint32(sizeData[:])
	if size == 0 || size > MaxHeaderSize {
		return Header{}, fmt.Errorf("protocol header size %d is outside 1-%d", size, MaxHeaderSize)
	}
	data := make([]byte, size)
	if _, err := io.ReadFull(r, data); err != nil {
		return Header{}, fmt.Errorf("read protocol header: %w", err)
	}
	var header Header
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&header); err != nil {
		return Header{}, fmt.Errorf("decode protocol header: %w", err)
	}
	if err := requireJSONEnd(decoder); err != nil {
		return Header{}, fmt.Errorf("decode protocol header: %w", err)
	}
	if err := header.Validate(); err != nil {
		return Header{}, err
	}
	return header, nil
}

// FrameType identifies one exec stream frame.
type FrameType byte

const (
	FrameStdin FrameType = iota + 1
	FrameStdout
	FrameStderr
	FrameResize
	FrameSignal
	FrameExit
	FrameError
)

// Frame carries bounded interactive or one-shot process data.
type Frame struct {
	Type FrameType
	Data []byte
}

// WriteFrame writes one bounded frame.
func WriteFrame(w io.Writer, frame Frame) error {
	if frame.Type < FrameStdin || frame.Type > FrameError {
		return fmt.Errorf("invalid frame type %d", frame.Type)
	}
	if len(frame.Data) > MaxFrameSize {
		return fmt.Errorf("frame is %d bytes; maximum is %d", len(frame.Data), MaxFrameSize)
	}
	header := [5]byte{byte(frame.Type)}
	binary.BigEndian.PutUint32(header[1:], uint32(len(frame.Data)))
	if err := writeAll(w, header[:]); err != nil {
		return fmt.Errorf("write frame header: %w", err)
	}
	if len(frame.Data) == 0 {
		return nil
	}
	if err := writeAll(w, frame.Data); err != nil {
		return fmt.Errorf("write frame data: %w", err)
	}
	return nil
}

func writeAll(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := writer.Write(data)
		if err != nil {
			return err
		}
		if written <= 0 || written > len(data) {
			return io.ErrShortWrite
		}
		data = data[written:]
	}
	return nil
}

func requireJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("unexpected data after JSON object")
		}
		return err
	}
	return nil
}

// ReadFrame reads one bounded frame.
func ReadFrame(r io.Reader) (Frame, error) {
	var header [5]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return Frame{}, fmt.Errorf("read frame header: %w", err)
	}
	frameType := FrameType(header[0])
	if frameType < FrameStdin || frameType > FrameError {
		return Frame{}, fmt.Errorf("invalid frame type %d", frameType)
	}
	size := binary.BigEndian.Uint32(header[1:])
	if size > MaxFrameSize {
		return Frame{}, fmt.Errorf("frame size %d exceeds maximum %d", size, MaxFrameSize)
	}
	data := make([]byte, size)
	if _, err := io.ReadFull(r, data); err != nil {
		return Frame{}, fmt.Errorf("read frame data: %w", err)
	}
	return Frame{Type: frameType, Data: data}, nil
}

// EncodeResize returns the fixed payload for a resize frame.
func EncodeResize(rows, columns uint16) []byte {
	data := make([]byte, 4)
	binary.BigEndian.PutUint16(data[0:2], rows)
	binary.BigEndian.PutUint16(data[2:4], columns)
	return data
}

// DecodeResize validates and reads a resize frame payload.
func DecodeResize(data []byte) (rows, columns uint16, err error) {
	if len(data) != 4 {
		return 0, 0, fmt.Errorf("resize frame has %d bytes; want 4", len(data))
	}
	return binary.BigEndian.Uint16(data[0:2]), binary.BigEndian.Uint16(data[2:4]), nil
}

// EncodeExit returns the fixed payload for an exit frame.
func EncodeExit(status int32) []byte {
	data := make([]byte, 4)
	binary.BigEndian.PutUint32(data, uint32(status))
	return data
}

// DecodeExit validates and reads an exit frame payload.
func DecodeExit(data []byte) (int32, error) {
	if len(data) != 4 {
		return 0, fmt.Errorf("exit frame has %d bytes; want 4", len(data))
	}
	return int32(binary.BigEndian.Uint32(data)), nil
}
