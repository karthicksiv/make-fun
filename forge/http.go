package main

// The HTTP edge: one small request/response pair the whole forge is written against, the HTTP/1.1
// wire format (request heads in, replies out), and a plain goroutine-per-connection server. The
// event loop in eventloop.go serves the same wire format faster; this server is its fallback.
// Nothing here knows what a route is.

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// headerField is one request header as it was sent.
type headerField struct {
	name  string
	value string
}

// Headers is a request's header list.
type Headers []headerField

// Get returns the first value of a header, matched without regard to case ("" when absent).
func (h Headers) Get(name string) string {
	for i := range h {
		if len(h[i].name) == len(name) && strings.EqualFold(h[i].name, name) {
			return h[i].value
		}
	}
	return ""
}

// Request is one HTTP request, its body read in full. Body aliases the connection's buffer: a
// handler may read it until it returns and must copy what it keeps.
type Request struct {
	Method   string
	Path     string // the URL path, percent-decoded
	RawPath  string // the URL path as sent
	RawQuery string
	Host     string // the Host header: the origin the client dialed
	Header   Headers
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

const (
	maxHeadBytes = 1 << 20
	maxBodyBytes = 512 << 20
	// A connection's buffer returns to this size after a request that needed more.
	restingBufferBytes = 64 << 10
)

func statusText(code int) string {
	switch code {
	case 100:
		return "Continue"
	case 200:
		return "OK"
	case 201:
		return "Created"
	case 202:
		return "Accepted"
	case 204:
		return "No Content"
	case 304:
		return "Not Modified"
	case 400:
		return "Bad Request"
	case 401:
		return "Unauthorized"
	case 403:
		return "Forbidden"
	case 404:
		return "Not Found"
	case 405:
		return "Method Not Allowed"
	case 409:
		return "Conflict"
	case 413:
		return "Payload Too Large"
	case 422:
		return "Unprocessable Entity"
	case 500:
		return "Internal Server Error"
	}
	return "Status"
}

var errBadRequest = errors.New("malformed request")

var headTerminator = []byte("\r\n\r\n")

// requestFraming is what a request's head says about its body and its connection.
type requestFraming struct {
	contentLength  int // -1 when the head names none
	chunked        bool
	expectContinue bool
	gzipped        bool
	keepAlive      bool
}

// parseHead reads a request head (everything before the blank line) into req. fields is backing
// storage for the header list, so a connection can reuse one array for every request.
func parseHead(head string, req *Request, fields []headerField) (requestFraming, error) {
	framing := requestFraming{contentLength: -1}
	*req = Request{Header: fields[:0]}
	line, rest, _ := strings.Cut(head, "\r\n")
	method, afterMethod, ok := strings.Cut(line, " ")
	if !ok {
		return framing, errBadRequest
	}
	target, proto, ok := strings.Cut(afterMethod, " ")
	if !ok || !strings.HasPrefix(proto, "HTTP/1.") {
		return framing, errBadRequest
	}
	req.Method = method
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		// absolute-form: drop the scheme and authority.
		afterScheme := target[strings.Index(target, "://")+3:]
		if slash := strings.IndexByte(afterScheme, '/'); slash >= 0 {
			target = afterScheme[slash:]
		} else {
			target = "/"
		}
	}
	req.RawPath, req.RawQuery, _ = strings.Cut(target, "?")
	req.Path = req.RawPath
	if strings.IndexByte(req.RawPath, '%') >= 0 {
		if decoded, err := url.PathUnescape(req.RawPath); err == nil {
			req.Path = decoded
		}
	}
	framing.keepAlive = proto != "HTTP/1.0"
	for rest != "" {
		var field string
		field, rest, _ = strings.Cut(rest, "\r\n")
		colon := strings.IndexByte(field, ':')
		if colon <= 0 {
			continue
		}
		name, value := field[:colon], strings.TrimSpace(field[colon+1:])
		req.Header = append(req.Header, headerField{name, value})
		switch len(name) {
		case 4:
			if strings.EqualFold(name, "host") {
				req.Host = value
			}
		case 6:
			if strings.EqualFold(name, "expect") && strings.EqualFold(value, "100-continue") {
				framing.expectContinue = true
			}
		case 10:
			if strings.EqualFold(name, "connection") {
				lower := strings.ToLower(value)
				if strings.Contains(lower, "close") {
					framing.keepAlive = false
				} else if strings.Contains(lower, "keep-alive") {
					framing.keepAlive = true
				}
			}
		case 14:
			if strings.EqualFold(name, "content-length") {
				n, err := strconv.Atoi(value)
				if err != nil || n < 0 || n > maxBodyBytes {
					return framing, errBadRequest
				}
				framing.contentLength = n
			}
		case 16:
			if strings.EqualFold(name, "content-encoding") && strings.EqualFold(value, "gzip") {
				framing.gzipped = true
			}
		case 17:
			if strings.EqualFold(name, "transfer-encoding") && strings.Contains(strings.ToLower(value), "chunked") {
				framing.chunked = true
			}
		}
	}
	return framing, nil
}

// answer runs a parsed request through the forge, undoing a gzip request encoding first.
func answer(app *App, req *Request, gzipped bool) *Response {
	if gzipped && len(req.Body) > 0 {
		zr, err := gzip.NewReader(bytes.NewReader(req.Body))
		if err != nil {
			return jsonMessage(400, "Problems reading the request body")
		}
		plain, err := io.ReadAll(io.LimitReader(zr, maxBodyBytes))
		if err != nil {
			return jsonMessage(400, "Problems reading the request body")
		}
		req.Body = plain
	}
	return app.handle(req)
}

// appendResponse serialises a reply: status line, headers and body in one run of bytes.
func appendResponse(out []byte, res *Response, keepAlive, headOnly bool) []byte {
	out = append(out, "HTTP/1.1 "...)
	out = strconv.AppendInt(out, int64(res.Status), 10)
	out = append(out, ' ')
	out = append(out, statusText(res.Status)...)
	out = append(out, "\r\n"...)
	if res.ContentType != "" {
		out = append(out, "Content-Type: "...)
		out = append(out, res.ContentType...)
		out = append(out, "\r\n"...)
	}
	for i := 0; i+1 < len(res.Headers); i += 2 {
		out = append(out, res.Headers[i]...)
		out = append(out, ": "...)
		out = append(out, res.Headers[i+1]...)
		out = append(out, "\r\n"...)
	}
	bodiless := res.Status == 204 || res.Status == 304
	if !bodiless {
		out = append(out, "Content-Length: "...)
		out = strconv.AppendInt(out, int64(len(res.Body)), 10)
		out = append(out, "\r\n"...)
	}
	if !keepAlive {
		out = append(out, "Connection: close\r\n"...)
	}
	out = append(out, "\r\n"...)
	if !bodiless && !headOnly {
		out = append(out, res.Body...)
	}
	return out
}

var continueReply = []byte("HTTP/1.1 100 Continue\r\n\r\n")

// ---------------------------------------------------------------- the goroutine server

// serve accepts connections until the listener closes, one goroutine a connection.
func serve(listener net.Listener, app *App) error {
	for {
		conn, err := listener.Accept()
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				continue
			}
			return err
		}
		if tcp, ok := conn.(*net.TCPConn); ok {
			tcp.SetNoDelay(true)
		}
		go (&httpConn{conn: conn, app: app, buf: make([]byte, 16<<10)}).run()
	}
}

// httpConn is one client connection and its buffers.
type httpConn struct {
	conn net.Conn
	app  *App
	buf  []byte // bytes read and not yet consumed are buf[start:end]
	// start and end bound the unconsumed bytes.
	start, end int
	out        []byte
	req        Request
	fields     [24]headerField
}

// fill reads more bytes into the buffer, growing it when it is full.
func (c *httpConn) fill() error {
	if c.start > 0 && c.start == c.end {
		c.start, c.end = 0, 0
	}
	if c.end == len(c.buf) {
		if c.start > 0 {
			c.end = copy(c.buf, c.buf[c.start:c.end])
			c.start = 0
		} else {
			grown := make([]byte, 2*len(c.buf))
			c.end = copy(grown, c.buf[:c.end])
			c.buf = grown
		}
	}
	n, err := c.conn.Read(c.buf[c.end:])
	c.end += n
	if n > 0 {
		return nil
	}
	if err == nil {
		err = io.ErrNoProgress
	}
	return err
}

// need makes sure n unconsumed bytes are buffered.
func (c *httpConn) need(n int) error {
	if c.start+n > len(c.buf) {
		// Make room in one step for a body whose length is known.
		grown := make([]byte, c.end-c.start, n+4096)
		copy(grown, c.buf[c.start:c.end])
		c.buf = grown[:cap(grown)]
		c.end -= c.start
		c.start = 0
	}
	for c.end-c.start < n {
		if err := c.fill(); err != nil {
			return err
		}
	}
	return nil
}

func (c *httpConn) run() {
	defer c.conn.Close()
	for {
		keepAlive, err := c.serveOne()
		if err != nil {
			if err == errBadRequest {
				c.conn.Write(appendResponse(nil, jsonMessage(400, "Bad Request"), false, false))
			}
			return
		}
		if !keepAlive {
			return
		}
		if c.start == c.end {
			c.start, c.end = 0, 0
			if len(c.buf) > restingBufferBytes {
				c.buf = make([]byte, 16<<10)
			}
		}
	}
}

// serveOne reads one request, answers it, and reports whether the connection stays open.
func (c *httpConn) serveOne() (bool, error) {
	// The head: everything up to the blank line.
	scanned := 0
	headEnd := -1
	for {
		if window := c.buf[c.start+scanned : c.end]; len(window) > 0 {
			if i := bytes.Index(window, headTerminator); i >= 0 {
				headEnd = scanned + i
				break
			}
			if scanned = c.end - c.start - 3; scanned < 0 {
				scanned = 0
			}
		}
		if c.end-c.start > maxHeadBytes {
			return false, errBadRequest
		}
		if err := c.fill(); err != nil {
			return false, err
		}
	}
	head := string(c.buf[c.start : c.start+headEnd])
	c.start += headEnd + 4

	req := &c.req
	framing, err := parseHead(head, req, c.fields[:0])
	if err != nil {
		return false, err
	}
	if framing.expectContinue && (framing.chunked || framing.contentLength > 0) {
		if _, err := c.conn.Write(continueReply); err != nil {
			return false, err
		}
	}
	switch {
	case framing.chunked:
		body, err := c.readChunked()
		if err != nil {
			return false, err
		}
		req.Body = body
	case framing.contentLength > 0:
		if err := c.need(framing.contentLength); err != nil {
			return false, err
		}
		req.Body = c.buf[c.start : c.start+framing.contentLength]
		c.start += framing.contentLength
	}
	res := answer(c.app, req, framing.gzipped)
	c.out = appendResponse(c.out[:0], res, framing.keepAlive, req.Method == "HEAD")
	_, err = c.conn.Write(c.out)
	if cap(c.out) > 4*restingBufferBytes {
		c.out = nil
	}
	if err != nil {
		return false, err
	}
	return framing.keepAlive, nil
}

// readChunked decodes a chunked request body.
func (c *httpConn) readChunked() ([]byte, error) {
	var body []byte
	line := func() (string, error) {
		for {
			if i := bytes.Index(c.buf[c.start:c.end], []byte("\r\n")); i >= 0 {
				text := string(c.buf[c.start : c.start+i])
				c.start += i + 2
				return text, nil
			}
			if c.end-c.start > maxHeadBytes {
				return "", errBadRequest
			}
			if err := c.fill(); err != nil {
				return "", err
			}
		}
	}
	for {
		sizeLine, err := line()
		if err != nil {
			return nil, err
		}
		size, err := chunkSize(sizeLine)
		if err != nil || len(body)+size > maxBodyBytes {
			return nil, errBadRequest
		}
		if size == 0 {
			// Trailers, if any, end at an empty line.
			for {
				trailer, err := line()
				if err != nil {
					return nil, err
				}
				if trailer == "" {
					return body, nil
				}
			}
		}
		if err := c.need(size + 2); err != nil {
			return nil, err
		}
		body = append(body, c.buf[c.start:c.start+size]...)
		c.start += size + 2
	}
}

// chunkSize reads a chunk-size line: hex digits, then optional extensions after a semicolon.
func chunkSize(line string) (int, error) {
	if semi := strings.IndexByte(line, ';'); semi >= 0 {
		line = line[:semi]
	}
	size, err := strconv.ParseInt(strings.TrimSpace(line), 16, 32)
	if err != nil || size < 0 {
		return 0, errBadRequest
	}
	return int(size), nil
}
