package desktop

import (
	"context"
	"io"
	"net"
	"os/exec"
)

// DialCommand connects a fixed local display command to a socket. net.Pipe
// gives the HTTP transport real read and write deadlines. Closing the socket
// also ends the command, including when a WebSocket viewer disconnects.
func DialCommand(ctx context.Context, name string, args ...string) (net.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	processContext, cancel := context.WithCancel(context.Background())
	command := exec.CommandContext(processContext, name, args...)
	input, err := command.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	output, err := command.StdoutPipe()
	if err != nil {
		input.Close()
		cancel()
		return nil, err
	}
	if err := command.Start(); err != nil {
		input.Close()
		output.Close()
		cancel()
		return nil, err
	}
	local, remote := net.Pipe()
	go func() { _, _ = io.Copy(input, remote); input.Close() }()
	go func() { _, _ = io.Copy(remote, output); remote.Close(); _ = command.Wait(); cancel() }()
	return &commandConnection{Conn: local, cancel: cancel}, nil
}

type commandConnection struct {
	net.Conn
	cancel context.CancelFunc
}

func (c *commandConnection) Close() error { c.cancel(); return c.Conn.Close() }
