package h2client

import (
	"github.com/pkg/errors"
)

const (
	DefaultMaxIdleConnsPerHost = 2
)

type (
	ConnectionPool struct {
		MaxIdleConnsPerHost int // ToDo: поддержка пула (host/ip+port)
	}
)

func NewConnectionPool() *ConnectionPool {
	pool := ConnectionPool{}
	pool.MaxIdleConnsPerHost = DefaultMaxIdleConnsPerHost
	return &pool
}

func (p *ConnectionPool) Do(req *request) (*response, error) {
	if err := req.prepare(); err != nil {
		return nil, errors.Wrap(err, `Wrong request`)
	}

	conn, err := p.getConn(req)
	if err != nil {
		return nil, errors.Wrap(err, `There are no available connections`)
	}

	resp, err := conn.Req(req)
	if err != nil {
		err = errors.Wrap(err, `Do request fail`)
	}

	p.retConn(req, conn)

	return resp, err
}

func (p *ConnectionPool) getConn(req *request) (conn *Connection, err error) {
	conn, err = NewConnection(req.Host, req.Port)
	if err != nil {
		err = errors.Wrap(err, `Cannot establist new connection`)
	}
	return
}

func (p *ConnectionPool) retConn(req *request, conn *Connection) {
	conn.Close()
}
