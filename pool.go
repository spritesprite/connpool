package connpool

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// ConnPool uses channel to buffer connections.
type ConnPool struct {
	lock              sync.RWMutex // for closed
	timeLock          sync.RWMutex // for lastSuccTime
	conns             chan net.Conn
	minConnNum        int
	maxConnNum        int
	totalConnNum      int // deprecated
	minChannelConnNum int // deprecated
	closed            bool
	connCreator       func() (net.Conn, error)
	pendingCount      int32
	lastSuccTime      float64
	wg                sync.WaitGroup
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

// NewClosedPool provides an empty closed pool.
func NewClosedPool() *ConnPool {
	pool := &ConnPool{}
	pool.closed = true
	return pool
}

func (p *ConnPool) init() error {
	for i := 0; i < p.minConnNum; i++ {
		conn, err := p.createConn()
		if err != nil {
			return err
		}
		// fmt.Printf("[ConnPool] init() enqueue %s->%s\n", conn.LocalAddr().String(), conn.RemoteAddr().String())
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

	go p.supplementConn()
	if len(p.conns) == 0 {
		return p.createConn()
	}

	select {
	case conn := <-p.conns:
		// fmt.Printf("[ConnPool] Get() dequeue %s->%s\n", conn.LocalAddr().String(), conn.RemoteAddr().String())
		return p.packConn(conn), nil
	}
}

// GetWithTimeout can let you get connection wait for a time duration. If cannot get connection in this time.
// It will return TimeOutError.
func (p *ConnPool) GetWithTimeout(timeout time.Duration) (net.Conn, error) {
	if p.isClosed() == true {
		return nil, errPoolIsClose
	}

	go p.supplementConn()
	if len(p.conns) == 0 {
		return p.createConn()
	}

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

	go p.supplementConn()

	if len(p.conns) == 0 {
		return p.createConn()
	}

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
	p.closed = true
	p.lock.Unlock()

	p.wg.Wait() // might panic if we just close p.conns without waiting
	close(p.conns)
	for conn := range p.conns {
		conn.Close()
	}
	return nil
}

func (p *ConnPool) isClosed() bool {
	p.lock.RLock()
	defer p.lock.RUnlock()
	return p.closed
}

func (p *ConnPool) setLastSuccTime() {
	p.timeLock.Lock()
	defer p.timeLock.Unlock()
	p.lastSuccTime = GetCurrentTimeInFloat64(3)
}

func (p *ConnPool) getLastSuccTime() float64 {
	p.timeLock.RLock()
	defer p.timeLock.RUnlock()
	return p.lastSuccTime
}

// createConn will create one connection from connCreator. And increase connection counter.
func (p *ConnPool) createConn() (net.Conn, error) {
	// if p.totalConnNum >= p.maxConnNum {
	// 	return nil, fmt.Errorf("Connot Create new connection. Now has %d.Max is %d", p.totalConnNum, p.maxConnNum)
	// }
	conn, err := p.connCreator()
	if err != nil {
		return nil, fmt.Errorf("cannot create new connection: %s", err)
	}
	// fmt.Printf("[ConnPool] createConn() create %s->%s\n", conn.LocalAddr().String(), conn.RemoteAddr().String())
	// p.totalConnNum = p.totalConnNum + 1
	p.setLastSuccTime()
	return conn, nil
}

func (p *ConnPool) packConn(conn net.Conn) net.Conn {
	ret := &CpConn{pool: p}
	ret.Conn = conn
	return ret
}

func (p *ConnPool) supplementConn() {
	p.wg.Add(1)
	defer p.wg.Done()
	if p.isClosed() {
		return
	}

	atomic.AddInt32(&p.pendingCount, 1)
	defer atomic.AddInt32(&p.pendingCount, -1)

	if GetCurrentTimeInFloat64(3)-p.getLastSuccTime() < 3 && len(p.conns)+int(atomic.LoadInt32(&p.pendingCount)) < p.maxConnNum {
		newConn, err := p.createConn()
		if err != nil {
			return
		}
		// fmt.Printf("[ConnPool] supplementConn() enqueue %s->%s\n", newConn.LocalAddr().String(), newConn.RemoteAddr().String())

		p.conns <- newConn
	}
}

// NanoSec .
const NanoSec float64 = 1000000000

// GetCurrentTimeInFloat64 gets the current time.
func GetCurrentTimeInFloat64(precision int) float64 {
	p := math.Pow10(precision)
	return float64(int(float64(time.Now().UnixNano())/NanoSec*p)) / p
}

// // Put can put connection back in connection pool. If connection has been closed, the conneciton will be close too.
// func (p *ConnPool) Put(conn net.Conn) error {
// 	if p.isClosed() == true {
// 		return errPoolIsClose
// 	}
// 	if conn == nil {
// 		p.lock.Lock()
// 		p.totalConnNum = p.totalConnNum - 1
// 		p.lock.Unlock()
// 		return errors.New("Cannot put nil to connection pool.")
// 	}

// 	select {
// 	case p.conns <- conn:
// 		// fmt.Printf("[ConnPool] Put() enqueue %s->%s\n", conn.LocalAddr().String(), conn.RemoteAddr().String())
// 		return nil
// 	default:
// 		return conn.Close()
// 	}
// }

// // RemoveConn let connection not belong connection pool.And it will close connection.
// func (p *ConnPool) Remove(conn net.Conn) error {
// 	if p.isClosed() == true {
// 		return errPoolIsClose
// 	}

// 	p.lock.Lock()
// 	p.totalConnNum = p.totalConnNum - 1
// 	p.lock.Unlock()
// 	switch conn.(type) {
// 	case *CpConn:
// 		return conn.(*CpConn).Destroy()
// 	default:
// 		// fmt.Printf("[ConnPool] Remove() close %s->%s\n", conn.LocalAddr().String(), conn.RemoteAddr().String())
// 		return conn.Close()
// 	}
// 	return nil
// }

// func (p *ConnPool) createConnNolock() (net.Conn, error) {
// 	// if p.totalConnNum >= p.maxConnNum {
// 	// 	return nil, fmt.Errorf("Connot Create new connection. Now has %d.Max is %d", p.totalConnNum, p.maxConnNum)
// 	// }
// 	conn, err := p.connCreator()
// 	if err != nil {
// 		return nil, fmt.Errorf("Cannot create new connection.%s", err)
// 	}
// 	// fmt.Printf("[ConnPool] createConn() create %s->%s\n", conn.LocalAddr().String(), conn.RemoteAddr().String())
// 	// p.totalConnNum = p.totalConnNum + 1
// 	return conn, nil
// }
