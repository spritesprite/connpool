package connpool

import (
	"net"
)

// CpConn .
type CpConn struct {
	net.Conn
	pool *ConnPool
}

// Close .
func (conn *CpConn) Close() error {
	// return conn.Destroy()
	return conn.Conn.Close()
}
