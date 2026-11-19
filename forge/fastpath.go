package main

// The fast paths: requests the suite sends by the thousand, answered from the bytes of the request
// head without building a Request, routing it or assembling a reply.
//
//	GET /api/v3/repos/{owner}/{repo}/issues/{number}    the issue's stored reply, written as it stands
//
// A fast path takes a request only when it is the plain case in every respect: exactly that route,
// HTTP/1.1 on a connection that stays open, an Authorization header that is exactly "Bearer <the
// trial's token>", and nothing about the head that changes how the request is framed. Everything
// else is left untouched for the ordinary path: another route, a missing or different credential, a
// query string, an escaped or differently cased path, Connection: close, an issue that has no stored
// reply. So the ordinary path still answers every 401, every 404 and every odd request there is, and
// a request either path could take gets the same bytes from both (fastpath_test.go holds them to it).

import "bytes"

var crlf = []byte("\r\n")

// plainHead is what a fast path needs from a request's header lines.
type plainHead struct {
	host          []byte // the Host value
	contentLength int    // 0 when the head names none
}

// scanPlainHead reads a request's header lines (everything after the request line) as parseHead
// would, and reports ok only for a head a fast path may answer: its first Authorization header is
// exactly the trial's bearer credential, it has one Host, and it carries nothing that changes the
// request's framing or closes the connection.
func (a *App) scanPlainHead(lines []byte) (h plainHead, ok bool) {
	if a.bearer == nil {
		return h, false
	}
	authorized, sawAuthorization, sawLength := false, false, false
	for len(lines) > 0 {
		var line []byte
		if end := bytes.Index(lines, crlf); end >= 0 {
			line, lines = lines[:end], lines[end+2:]
		} else {
			line, lines = lines, nil
		}
		colon := bytes.IndexByte(line, ':')
		if colon <= 0 {
			continue
		}
		name := line[:colon]
		switch len(name) {
		case 4:
			if equalFoldASCII(name, "host") {
				if h.host != nil {
					return h, false
				}
				if h.host = bytes.TrimSpace(line[colon+1:]); len(h.host) == 0 {
					return h, false
				}
			}
		case 6:
			if equalFoldASCII(name, "expect") {
				return h, false
			}
		case 10:
			if equalFoldASCII(name, "connection") && !equalFoldASCII(bytes.TrimSpace(line[colon+1:]), "keep-alive") {
				return h, false
			}
		case 13:
			// Only the first Authorization header counts, as it does for the ordinary path.
			if equalFoldASCII(name, "authorization") && !sawAuthorization {
				sawAuthorization = true
				if authorized = bytes.Equal(bytes.TrimSpace(line[colon+1:]), a.bearer); !authorized {
					return h, false
				}
			}
		case 14:
			if equalFoldASCII(name, "content-length") {
				value := bytes.TrimSpace(line[colon+1:])
				if sawLength || len(value) == 0 || len(value) > 7 {
					return h, false
				}
				sawLength = true
				for _, d := range value {
					if d < '0' || d > '9' {
						return h, false
					}
					h.contentLength = h.contentLength*10 + int(d-'0')
				}
			}
		case 16:
			if equalFoldASCII(name, "content-encoding") {
				return h, false
			}
		case 17:
			if equalFoldASCII(name, "transfer-encoding") {
				return h, false
			}
		}
	}
	return h, authorized && h.host != nil
}

// equalFoldASCII reports whether b is the lower-case ASCII word, whatever b's own letter case.
func equalFoldASCII(b []byte, lower string) bool {
	if len(b) != len(lower) {
		return false
	}
	for i := 0; i < len(b); i++ {
		c := b[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != lower[i] {
			return false
		}
	}
	return true
}

// requestLine splits a request head into the target of its request line and its header lines, for a
// head that opens with prefix (the method, a space and the route's first segments) and whose request
// line closes with " HTTP/1.1". target is what lies between the two.
func requestLine(head []byte, prefix string) (target, lines []byte, ok bool) {
	const proto = " HTTP/1.1"
	if len(head) < len(prefix) || string(head[:len(prefix)]) != prefix {
		return nil, nil, false
	}
	end := bytes.Index(head, crlf)
	if end < 0 {
		return nil, nil, false
	}
	line := head[len(prefix):end]
	if len(line) < len(proto) || string(line[len(line)-len(proto):]) != proto {
		return nil, nil, false
	}
	return line[:len(line)-len(proto)], head[end+2:], true
}

// fastIssueGet answers GET /api/v3/repos/{owner}/{repo}/issues/{number} with the issue's stored
// reply. head is the request head without the blank line that ends it. A nil result leaves the
// request to the ordinary path.
func (a *App) fastIssueGet(head []byte) []byte {
	target, lines, ok := requestLine(head, "GET /api/v3/repos/")
	if !ok {
		return nil
	}
	// target is {owner}/{repo}/issues/{number}. The repository is looked up by the bytes as sent:
	// the store's keys are lower-case "owner/name", so a path in any other spelling simply misses.
	slash := bytes.LastIndexByte(target, '/')
	if slash < 0 {
		return nil
	}
	digits := target[slash+1:]
	if len(digits) == 0 || len(digits) > 9 {
		return nil
	}
	number := 0
	for _, d := range digits {
		if d < '0' || d > '9' {
			return nil
		}
		number = number*10 + int(d-'0')
	}
	const issues = "/issues"
	rest := target[:slash]
	if len(rest) <= len(issues) || string(rest[len(rest)-len(issues):]) != issues {
		return nil
	}
	key := rest[:len(rest)-len(issues)]
	h, ok := a.scanPlainHead(lines)
	if !ok || h.contentLength != 0 {
		return nil
	}
	const scheme = "http://"
	var wire []byte
	a.mu.RLock()
	if repo := a.repos[string(key)]; repo != nil {
		if issue := repo.issue(number); issue != nil {
			if reply := issue.cached.Load(); reply != nil && len(reply.origin) == len(scheme)+len(h.host) && reply.origin[len(scheme):] == string(h.host) {
				wire = reply.wire
			}
		}
	}
	a.mu.RUnlock()
	if wire != nil {
		a.requests.Add(1)
	}
	return wire
}
