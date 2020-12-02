package connpool

import (
	"errors"
	"fmt"
	"net"
)

type CpConn struct {
	net.Conn
	pool *ConnPool
}

// Destroy will close connection and release connection from connection pool.
func (conn *CpConn) Destroy() error {
	if conn.pool == nil {
		return errors.New("Connection not belong any connection pool.")
	}
	err := conn.pool.Remove(conn.Conn)
	if err != nil {
		return err
	}
	conn.pool = nil
	return nil
}

// Close will close the real connection, remove it from the pool, and create a new connection.
func (conn *CpConn) Close() error {
	if conn.pool == nil {
		return errors.New("Connection not belong any connection pool.")
	}

	if len(conn.pool.conns) < conn.pool.minChannelConnNum {
		go func(pool *ConnPool) {
			newConn, err := pool.createConn()
			if err != nil {
				return
			}
			fmt.Printf("[ConnPool] Close() enqueue %s->%s\n", newConn.LocalAddr().String(), newConn.RemoteAddr().String())
			pool.conns <- newConn
		}(conn.pool)
	}
	return conn.Destroy()
	// err := conn.pool.Remove(conn.Conn) // not sure if this is right
	// return err
}

// // Close will push connection back to connection pool. It will not close the real connection.
// func (conn *CpConn) Close() error {
// 	if conn.pool == nil {
// 		return errors.New("Connection not belong any connection pool.")
// 	}
// 	return conn.pool.Put(conn.Conn)
// }
