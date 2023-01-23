package proxy

import (
	"net/http"
	"net/url"
	"strings"
)

type Logger func(s string)

type HostRewriter struct {
	host   string
	next   http.RoundTripper
	logger Logger
}

func NewHostRewriter(host string, next http.RoundTripper, logger Logger) HostRewriter {
	return HostRewriter{
		host:   host,
		next:   next,
		logger: logger,
	}
}

func (rt HostRewriter) RoundTrip(req *http.Request) (*http.Response, error) {
	urlStr := strings.Replace(req.URL.String(), req.Host, rt.host, 1)
	req.URL, _ = url.Parse(urlStr)

	logStr := "Rewriting host to " + rt.host + " from " + req.Host + " [" + req.URL.String() + "]"

	rt.logger(logStr)

	req.Host = rt.host
	req.URL.Scheme = "http"

	return rt.next.RoundTrip(req)
}
