package main

// The HTTP edge: one small request/response pair the whole forge is written against, and the adapter
// that serves it over net/http. Handlers never see net/http types, so the transport can change
// without touching them.

import (
	"compress/gzip"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Request is one HTTP request, its body read in full.
type Request struct {
	Method   string
	Path     string // the URL path, percent-decoded
	RawPath  string // the URL path as sent
	RawQuery string
	Host     string // the Host header: the origin the client dialed
	Header   http.Header
	Body     []byte

	query url.Values
}

// Query returns the parsed query string.
func (r *Request) Query() url.Values {
	if r.query == nil {
		r.query, _ = url.ParseQuery(r.RawQuery)
		if r.query == nil {
			r.query = url.Values{}
		}
	}
	return r.query
}

// Response is one HTTP reply.
type Response struct {
	Status      int
	ContentType string
	Headers     []string // name, value, name, value ...
	Body        []byte
}

func (r *Response) setHeader(name, value string) *Response {
	r.Headers = append(r.Headers, name, value)
	return r
}

const maxBodyBytes = 512 << 20

func serve(listener net.Listener, app *App) error {
	handler := http.HandlerFunc(func(w http.ResponseWriter, hr *http.Request) {
		req := &Request{
			Method:   hr.Method,
			Path:     hr.URL.Path,
			RawPath:  hr.URL.EscapedPath(),
			RawQuery: hr.URL.RawQuery,
			Host:     hr.Host,
			Header:   hr.Header,
		}
		var bodyErr error
		if hr.Body != nil && hr.Method != http.MethodGet && hr.Method != http.MethodHead {
			var reader io.Reader = io.LimitReader(hr.Body, maxBodyBytes)
			if strings.EqualFold(hr.Header.Get("Content-Encoding"), "gzip") {
				if zr, err := gzip.NewReader(reader); err == nil {
					reader = zr
				} else {
					bodyErr = err
				}
			}
			if bodyErr == nil {
				if hr.ContentLength > 0 {
					buf := make([]byte, 0, hr.ContentLength)
					req.Body, bodyErr = readAllInto(reader, buf)
				} else {
					req.Body, bodyErr = io.ReadAll(reader)
				}
			}
		}
		var res *Response
		if bodyErr != nil {
			res = jsonMessage(400, "Problems reading the request body")
		} else {
			res = app.handle(req)
		}
		header := w.Header()
		for i := 0; i+1 < len(res.Headers); i += 2 {
			header.Set(res.Headers[i], res.Headers[i+1])
		}
		if res.ContentType != "" {
			header.Set("Content-Type", res.ContentType)
		}
		if res.Status != http.StatusNoContent && res.Status != http.StatusNotModified {
			header.Set("Content-Length", strconv.Itoa(len(res.Body)))
		}
		w.WriteHeader(res.Status)
		if hr.Method != http.MethodHead && len(res.Body) > 0 {
			w.Write(res.Body)
		}
	})
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 60 * time.Second,
		IdleTimeout:       10 * time.Minute,
		MaxHeaderBytes:    1 << 20,
	}
	return server.Serve(listener)
}

// readAllInto reads r to its end, appending to buf.
func readAllInto(r io.Reader, buf []byte) ([]byte, error) {
	for {
		if len(buf) == cap(buf) {
			buf = append(buf, 0)[:len(buf)]
		}
		n, err := r.Read(buf[len(buf):cap(buf)])
		buf = buf[:len(buf)+n]
		if err == io.EOF {
			return buf, nil
		}
		if err != nil {
			return buf, err
		}
	}
}
