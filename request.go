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
		prepared bool

		Method  string
		Headers []HeaderPair
		Body    *bytes.Buffer

		// Url имеет больший приоритет, чем Host, Path и Port
		Url  string
		Host string
		Port int
		Path string
		//Ip       net.IP

		DialTimeout time.Duration // ToDo: поддержать
		Timeout     time.Duration // ToDo: поддержать
	}
)

func NewRequest() *request {
	req := request{}
	req.Reset()
	return &req
}

func (r *request) Reset() {
	r.prepared = false

	r.Method = `GET`
	r.Headers = r.Headers[0:0]
	if r.Body != nil {
		r.Body.Reset()
	}
	r.Url = ``
	r.Host = ``
	r.Port = 443
	r.Path = ``
	//r.Ip = r.Ip[0:0]

	r.DialTimeout = 1 * time.Second
	r.Timeout = 1 * time.Second
}

func (r *request) prepare() error {
	if r.prepared {
		return nil
	}

	if r.Url != `` {
		if err := r.parseUrl(); err != nil {
			return errors.Wrap(err, `Cannot parse URL`)
		}
	}

	r.prepared = true

	return nil
}

func (r *request) parseUrl() error {
	parsedUrl, err := url.Parse(r.Url)
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

func (r *request) parseHostPort(parsedUrl *url.URL) error {
	// ToDo: кешировать разбор
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

	/*host := r.Host + `:` + strconv.Itoa(r.Port)
	addr, err := net.ResolveTCPAddr(`tcp`, host)
	if err != nil {
		return errors.Wrap(err, `Cannot resolve URL`)
	}
	r.Ip = addr.IP*/

	return nil
}
