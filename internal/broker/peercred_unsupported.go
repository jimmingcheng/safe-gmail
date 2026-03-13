//go:build !linux && !darwin

package broker

import (
	"fmt"
	"net"
)

func peerUID(_ *net.UnixConn) (uint32, error) {
	return 0, fmt.Errorf("peer credential lookup is unsupported on this platform")
}
