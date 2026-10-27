package httpx

import (
	"errors"
	"net/http"
	"time"
)

func NewClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 15 * time.Second
	return &http.Client{Transport: transport, Timeout: 35 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 8 {
			return errors.New("too many download redirects")
		}
		if req.URL.Scheme != "https" {
			return errors.New("insecure redirect refused")
		}
		return nil
	}}
}

var Client = NewClient()
