package netcommon

import (
	"net/http"
	"time"
)

const DefaultHTTPTimeout = 30 * time.Second

func sharedTransport() *http.Transport {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Transport{MaxIdleConns: 100, MaxIdleConnsPerHost: 10, IdleConnTimeout: 90 * time.Second}
	}
	t := transport.Clone()
	t.MaxIdleConns = 100
	t.MaxIdleConnsPerHost = 10
	t.IdleConnTimeout = 90 * time.Second
	return t
}

func DefaultHTTPClient() *http.Client {
	return NewHTTPClient(DefaultHTTPTimeout)
}

func NewHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: sharedTransport(),
	}
}
