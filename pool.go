package connpool

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

// ConnPool uses channel buffer connections.
type ConnPool struct {
	lock              sync.Mutex
	conns             chan net.Conn
	minConnNum        int
	maxConnNum        int
	totalConnNum      int
	closed            bool
	connCreator       func() (net.Conn, error)
	minChannelConnNum int
}

var (
	errPoolIsClose = errors.New("Connection pool has been closed")
	// Error for get connection time out.
	errTimeOut      = errors.New("Get Connection timeout")
	errContextClose = errors.New("Get Connection close by context")
)

// NewPool return new ConnPool. It base on channel. It will init minConn connections in channel first.
// When Get()/GetWithTimeout called, if channel still has connection it will get connection from channel.
// Otherwise ConnPool check number of connection which had already created as the number are less than maxConn,
// it use connCreator function to create new connection.
func NewPool(minConn, maxConn, minRemain int, connCreator func() (net.Conn, error)) (*ConnPool, error) {
	if minConn > maxConn || minConn < 0 || maxConn <= 0 {
		return nil, errors.New("Number of connection bound error")
	}

	pool := &ConnPool{}
	pool.minConnNum = minConn
	pool.maxConnNum = maxConn
	pool.connCreator = connCreator
	pool.conns = make(chan net.Conn, maxConn)
	pool.closed = false
	pool.totalConnNum = 0
	pool.minChannelConnNum = minRemain
	err := pool.init()
	if err != nil {
		return nil, err
	}
	return pool, nil
}

func (p *ConnPool) init() error {
	for i := 0; i < p.minConnNum; i++ {
		conn, err := p.createConn()
		if err != nil {
			return err
		}
		fmt.Printf("[ConnPool] enqueue %s->%s", conn.LocalAddr().String(), conn.RemoteAddr().String())
		p.conns <- conn
	}
	return nil
}

// Get get connection from connection pool. If connection pool is empty and already created connection number less than Max number of connection
// it will create new one. Otherwise it wil wait someone put connection back.
func (p *ConnPool) Get() (net.Conn, error) {
	if p.isClosed() == true {
		return nil, errPoolIsClose
	}

	if len(p.conns) < p.minChannelConnNum {
		go func() {
			conn, err := p.createConn()
			if err != nil {
				return
			}
			fmt.Printf("[ConnPool] enqueue %s->%s", conn.LocalAddr().String(), conn.RemoteAddr().String())
			p.conns <- conn
		}()
	}

	select {
	case conn := <-p.conns:
		fmt.Printf("[ConnPool] dequeue %s->%s", conn.LocalAddr().String(), conn.RemoteAddr().String())
		return p.packConn(conn), nil
	}
}

// GetWithTimeout can let you get connection wait for a time duration. If cannot get connection in this time.
// It will return TimeOutError.
func (p *ConnPool) GetWithTimeout(timeout time.Duration) (net.Conn, error) {
	if p.isClosed() == true {
		return nil, errPoolIsClose
	}
	go func() {
		conn, err := p.createConn()
		if err != nil {
			return
		}
		p.conns <- conn
	}()
	select {
	case conn := <-p.conns:
		return p.packConn(conn), nil
	case <-time.After(timeout):
		return nil, errTimeOut
	}
}

func (p *ConnPool) GetWithContext(ctx context.Context) (net.Conn, error) {
	if p.isClosed() == true {
		return nil, errPoolIsClose
	}
	go func() {
		conn, err := p.createConn()
		if err != nil {
			return
		}
		p.conns <- conn
	}()
	select {
	case conn := <-p.conns:
		return p.packConn(conn), nil
	case <-ctx.Done():
		return nil, errContextClose
	}
}

// Close close the connection pool. When close the connection pool it also close all connection already in connection pool.
// If connection not put back in connection it will not close. But it will close when it put back.
func (p *ConnPool) Close() error {
	if p.isClosed() == true {
		return errPoolIsClose
	}
	p.lock.Lock()
	defer p.lock.Unlock()
	p.closed = true
	close(p.conns)
	for conn := range p.conns {
		conn.Close()
	}
	return nil
}

// Put can put connection back in connection pool. If connection has been closed, the conneciton will be close too.
func (p *ConnPool) Put(conn net.Conn) error {
	if p.isClosed() == true {
		return errPoolIsClose
	}
	if conn == nil {
		p.lock.Lock()
		p.totalConnNum = p.totalConnNum - 1
		p.lock.Unlock()
		return errors.New("Cannot put nil to connection pool.")
	}

	select {
	case p.conns <- conn:
		fmt.Printf("[ConnPool] enqueue %s->%s", conn.LocalAddr().String(), conn.RemoteAddr().String())
		return nil
	default:
		return conn.Close()
	}
}

func (p *ConnPool) isClosed() bool {
	p.lock.Lock()
	ret := p.closed
	p.lock.Unlock()
	return ret
}

// RemoveConn let connection not belong connection pool.And it will close connection.
func (p *ConnPool) Remove(conn net.Conn) error {
	if p.isClosed() == true {
		return errPoolIsClose
	}

	p.lock.Lock()
	p.totalConnNum = p.totalConnNum - 1
	p.lock.Unlock()
	switch conn.(type) {
	case *CpConn:
		return conn.(*CpConn).Destroy()
	default:
		fmt.Printf("[ConnPool] close %s->%s", conn.LocalAddr().String(), conn.RemoteAddr().String())
		return conn.Close()
	}
	return nil
}

// createConn will create one connection from connCreator. And increase connection counter.
func (p *ConnPool) createConn() (net.Conn, error) {
	p.lock.Lock()
	defer p.lock.Unlock()
	if p.totalConnNum >= p.maxConnNum {
		return nil, fmt.Errorf("Connot Create new connection. Now has %d.Max is %d", p.totalConnNum, p.maxConnNum)
	}
	conn, err := p.connCreator()
	if err != nil {
		return nil, fmt.Errorf("Cannot create new connection.%s", err)
	}
	fmt.Printf("[ConnPool] create %s->%s", conn.LocalAddr().String(), conn.RemoteAddr().String())
	p.totalConnNum = p.totalConnNum + 1
	return conn, nil
}

func (p *ConnPool) packConn(conn net.Conn) net.Conn {
	ret := &CpConn{pool: p}
	ret.Conn = conn
	return ret
}
