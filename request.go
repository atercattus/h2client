package h2client

import (
	"bytes"
	"crypto/tls"
	"github.com/pkg/errors"
	"math"
	"net"
	"net/url"
	"strconv"
	"sync"
	"time"
	"unsafe"
)

type (
	TLSConfig struct {
		Certificates []tls.Certificate
	}

	request struct {
		Method  string
		Headers []HeaderPair
		Body    *bytes.Buffer

		Host string
		Port int
		Path string

		DialTimeout time.Duration
		Timeout     time.Duration

		tlsConf   *TLSConfig
		tlsConfMu sync.RWMutex

		cacheKey string

		timer *time.Timer
	}
)

const (
	neverReached = time.Duration(math.MaxInt64)
)

var (
	poolRequests sync.Pool
)

func init() {
	poolRequests.New = func() interface{} {
		req := request{}
		req.timer = time.NewTimer(neverReached)
		return &req
	}
}

func NewRequest() *request {
	req := poolRequests.Get().(*request)
	req.Reset()
	return req
}

func (r *request) ParseUrl(url_ string) error {
	parsedUrl, err := url.Parse(url_)
	if err != nil {
		return errors.Wrap(err, `Parse URL fail`)
	} else if parsedUrl.Scheme != `https` {
		return errors.Wrap(err, `Only https scheme allowed`)
	}

	if err := r.parseHostPort(parsedUrl); err != nil {
		return errors.Wrap(err, `Parse host and port fail`)
	}

	r.Path = parsedUrl.Path
	if parsedUrl.RawQuery != `` {
		r.Path += `?` + parsedUrl.RawQuery
	}

	return nil
}

func (r *request) Reset() {
	r.Method = `GET`
	r.Headers = r.Headers[0:0]
	if r.Body != nil {
		r.Body.Reset()
	}
	r.Host = ``
	r.Port = 443
	r.Path = ``

	r.timer.Reset(neverReached)

	r.DialTimeout = 1 * time.Second
	r.Timeout = 1 * time.Second

	r.SetTLSConfig(nil)
}

func (r *request) Close() {
	poolRequests.Put(r)
}

func (r *request) SetTLSConfig(config *TLSConfig) {
	r.tlsConfMu.Lock()
	r.tlsConf = config
	r.cacheKey = ``
	r.tlsConfMu.Unlock()
}

func (r *request) getCacheKey() string {
	if r.cacheKey == `` {
		tlsConfPtr := strconv.FormatInt(int64(uintptr(unsafe.Pointer(r.tlsConf))), 16)
		r.cacheKey = r.Host + `:` + strconv.Itoa(r.Port) + `:0x` + tlsConfPtr
	}
	return r.cacheKey
}

func (r *request) parseHostPort(parsedUrl *url.URL) error {
	// ToDo: кешировать разбор ?
	var (
		portStr string
		err     error
	)

	if r.Host, portStr, err = net.SplitHostPort(parsedUrl.Host); err != nil {
		// считаем, что все, что есть - хост
		r.Host = parsedUrl.Host
		r.Port = 443
	} else if r.Port, err = net.LookupPort(`tcp4`, portStr); err != nil {
		return errors.Wrap(err, `Wrong port format`)
	}

	return nil
}
