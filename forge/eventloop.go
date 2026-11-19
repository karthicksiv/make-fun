package main

// The event loop: every connection served by one goroutine over epoll, with non-blocking sockets and
// requests answered inline. A request costs one readiness report, one read and one write, and no
// goroutine is woken on the way.
//
// Between requests the loop sleeps in epoll and uses no CPU. An earlier version kept polling for a
// while after each request to save the wake-up; measured against the suite it gained nothing on two
// CPUs and lost a quarter on one, because the trial's CPUs are shared with the suite that is doing
// the timing. A forge that is idle whenever it has nothing to answer leaves them to the suite.

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"syscall"
	"unsafe"
)

const connBufferBytes = 8 << 10

// Where a chunked request body stands.
const (
	chunkAtSize = iota
	chunkAtData
	chunkAtDataEnd
	chunkAtTrailers
)

// loopConn is one connection of the event loop.
type loopConn struct {
	fd  int
	buf []byte // bytes read and not yet consumed are buf[start:end]
	// start and end bound the unconsumed bytes; scanned is how far the search for the end of the
	// head has got, counted from start.
	start, end, scanned int

	headParsed bool
	framing    requestFraming
	req        Request
	fields     [24]headerField

	chunkState int
	chunkLeft  int
	chunkBody  []byte

	pending    []byte // reply bytes the socket has not taken yet
	closeAfter bool
}

type eventLoop struct {
	app      *App
	epfd     int
	listenFd int
	keep     *os.File // holds the listening descriptor open
	conns    []*loopConn
	out      []byte
	events   [256]syscall.EpollEvent
}

// newEventLoop takes over a listener: the loop accepts on its descriptor directly.
func newEventLoop(listener net.Listener, app *App) (*eventLoop, error) {
	tcp, ok := listener.(*net.TCPListener)
	if !ok {
		return nil, fmt.Errorf("the event loop needs a TCP listener")
	}
	file, err := tcp.File()
	if err != nil {
		return nil, err
	}
	fd := int(file.Fd())
	if err := syscall.SetNonblock(fd, true); err != nil {
		file.Close()
		return nil, err
	}
	epfd, err := syscall.EpollCreate1(syscall.EPOLL_CLOEXEC)
	if err != nil {
		file.Close()
		return nil, err
	}
	event := syscall.EpollEvent{Events: syscall.EPOLLIN, Fd: int32(fd)}
	if err := syscall.EpollCtl(epfd, syscall.EPOLL_CTL_ADD, fd, &event); err != nil {
		syscall.Close(epfd)
		file.Close()
		return nil, err
	}
	// The duplicate now owns the socket; the original listener is no longer needed.
	listener.Close()
	return &eventLoop{app: app, epfd: epfd, listenFd: fd, keep: file}, nil
}

// run serves forever, blocked in epoll whenever nothing is ready. It deliberately does not pin
// itself to an OS thread: nothing here depends on which thread runs it, and an unpinned goroutine
// is rescheduled without a thread hand-off when the runtime preempts it.
func (l *eventLoop) run() {
	for {
		n, err := syscall.EpollWait(l.epfd, l.events[:], -1)
		if err != nil {
			if err == syscall.EINTR {
				continue
			}
			fmt.Fprintf(os.Stderr, "[forge] epoll failed: %v\n", err)
			os.Exit(1)
		}
		for i := 0; i < n; i++ {
			fd := int(l.events[i].Fd)
			if fd == l.listenFd {
				l.accept()
				continue
			}
			if fd < len(l.conns) {
				if c := l.conns[fd]; c != nil {
					l.ready(c, l.events[i].Events)
				}
			}
		}
	}
}

func (l *eventLoop) accept() {
	for {
		nfd, _, errno := syscall.RawSyscall6(syscall.SYS_ACCEPT4, uintptr(l.listenFd), 0, 0, syscall.SOCK_NONBLOCK|syscall.SOCK_CLOEXEC, 0, 0)
		if errno != 0 {
			if errno == syscall.EINTR {
				continue
			}
			return
		}
		fd := int(nfd)
		syscall.SetsockoptInt(fd, syscall.IPPROTO_TCP, syscall.TCP_NODELAY, 1)
		event := syscall.EpollEvent{Events: syscall.EPOLLIN, Fd: int32(fd)}
		if err := syscall.EpollCtl(l.epfd, syscall.EPOLL_CTL_ADD, fd, &event); err != nil {
			syscall.Close(fd)
			continue
		}
		for fd >= len(l.conns) {
			l.conns = append(l.conns, make([]*loopConn, 64)...)
		}
		l.conns[fd] = &loopConn{fd: fd, buf: make([]byte, connBufferBytes)}
	}
}

func (l *eventLoop) close(c *loopConn) {
	syscall.EpollCtl(l.epfd, syscall.EPOLL_CTL_DEL, c.fd, nil)
	syscall.Close(c.fd)
	l.conns[c.fd] = nil
}

// interest sets which readiness the loop is told about for a connection.
func (l *eventLoop) interest(c *loopConn, events uint32) {
	event := syscall.EpollEvent{Events: events, Fd: int32(c.fd)}
	syscall.EpollCtl(l.epfd, syscall.EPOLL_CTL_MOD, c.fd, &event)
}

func (l *eventLoop) ready(c *loopConn, events uint32) {
	if events&syscall.EPOLLOUT != 0 && c.pending != nil {
		if !l.flush(c) {
			return
		}
	}
	if events&syscall.EPOLLIN != 0 {
		l.read(c)
		return
	}
	if events&(syscall.EPOLLERR|syscall.EPOLLHUP) != 0 {
		l.close(c)
	}
}

// read takes what the socket holds and answers every request that is now complete.
func (l *eventLoop) read(c *loopConn) {
	if c.end == len(c.buf) {
		if c.start > 0 {
			c.end = copy(c.buf, c.buf[c.start:c.end])
			c.start = 0
		} else {
			grown := make([]byte, 2*len(c.buf))
			copy(grown, c.buf[:c.end])
			c.buf = grown
		}
	}
	for {
		n, _, errno := syscall.RawSyscall(syscall.SYS_READ, uintptr(c.fd), uintptr(unsafe.Pointer(&c.buf[c.end])), uintptr(len(c.buf)-c.end))
		if errno == syscall.EINTR {
			continue
		}
		if errno == syscall.EAGAIN {
			return
		}
		if errno != 0 || n == 0 {
			l.close(c)
			return
		}
		c.end += int(n)
		break
	}
	l.process(c)
}

// process answers the complete requests in a connection's buffer, in order.
func (l *eventLoop) process(c *loopConn) {
	for c.pending == nil {
		if !c.headParsed {
			window := c.buf[c.start+c.scanned : c.end]
			if len(window) == 0 {
				break
			}
			i := bytes.Index(window, headTerminator)
			if i < 0 {
				if c.scanned = c.end - c.start - 3; c.scanned < 0 {
					c.scanned = 0
				}
				if c.end-c.start > maxHeadBytes {
					l.refuse(c)
					return
				}
				break
			}
			headEnd := c.scanned + i
			if wire := l.app.fastIssueGet(c.buf[c.start : c.start+headEnd]); wire != nil {
				// A stored reply goes out as it stands; the head was never parsed.
				c.start += headEnd + 4
				c.scanned = 0
				if !l.write(c, wire) {
					return
				}
				continue
			}
			if reply, used := l.app.fastIssueCreate(c.buf[c.start:c.start+headEnd], c.buf[c.start+headEnd+4:c.end], l.out[:0]); reply != nil {
				l.out = reply
				c.start += headEnd + 4 + used
				c.scanned = 0
				if !l.write(c, reply) {
					return
				}
				continue
			}
			head := string(c.buf[c.start : c.start+headEnd])
			c.start += headEnd + 4
			c.scanned = 0
			framing, err := parseHead(head, &c.req, c.fields[:0])
			if err != nil {
				l.refuse(c)
				return
			}
			c.framing, c.headParsed = framing, true
			if framing.expectContinue && (framing.chunked || framing.contentLength > 0) {
				syscall.Write(c.fd, continueReply)
			}
			if framing.chunked {
				c.chunkState, c.chunkLeft, c.chunkBody = chunkAtSize, 0, c.chunkBody[:0]
			}
		}
		switch {
		case c.framing.chunked:
			done, err := c.advanceChunked()
			if err != nil {
				l.refuse(c)
				return
			}
			if !done {
				l.settle(c)
				return
			}
			c.req.Body = c.chunkBody
		case c.framing.contentLength > 0:
			need := c.framing.contentLength
			if c.end-c.start < need {
				if c.start+need > len(c.buf) {
					// Make room for the whole body in one step.
					grown := make([]byte, need+4096)
					c.end = copy(grown, c.buf[c.start:c.end])
					c.start = 0
					c.buf = grown
				}
				return
			}
			c.req.Body = c.buf[c.start : c.start+need]
			c.start += need
		}
		c.headParsed = false
		res := answer(l.app, &c.req, c.framing.gzipped)
		l.out = appendResponse(l.out[:0], res, c.framing.keepAlive, c.req.Method == "HEAD")
		if !c.framing.keepAlive {
			c.closeAfter = true
		}
		if !l.write(c, l.out) {
			return
		}
		if cap(l.out) > 4<<20 {
			l.out = nil
		}
		if c.closeAfter && c.pending == nil {
			l.close(c)
			return
		}
	}
	l.settle(c)
}

// settle tidies a connection's buffer between requests.
func (l *eventLoop) settle(c *loopConn) {
	if c.start == c.end {
		c.start, c.end, c.scanned = 0, 0, 0
		if len(c.buf) > restingBufferBytes && !c.headParsed {
			c.buf = make([]byte, connBufferBytes)
		}
		if cap(c.chunkBody) > restingBufferBytes && !c.headParsed {
			c.chunkBody = nil
		}
	}
}

// refuse answers a malformed request and drops the connection.
func (l *eventLoop) refuse(c *loopConn) {
	syscall.Write(c.fd, appendResponse(nil, jsonMessage(400, "Bad Request"), false, false))
	l.close(c)
}

// write sends a reply. It reports false when the connection was closed under it.
func (l *eventLoop) write(c *loopConn, data []byte) bool {
	for len(data) > 0 {
		n, _, errno := syscall.RawSyscall(syscall.SYS_WRITE, uintptr(c.fd), uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)))
		if errno == syscall.EINTR {
			continue
		}
		if errno == syscall.EAGAIN {
			// The socket is full: keep the rest and wait until it drains.
			c.pending = append(c.pending[:0], data...)
			l.interest(c, syscall.EPOLLOUT)
			return true
		}
		if errno != 0 {
			l.close(c)
			return false
		}
		data = data[n:]
	}
	return true
}

// flush continues a reply the socket could not take at once. It reports whether the connection is
// ready for its next request.
func (l *eventLoop) flush(c *loopConn) bool {
	data := c.pending
	for len(data) > 0 {
		n, _, errno := syscall.RawSyscall(syscall.SYS_WRITE, uintptr(c.fd), uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)))
		if errno == syscall.EINTR {
			continue
		}
		if errno == syscall.EAGAIN {
			c.pending = append(c.pending[:0], data...)
			return false
		}
		if errno != 0 {
			l.close(c)
			return false
		}
		data = data[n:]
	}
	c.pending = nil
	if c.closeAfter {
		l.close(c)
		return false
	}
	l.interest(c, syscall.EPOLLIN)
	// Requests that arrived behind the one just answered are already buffered.
	l.process(c)
	return l.conns[c.fd] == c && c.pending == nil
}

// advanceChunked decodes as much of a chunked body as has arrived; done says the body is complete.
func (c *loopConn) advanceChunked() (done bool, err error) {
	for {
		switch c.chunkState {
		case chunkAtSize, chunkAtTrailers:
			i := bytes.Index(c.buf[c.start:c.end], []byte("\r\n"))
			if i < 0 {
				if c.end-c.start > maxHeadBytes {
					return false, errBadRequest
				}
				return false, nil
			}
			line := string(c.buf[c.start : c.start+i])
			c.start += i + 2
			if c.chunkState == chunkAtTrailers {
				if line == "" {
					return true, nil
				}
				continue
			}
			size, err := chunkSize(line)
			if err != nil || len(c.chunkBody)+size > maxBodyBytes {
				return false, errBadRequest
			}
			if size == 0 {
				c.chunkState = chunkAtTrailers
			} else {
				c.chunkLeft, c.chunkState = size, chunkAtData
			}
		case chunkAtData:
			available := c.end - c.start
			if available == 0 {
				return false, nil
			}
			if available > c.chunkLeft {
				available = c.chunkLeft
			}
			c.chunkBody = append(c.chunkBody, c.buf[c.start:c.start+available]...)
			c.start += available
			if c.chunkLeft -= available; c.chunkLeft == 0 {
				c.chunkState = chunkAtDataEnd
			}
		case chunkAtDataEnd:
			if c.end-c.start < 2 {
				return false, nil
			}
			c.start += 2
			c.chunkState = chunkAtSize
		}
	}
}
