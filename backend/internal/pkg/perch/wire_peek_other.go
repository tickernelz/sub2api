//go:build !unix

package perch

import "net"

func wireConnIdle(net.Conn) bool { return false }
