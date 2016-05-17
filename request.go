package h2client

import (
	"bytes"
	"github.com/pkg/errors"
	"net"
	"net/url"
	"time"
)

type (
	request struct {
		Method  string
		Headers []HeaderPair
		Body    *bytes.Buffer

		Host string
		Port int
		Path string

		DialTimeout time.Duration // ToDo: поддержать
		Timeout     time.Duration // ToDo: поддержать
	}
)

func NewRequest() *request {
	req := request{}
	req.Reset()
	return &req
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

	r.DialTimeout = 1 * time.Second
	r.Timeout = 1 * time.Second
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
