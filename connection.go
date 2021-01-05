package connpool

import (
	"net"
)

type CpConn struct {
	net.Conn
	pool *ConnPool
}

// // Destroy will close connection and release connection from connection pool.
// func (conn *CpConn) Destroy() error {
// 	if conn.pool == nil {
// 		return errors.New("Connection not belong any connection pool.")
// 	}
// 	err := conn.pool.Remove(conn.Conn)
// 	if err != nil {
// 		return err
// 	}
// 	conn.pool = nil
// 	return nil
// }

// Close will close the real connection, remove it from the pool, and create a new connection.
func (conn *CpConn) Close() error {
	// return conn.Destroy()
	return conn.Close()
}

// // Close will close the real connection, remove it from the pool, and create a new connection.
// func (conn *CpConn) Close() error {
// 	if conn.pool == nil {
// 		return errors.New("Connection not belong any connection pool.")
// 	}

// 	go conn.pool.supplementConn()

// 	return conn.Destroy()
// 	// err := conn.pool.Remove(conn.Conn) // not sure if this is right
// 	// return err
// }
