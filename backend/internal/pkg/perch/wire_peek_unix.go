//go:build unix

package perch

import (
	"crypto/tls"
	"net"

	"golang.org/x/sys/unix"
)

func wireConnIdle(conn net.Conn) bool {
	if tlsConn, ok := conn.(*tls.Conn); ok {
		conn = tlsConn.NetConn()
	}
	tcp, ok := conn.(*net.TCPConn)
	if !ok {
		return false
	}
	raw, err := tcp.SyscallConn()
	if err != nil {
		return false
	}
	var (
		peeked int
		sysErr error
		buf    [1]byte
	)
	if err := raw.Read(func(fd uintptr) bool {
		peeked, _, sysErr = unix.Recvfrom(int(fd), buf[:], unix.MSG_PEEK|unix.MSG_DONTWAIT)
		return true
	}); err != nil {
		return false
	}
	if peeked > 0 {
		return false
	}
	return sysErr == unix.EAGAIN || sysErr == unix.EWOULDBLOCK
}
