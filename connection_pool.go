package h2client

import (
	"fmt"
	"github.com/pkg/errors"
	"sync"
)

type (
	connectionPoolItem struct {
		sync.RWMutex
		conns []*Connection
	}

	ConnectionPool struct {
		pool            map[string]*connectionPoolItem
		poolMu          sync.RWMutex
		maxConnsPerHost int
	}
)

func NewConnectionPool(maxConnsPerHost int) *ConnectionPool {
	pool := ConnectionPool{}
	pool.pool = make(map[string]*connectionPoolItem)

	if maxConnsPerHost <= 0 {
		maxConnsPerHost = 10
	}
	pool.maxConnsPerHost = maxConnsPerHost

	return &pool
}

func (p *ConnectionPool) Do(req *request) (*response, error) {
	conn, err := p.getConn(req)
	if err != nil {
		return nil, errors.Wrap(err, `There are no available connections`)
	}

	resp, err := conn.reqWithLockedStream(req)
	if err != nil {
		err = errors.Wrap(err, `Failed request execution`)
	}

	p.retConn(req, conn)

	return resp, err
}

func (p *ConnectionPool) getConn(req *request) (conn *Connection, err error) {
	poolKey := req.getCacheKey()

	p.poolMu.RLock()
	hostPool, ok := p.pool[poolKey]
	if !ok {
		p.poolMu.RUnlock()
		p.poolMu.Lock()
		if hostPool, ok = p.pool[poolKey]; !ok {
			hostPool = &connectionPoolItem{}
			p.pool[poolKey] = hostPool
		}
		p.poolMu.Unlock()
	} else {
		p.poolMu.RUnlock()
	}

	hostPool.RLock()
	for _, conn := range hostPool.conns {
		if conn.LockStream() {
			hostPool.RUnlock()
			return conn, nil
		} else if conn.HasGoAwayFrames() {
			fmt.Println(`conn for req`, req.getCacheKey(), `has GOAWAY frames#1:`, conn.GetGoAwayFrames())
		}
	}
	hostPool.RUnlock()

	// Все имеющиеся в пуле соединения нагружены по полной.
	// Нужно выделить еще один коннект (еси не превысили лимит).

	hostPool.Lock()

	for _, conn := range hostPool.conns {
		if conn.LockStream() {
			hostPool.Unlock()
			return conn, nil
		} else if conn.HasGoAwayFrames() {
			fmt.Println(`conn for req`, req.getCacheKey(), `has GOAWAY frames#2:`, conn.GetGoAwayFrames())
		}
	}

	if len(hostPool.conns) >= p.maxConnsPerHost {
		hostPool.Unlock()
		return nil, errors.Wrap(ErrPoolCapacityLimit, `Limit check`)
	}

	fmt.Println(`NewConnection`, req.getCacheKey())
	if conn, err = NewConnection(req); err != nil {
		hostPool.Unlock()
		return nil, errors.Wrap(err, `Cannot establish new connection`)
	}

	if !conn.LockStream() {
		if conn.HasGoAwayFrames() {
			fmt.Println(`conn for req`, req.getCacheKey(), `has GOAWAY frames#3:`, conn.GetGoAwayFrames())
		}
		conn.Close()
		hostPool.Unlock()
		return nil, errors.Wrap(ErrBug, `Cannot lock stream on new connection`)
	}

	hostPool.conns = append(hostPool.conns, conn)

	hostPool.Unlock()

	return conn, nil
}

func (p *ConnectionPool) retConn(req *request, conn *Connection) {
	conn.UnlockStream()
	// ToDo: уменьшать размер пула, если есть пустые соединения (заодно ввести MaxIdleConnsPerHost и выкинуть(?) maxConnsPerHost)
}
