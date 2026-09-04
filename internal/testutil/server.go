// Package testutil supplies a local KIS-compatible HTTP stub for package tests.
package testutil

import (
	"net/http"
	"net/http/httptest"
)

type Handler func(*http.Request) (status int, headers http.Header, body string)

type Server struct {
	*httptest.Server
	Requests []*http.Request
}

func New(handler Handler) *Server {
	s := &Server{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.Requests = append(s.Requests, r.Clone(r.Context()))
		status, headers, body := handler(r)
		for key, values := range headers {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	return s
}
