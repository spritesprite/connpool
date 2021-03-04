# connpool

A connection pool for net.Conn in Golang.

First of all, this is NOT a typical connection pool where we hold connections and pop one when needed.

This connection pool is made for users who want to build a large number of "short" connections, and when they want the connection, they want it to be created as soon as possible.

It's an adaptive connection pool. When the requestors call Get(), if the pool has remaining connections, it returns one of them, if it doesn't, the pool will create 2 new connections concurrently and return one of them. Therefore, requestors will get their connection in a short period of time, without worrying about creating too many dangling connections.