package vmproto

import (
	"bytes"
	"encoding/binary"
	"io"
	"strings"
	"testing"
)

func TestHeaderRoundTrip(t *testing.T) {
	t.Parallel()
	want := NewHeader(ServiceExec, "request-1")
	want.Command = []string{"bash", "-c", "printf ok"}
	want.Environment = []string{"NAME=value"}
	want.Interactive = true
	want.Rows = 40
	want.Columns = 120
	var buffer bytes.Buffer
	if err := WriteHeader(&buffer, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadHeader(&buffer)
	if err != nil {
		t.Fatal(err)
	}
	if got.Service != want.Service || got.RequestID != want.RequestID || !got.Interactive {
		t.Fatalf("header = %#v, want %#v", got, want)
	}
}

func TestReloadHeaderValidatesForwardPorts(t *testing.T) {
	t.Parallel()
	header := NewHeader(ServiceReload, "reload")
	header.ForwardPorts = []int{8080, 9000}
	var buffer bytes.Buffer
	if err := WriteHeader(&buffer, header); err != nil {
		t.Fatal(err)
	}
	got, err := ReadHeader(&buffer)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ForwardPorts) != 2 || got.ForwardPorts[1] != 9000 {
		t.Fatalf("forward ports = %v", got.ForwardPorts)
	}
	for _, ports := range [][]int{{0}, {65536}, {8080, 8080}} {
		invalid := NewHeader(ServiceReload, "invalid")
		invalid.ForwardPorts = ports
		if err := invalid.Validate(); err == nil {
			t.Fatalf("ports %v passed validation", ports)
		}
	}
}

func TestReadHeaderRejectsInvalidInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{name: "empty", data: []byte{0, 0, 0, 0}, want: "outside"},
		{name: "too large", data: []byte{0, 1, 0, 1}, want: "outside"},
		{name: "short body", data: append([]byte{0, 0, 0, 4}, []byte("{}")...), want: "read protocol header"},
		{name: "unknown field", data: encodedJSON(`{"magic":"COOPER-VM","version":1,"service":"health","request_id":"r","bad":true}`), want: "unknown field"},
		{name: "wrong magic", data: encodedJSON(`{"magic":"bad","version":1,"service":"health","request_id":"r"}`), want: "magic"},
		{name: "wrong version", data: encodedJSON(`{"magic":"COOPER-VM","version":9,"service":"health","request_id":"r"}`), want: "version"},
		{name: "trailing JSON", data: encodedJSON(`{"magic":"COOPER-VM","version":1,"service":"health","request_id":"r"}{}`), want: "after JSON"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := ReadHeader(bytes.NewReader(test.data))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ReadHeader() error = %v, want text %q", err, test.want)
			}
		})
	}
}

func TestFrameRoundTrip(t *testing.T) {
	t.Parallel()
	for frameType := FrameStdin; frameType <= FrameError; frameType++ {
		var buffer bytes.Buffer
		want := Frame{Type: frameType, Data: []byte("payload")}
		if err := WriteFrame(&buffer, want); err != nil {
			t.Fatal(err)
		}
		got, err := ReadFrame(&buffer)
		if err != nil {
			t.Fatal(err)
		}
		if got.Type != want.Type || !bytes.Equal(got.Data, want.Data) {
			t.Fatalf("frame = %#v, want %#v", got, want)
		}
	}
}

func TestReadFrameRejectsOversizedAndInvalidFrames(t *testing.T) {
	t.Parallel()
	tests := [][]byte{
		{0, 0, 0, 0, 0},
		{byte(FrameStdin), 0, 16, 0, 1},
	}
	for _, data := range tests {
		if _, err := ReadFrame(bytes.NewReader(data)); err == nil {
			t.Fatalf("ReadFrame(%v) succeeded", data)
		}
	}
}

func TestProtocolWritersHandleShortWrites(t *testing.T) {
	t.Parallel()
	headerWriter := &shortWriter{maximum: 3}
	if err := WriteHeader(headerWriter, NewHeader(ServiceHealth, "request")); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadHeader(bytes.NewReader(headerWriter.data)); err != nil {
		t.Fatalf("short-write header is incomplete: %v", err)
	}

	frameWriter := &shortWriter{maximum: 2}
	want := Frame{Type: FrameStdout, Data: []byte("complete frame")}
	if err := WriteFrame(frameWriter, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFrame(bytes.NewReader(frameWriter.data))
	if err != nil || got.Type != want.Type || !bytes.Equal(got.Data, want.Data) {
		t.Fatalf("short-write frame = %#v, %v", got, err)
	}
}

func TestProtocolWritersRejectInvalidAndOversizedValues(t *testing.T) {
	t.Parallel()
	invalidHeader := NewHeader(ServiceExec, "request")
	invalidHeader.Command = []string{"bad\x00argument"}
	if err := WriteHeader(io.Discard, invalidHeader); err == nil {
		t.Fatal("WriteHeader accepted a command with a null byte")
	}
	interactive := NewHeader(ServiceExec, "request")
	interactive.Interactive = true
	if err := WriteHeader(io.Discard, interactive); err == nil {
		t.Fatal("WriteHeader accepted an interactive request without a terminal size")
	}
	if err := WriteFrame(io.Discard, Frame{Type: 0}); err == nil {
		t.Fatal("WriteFrame accepted an unknown frame type")
	}
	if err := WriteFrame(io.Discard, Frame{Type: FrameStdin, Data: make([]byte, MaxFrameSize+1)}); err == nil {
		t.Fatal("WriteFrame accepted an oversized frame")
	}
}

func TestResizeAndExitEncoding(t *testing.T) {
	t.Parallel()
	rows, columns, err := DecodeResize(EncodeResize(42, 180))
	if err != nil || rows != 42 || columns != 180 {
		t.Fatalf("resize = %d,%d,%v", rows, columns, err)
	}
	status, err := DecodeExit(EncodeExit(-9))
	if err != nil || status != -9 {
		t.Fatalf("exit = %d,%v", status, err)
	}
	if _, _, err := DecodeResize([]byte{1}); err == nil {
		t.Fatal("short resize succeeded")
	}
	if _, err := DecodeExit([]byte{1}); err == nil {
		t.Fatal("short exit succeeded")
	}
}

func encodedJSON(value string) []byte {
	data := []byte(value)
	result := make([]byte, 4, len(data)+4)
	binary.BigEndian.PutUint32(result, uint32(len(data)))
	return append(result, data...)
}

type shortWriter struct {
	maximum int
	data    []byte
}

func (w *shortWriter) Write(data []byte) (int, error) {
	size := len(data)
	if size > w.maximum {
		size = w.maximum
	}
	w.data = append(w.data, data[:size]...)
	return size, nil
}
