// Package vmguest runs inside the no-NIC Cooper guest.
package vmguest

import (
	"net"
	"os"
	"time"
)

type fileConnection struct{ *os.File }

func (f fileConnection) LocalAddr() net.Addr              { return fileAddress("guest") }
func (f fileConnection) RemoteAddr() net.Addr             { return fileAddress("host") }
func (f fileConnection) SetDeadline(time.Time) error      { return nil }
func (f fileConnection) SetReadDeadline(time.Time) error  { return nil }
func (f fileConnection) SetWriteDeadline(time.Time) error { return nil }

type fileAddress string

func (a fileAddress) Network() string { return "virtio-serial" }
func (a fileAddress) String() string  { return string(a) }
