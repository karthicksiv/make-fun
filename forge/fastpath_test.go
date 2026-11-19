package main

// The fast paths are held to the ordinary path: for every request a fast path takes, both must give
// the same bytes (the request id aside), and every request that is not the plain case must be left
// alone.

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const testToken = "shpat_test_0123456789abcdef"

// coldReply answers one complete raw request the way the event loop's ordinary path does.
func coldReply(t testing.TB, app *App, raw string) []byte {
	t.Helper()
	head, body, found := strings.Cut(raw, "\r\n\r\n")
	if !found {
		t.Fatalf("request has no blank line: %q", raw)
	}
	var req Request
	var fields [24]headerField
	framing, err := parseHead(head, &req, fields[:0])
	if err != nil {
		t.Fatalf("parseHead(%q): %v", head, err)
	}
	if framing.contentLength > 0 {
		req.Body = []byte(body[:framing.contentLength])
	}
	res := answer(app, &req, framing.gzipped)
	return appendResponse(nil, res, framing.keepAlive, req.Method == "HEAD")
}

var requestIDLine = regexp.MustCompile(`X-GitHub-Request-Id: [^\r]*\r\n`)

// sameReply compares two replies with their request ids set aside.
func sameReply(a, b []byte) bool {
	blank := []byte("X-GitHub-Request-Id: -\r\n")
	return bytes.Equal(requestIDLine.ReplaceAll(a, blank), requestIDLine.ReplaceAll(b, blank))
}

func status(reply []byte) string {
	line, _, _ := bytes.Cut(reply, crlf)
	return string(line)
}

func request(method, path, host string, headers []string, body string) string {
	var b strings.Builder
	b.WriteString(method + " " + path + " HTTP/1.1\r\n")
	b.WriteString("host: " + host + "\r\n")
	for _, h := range headers {
		b.WriteString(h + "\r\n")
	}
	if body != "" {
		b.WriteString("content-type: application/json\r\ncontent-length: " + strconv.Itoa(len(body)) + "\r\n")
	}
	b.WriteString("\r\n" + body)
	return b.String()
}

var bearer = "authorization: Bearer " + testToken

// forgeWithIssue is a forge holding one repository, widgets, with n issues in it.
func forgeWithIssue(t testing.TB, n int) *App {
	t.Helper()
	app := newApp(testToken, "2026-11-19T00:00:00Z")
	created := coldReply(t, app, request("POST", "/api/v3/user/repos", "forge.test:7100", []string{bearer}, `{"name":"widgets"}`))
	if status(created) != "HTTP/1.1 201 Created" {
		t.Fatalf("creating the repository: %s", created)
	}
	for i := 0; i < n; i++ {
		body := `{"title":"issue ` + strconv.Itoa(i+1) + ` \"quoted\" <tag> & more","body":"line one\nline two\ttabbed é ✓"}`
		made := coldReply(t, app, request("POST", "/api/v3/repos/"+defaultLogin+"/widgets/issues", "forge.test:7100", []string{bearer}, body))
		if status(made) != "HTTP/1.1 201 Created" {
			t.Fatalf("creating issue %d: %s", i+1, made)
		}
	}
	return app
}

func headOf(raw string) []byte {
	head, _, _ := strings.Cut(raw, "\r\n\r\n")
	return []byte(head)
}

func TestFastIssueGetMatchesOrdinaryPath(t *testing.T) {
	app := forgeWithIssue(t, 3)
	path := "/api/v3/repos/" + defaultLogin + "/widgets/issues/"
	host := "forge.test:7100"
	plain := [][]string{
		{bearer},
		{"Authorization: Bearer " + testToken},
		{"AUTHORIZATION:   Bearer " + testToken + "  "},
		{"accept: application/vnd.github+json", bearer, "user-agent: node", "accept-language: *", "sec-fetch-mode: cors", "accept-encoding: gzip, deflate"},
		{bearer, "connection: keep-alive"},
		{bearer, "Connection: Keep-Alive"},
		{bearer, "content-length: 0"},
		{bearer, "authorization: Bearer some-other-token"}, // only the first counts, on both paths
		{bearer, "x-no-colon-here", ": empty-name"},
	}
	for _, headers := range plain {
		for _, number := range []string{"1", "2", "3", "03"} {
			raw := request("GET", path+number, host, headers, "")
			fast := app.fastIssueGet(headOf(raw))
			if fast == nil {
				t.Fatalf("the fast path left a plain request alone: %q", raw)
			}
			cold := coldReply(t, app, raw)
			if status(cold) != "HTTP/1.1 200 OK" {
				t.Fatalf("ordinary path: %s", cold)
			}
			if !sameReply(fast, cold) {
				t.Fatalf("replies differ for %q\nfast: %q\ncold: %q", raw, fast, cold)
			}
		}
	}
}

func TestFastIssueGetLeavesEverythingElse(t *testing.T) {
	app := forgeWithIssue(t, 2)
	path := "/api/v3/repos/" + defaultLogin + "/widgets/issues/1"
	host := "forge.test:7100"
	upper := "/api/v3/repos/" + strings.ToUpper(defaultLogin) + "/widgets/issues/1"
	cases := []struct {
		name string
		raw  string
		want string // the ordinary path's status line
	}{
		{"no credential", request("GET", path, host, nil, ""), "HTTP/1.1 401 Unauthorized"},
		{"wrong token", request("GET", path, host, []string{"authorization: Bearer nope"}, ""), "HTTP/1.1 401 Unauthorized"},
		{"token is a prefix", request("GET", path, host, []string{bearer + "x"}, ""), "HTTP/1.1 401 Unauthorized"},
		{"wrong first credential", request("GET", path, host, []string{"authorization: Bearer nope", bearer}, ""), "HTTP/1.1 401 Unauthorized"},
		{"token scheme", request("GET", path, host, []string{"authorization: token " + testToken}, ""), "HTTP/1.1 200 OK"},
		{"two spaces", request("GET", path, host, []string{"authorization: Bearer  " + testToken}, ""), "HTTP/1.1 200 OK"},
		{"connection close", request("GET", path, host, []string{bearer, "connection: close"}, ""), "HTTP/1.1 200 OK"},
		{"query string", request("GET", path+"?per_page=1", host, []string{bearer}, ""), "HTTP/1.1 200 OK"},
		{"trailing slash", request("GET", path+"/", host, []string{bearer}, ""), "HTTP/1.1 200 OK"},
		{"upper-case owner", request("GET", upper, host, []string{bearer}, ""), "HTTP/1.1 200 OK"},
		{"escaped path", request("GET", strings.Replace(path, "widgets", "widget%73", 1), host, []string{bearer}, ""), "HTTP/1.1 200 OK"},
		{"signed number", request("GET", strings.TrimSuffix(path, "1")+"+1", host, []string{bearer}, ""), "HTTP/1.1 200 OK"},
		{"head", request("HEAD", path, host, []string{bearer}, ""), "HTTP/1.1 200 OK"},
		{"no such issue", request("GET", strings.TrimSuffix(path, "1")+"99", host, []string{bearer}, ""), "HTTP/1.1 404 Not Found"},
		{"issue zero", request("GET", strings.TrimSuffix(path, "1")+"0", host, []string{bearer}, ""), "HTTP/1.1 404 Not Found"},
		{"no such repository", request("GET", strings.Replace(path, "widgets", "gadgets", 1), host, []string{bearer}, ""), "HTTP/1.1 404 Not Found"},
		{"another route", request("GET", path+"/comments", host, []string{bearer}, ""), "HTTP/1.1 200 OK"},
		{"two hosts", request("GET", path, host, []string{"host: other.test", bearer}, ""), "HTTP/1.1 200 OK"},
		{"expect", request("GET", path, host, []string{bearer, "expect: 100-continue"}, ""), "HTTP/1.1 200 OK"},
		{"chunked", "GET " + path + " HTTP/1.1\r\nhost: " + host + "\r\n" + bearer + "\r\ntransfer-encoding: identity\r\n\r\n", "HTTP/1.1 200 OK"},
		{"http 1.0", "GET " + path + " HTTP/1.0\r\nhost: " + host + "\r\n" + bearer + "\r\n\r\n", "HTTP/1.1 200 OK"},
	}
	for _, c := range cases {
		if fast := app.fastIssueGet(headOf(c.raw)); fast != nil {
			t.Errorf("%s: the fast path answered %q", c.name, status(fast))
		}
		if got := status(coldReply(t, app, c.raw)); got != c.want {
			t.Errorf("%s: the ordinary path answered %q, want %q", c.name, got, c.want)
		}
	}
	// No token configured: every request belongs to the ordinary path.
	open := newApp("", "2026-11-19T00:00:00Z")
	if fast := open.fastIssueGet(headOf(request("GET", path, host, []string{bearer}, ""))); fast != nil {
		t.Errorf("a forge without a token took the fast path")
	}
}

func TestStoredIssueReplyFollowsTheIssue(t *testing.T) {
	app := forgeWithIssue(t, 1)
	path := "/api/v3/repos/" + defaultLogin + "/widgets/issues/1"
	get := request("GET", path, "forge.test:7100", []string{bearer}, "")
	before := app.fastIssueGet(headOf(get))
	if before == nil {
		t.Fatalf("a new issue has no stored reply")
	}
	// A change drops the stored reply; the ordinary path renders the next one.
	patched := coldReply(t, app, request("PATCH", path, "forge.test:7100", []string{bearer}, `{"state":"closed"}`))
	if status(patched) != "HTTP/1.1 200 OK" {
		t.Fatalf("closing the issue: %s", patched)
	}
	if stale := app.fastIssueGet(headOf(get)); stale != nil {
		t.Fatalf("the stored reply outlived a change to the issue")
	}
	cold := coldReply(t, app, get)
	if !bytes.Contains(cold, []byte(`"state":"closed"`)) {
		t.Fatalf("the ordinary path did not show the change: %s", cold)
	}
	after := app.fastIssueGet(headOf(get))
	if after == nil || !sameReply(after, cold) {
		t.Fatalf("the reply stored after the change is not the ordinary path's\nfast: %q\ncold: %q", after, cold)
	}
	// A reply names the origin it was rendered for; another origin gets its own.
	elsewhere := request("GET", path, "127.0.0.1:9", []string{bearer}, "")
	if wrong := app.fastIssueGet(headOf(elsewhere)); wrong != nil {
		t.Fatalf("a reply rendered for one origin answered another")
	}
	other := coldReply(t, app, elsewhere)
	if !bytes.Contains(other, []byte(`"url":"http://127.0.0.1:9/api/v3/repos/`)) {
		t.Fatalf("the ordinary path did not render the new origin: %s", other)
	}
	if moved := app.fastIssueGet(headOf(elsewhere)); moved == nil || !sameReply(moved, other) {
		t.Fatalf("the reply stored for the new origin is not the ordinary path's")
	}
}

func BenchmarkIssueGetFast(b *testing.B) {
	app := forgeWithIssue(b, 1)
	coldReply(b, app, suiteGet()) // stores the reply for the origin the benchmark dials
	head := headOf(suiteGet())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if app.fastIssueGet(head) == nil {
			b.Fatal("the fast path stood aside")
		}
	}
}

func BenchmarkIssueGetOrdinary(b *testing.B) {
	app := forgeWithIssue(b, 1)
	raw := []byte(suiteGet())
	headEnd := bytes.Index(raw, headTerminator)
	var req Request
	var fields [24]headerField
	var out []byte
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		framing, err := parseHead(string(raw[:headEnd]), &req, fields[:0])
		if err != nil {
			b.Fatal(err)
		}
		out = appendResponse(out[:0], answer(app, &req, framing.gzipped), framing.keepAlive, false)
	}
}

// suiteGet is a GET of an issue with the header lines the suite's client sends.
func suiteGet() string {
	return request("GET", "/api/v3/repos/"+defaultLogin+"/widgets/issues/1", "127.0.0.1:44165", []string{
		"connection: keep-alive",
		"accept: application/vnd.github+json",
		"user-agent: makefun-suite",
		bearer,
		"accept-language: *",
		"sec-fetch-mode: cors",
		"accept-encoding: gzip, deflate",
	}, "")
}
