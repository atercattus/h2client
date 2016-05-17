package h2client

import (
	"fmt"
	"github.com/pkg/errors"
	"strconv"
	"sync"
)

type (
	ConnectionPool struct {
		pool            map[string][]*Connection
		poolMu          sync.RWMutex
		maxConnsPerHost int
	}
)

func NewConnectionPool(maxConnsPerHost int) *ConnectionPool {
	pool := ConnectionPool{}
	pool.pool = make(map[string][]*Connection)

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
	poolKey := req.Host + `:` + strconv.Itoa(req.Port)

	p.poolMu.RLock()
	hostPool, ok := p.pool[poolKey]
	if !ok {
		p.poolMu.RUnlock()
		p.poolMu.Lock()
		if hostPool, ok = p.pool[poolKey]; !ok {
			conn, err = NewConnection(req.Host, req.Port)
			if err == nil {
				p.pool[poolKey] = []*Connection{conn}
			} else {
				err = errors.Wrap(err, `Cannot establish new connection`)
			}
			p.poolMu.Unlock()
			return conn, err
		}
		p.poolMu.Unlock()
		p.poolMu.RLock()
	}

	for _, conn := range hostPool {
		if conn.LockStream() {
			p.poolMu.RUnlock()
			return conn, nil
		}
	}

	p.poolMu.RUnlock()

	// Все имеющиеся в пуле соединения нагружены по полной.
	// Нужно выделить еще один коннект (еси не превысили лимит).

	p.poolMu.Lock()

	hostPool, _ = p.pool[poolKey] // удаляться ключи (пока?) не будут

	for _, conn := range hostPool {
		if conn.LockStream() {
			p.poolMu.Unlock()
			return conn, nil
		}
	}

	if len(hostPool) >= p.maxConnsPerHost {
		p.poolMu.Unlock()
		return nil, errors.Wrap(ErrPoolCapacityLimit, `Limit check`)
	}

	conn, err = NewConnection(req.Host, req.Port)
	if err != nil {
		p.poolMu.Unlock()
		return nil, errors.Wrap(err, `Cannot establish new connection`)
	}

	if !conn.LockStream() {
		conn.Close()
		p.poolMu.Unlock()
		return nil, errors.Wrap(ErrBug, `Cannot lock stream on new connection`)
	}

	hostPool = append(hostPool, conn)
	p.pool[poolKey] = hostPool

	p.poolMu.Unlock()

	return conn, nil
}

func (p *ConnectionPool) retConn(req *request, conn *Connection) {
	conn.UnlockStream()
	// уменьшать размер пула, если есть ненагружунные соединени
}
