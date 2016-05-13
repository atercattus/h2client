package h2client

import (
	"net/url"
	"github.com/pkg/errors"
	"bytes"
	"net"
	"strconv"
)

type (
	ConnectionPool struct {
	}
)

// ToDo: сделать, собственно пул коннектов к (host+port)

func NewConnectionPool() *ConnectionPool {
	pool := ConnectionPool{}
	return &pool
}

func (p *ConnectionPool) Do(method string, rawUrl string, headers []HeaderPair, body *bytes.Buffer) (*response, error) {
	parsedUrl, err := url.Parse(rawUrl)
	if err != nil {
		return nil, errors.Wrap(err, `Parse URL fail`)
	} else if parsedUrl.Scheme != `https` {
		return nil, errors.Wrap(err, `Only https scheme allowed`)
	}

	host, port, err := p.parseHostPort(parsedUrl)
	if err != nil {
		return nil, errors.Wrap(err, `Parse host and port fail`)
	}

	http2conn, err := NewConnection(host, port)
	if err != nil {
		return nil, errors.Wrap(err, `Cannot establist new connection`)
	}

	path := parsedUrl.Path
	if parsedUrl.RawQuery != `` {
		path += `?`+parsedUrl.RawQuery
	}

	resp, err := http2conn.Req(method, path, headers, body)

	http2conn.Close()

	return resp, err
}

func (p *ConnectionPool) parseHostPort(parsedUrl *url.URL) (host string, port int, err error) {
	// ToDo: кешировать разбор
	var portRaw string

	if host, portRaw, err = net.SplitHostPort(parsedUrl.Host); err != nil {
		host = parsedUrl.Host
		port = 443
	} else if port, err = strconv.Atoi(portRaw); err != nil {
		err = errors.Wrap(err, `Wrong port format`)
		return
	} else if port <= 0 || port > 65535 {
		err = errors.New(`Wrong port value`) // ToDo: возвращать переменную или константу
		return
	}

	return host, port, nil
}
