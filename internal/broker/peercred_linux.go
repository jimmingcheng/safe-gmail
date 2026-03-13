//go:build linux

package broker

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

func peerUID(conn *net.UnixConn) (uint32, error) {
	rawConn, err := conn.SyscallConn()
	if err != nil {
		return 0, fmt.Errorf("syscall conn: %w", err)
	}

	var uid uint32
	var innerErr error
	if err := rawConn.Control(func(fd uintptr) {
		cred, credErr := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		if credErr != nil {
			innerErr = credErr
			return
		}
		uid = cred.Uid
	}); err != nil {
		return 0, fmt.Errorf("peer credentials: %w", err)
	}
	if innerErr != nil {
		return 0, fmt.Errorf("peer credentials: %w", innerErr)
	}
	return uid, nil
}
