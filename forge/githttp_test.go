package main

// The push path, measured and checked without sockets: the receive-pack advertisement and the
// receive-pack exchange, shaped like the suite's push workload (one 1 KiB blob, its tree and its
// commit per push, a fresh commit on refs/heads/main each time).

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/binary"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

// whole is an object to put in a pack undeltified.
type whole struct {
	Type ObjType
	Data []byte
}

// packOf builds a version-2 pack of whole objects.
func packOf(objects ...whole) []byte {
	var out bytes.Buffer
	out.WriteString("PACK")
	binary.Write(&out, binary.BigEndian, uint32(2))
	binary.Write(&out, binary.BigEndian, uint32(len(objects)))
	for _, o := range objects {
		var head [16]byte
		out.Write(packEntryHeader(head[:0], o.Type, len(o.Data)))
		zw := zlib.NewWriter(&out)
		zw.Write(o.Data)
		zw.Close()
	}
	sum := sha1.Sum(out.Bytes())
	out.Write(sum[:])
	return out.Bytes()
}

// filler is n bytes of text that differs with seed.
func filler(seed, n int) []byte {
	var b bytes.Buffer
	for i := 0; b.Len() < n; i++ {
		b.WriteString("line " + strconv.Itoa(seed) + "." + strconv.Itoa(i*7919%1000) + " of the pushed change\n")
	}
	return b.Bytes()[:n]
}

// pushChain is count receive-pack request bodies, each adding one commit to refs/heads/main on top
// of the one before, and the commit ids they leave the branch at.
func pushChain(count int) (bodies [][]byte, tips []SHA) {
	parent := zeroSHA
	for i := 0; i < count; i++ {
		blob := filler(i, 1024)
		tree := buildTree([]TreeEntry{{Mode: "100644", Name: "bench-data.txt", ID: hashObject(ObjBlob, blob)}})
		var parents []SHA
		if !parent.IsZero() {
			parents = []SHA{parent}
		}
		sig := Signature{Name: "Conformance", Email: "conformance@make.fun", When: 1700000000 + int64(i), Zone: "+0000"}
		commit := buildCommit(hashObject(ObjTree, tree), parents, sig, sig, "bench: push "+strconv.Itoa(i+1)+"\n")
		tip := hashObject(ObjCommit, commit)
		body := pktLine(nil, parent.String()+" "+tip.String()+" refs/heads/main\x00report-status-v2 side-band-64k ofs-delta agent=makefun-git-client/1.0\n")
		body = pktFlush(body)
		body = append(body, packOf(whole{ObjBlob, blob}, whole{ObjTree, tree}, whole{ObjCommit, commit})...)
		bodies, tips = append(bodies, body), append(tips, tip)
		parent = tip
	}
	return bodies, tips
}

// forgeWithRepo is a forge holding one empty repository of the given name.
func forgeWithRepo(t testing.TB, name string) *App {
	t.Helper()
	app := newApp(testToken, "2026-11-19T00:00:00Z")
	created := coldReply(t, app, request("POST", "/api/v3/user/repos", "forge.test:7100", []string{bearer}, `{"name":"`+name+`"}`))
	if status(created) != "HTTP/1.1 201 Created" {
		t.Fatalf("creating the repository: %s", created)
	}
	return app
}

func gitRequest(method, path string, body []byte) string {
	var b strings.Builder
	b.WriteString(method + " " + path + " HTTP/1.1\r\nhost: forge.test:7100\r\nconnection: keep-alive\r\nuser-agent: makefun-git-client/1.0\r\naccept: */*\r\n" + bearer + "\r\n")
	if body != nil {
		b.WriteString("content-type: application/x-git-receive-pack-request\r\ncontent-length: " + strconv.Itoa(len(body)) + "\r\n")
	}
	b.WriteString("accept-language: *\r\nsec-fetch-mode: cors\r\naccept-encoding: gzip, deflate\r\n\r\n")
	b.Write(body)
	return b.String()
}

func replyBody(reply []byte) []byte {
	_, body, _ := bytes.Cut(reply, headTerminator)
	return body
}

func TestPushChainAdvertisesEachTip(t *testing.T) {
	app := forgeWithRepo(t, "pushes")
	advertise := gitRequest("GET", "/"+defaultLogin+"/pushes.git/info/refs?service=git-receive-pack", nil)
	empty := replyBody(coldReply(t, app, advertise))
	want := pktFlush(pktLine(nil, "# service=git-receive-pack\n"))
	want = pktFlush(pktLine(want, zeroHex+" capabilities^{}\x00"+receivePackCaps+"\n"))
	if !bytes.Equal(empty, want) {
		t.Fatalf("an empty repository advertises\n%q\nwant\n%q", empty, want)
	}
	bodies, tips := pushChain(5)
	for i, body := range bodies {
		pushed := coldReply(t, app, gitRequest("POST", "/"+defaultLogin+"/pushes.git/git-receive-pack", body))
		if status(pushed) != "HTTP/1.1 200 OK" || !bytes.Contains(pushed, []byte("ok refs/heads/main")) {
			t.Fatalf("push %d: %q", i+1, pushed)
		}
		cold := coldReply(t, app, advertise)
		got := replyBody(cold)
		want := pktFlush(pktLine(nil, "# service=git-receive-pack\n"))
		want = pktFlush(pktLine(want, tips[i].String()+" refs/heads/main\x00"+receivePackCaps+"\n"))
		if !bytes.Equal(got, want) {
			t.Fatalf("after push %d the repository advertises\n%q\nwant\n%q", i+1, got, want)
		}
		// The fast path gives the ordinary path's reply for this one-ref repository.
		fast := app.fastReceiveAdvertisement(headOf(advertise), nil)
		if fast == nil || !sameReply(fast, cold) {
			t.Fatalf("after push %d the fast advertisement is not the ordinary one\nfast: %q\ncold: %q", i+1, fast, cold)
		}
	}
	// A second ref, an empty repository, another service or a request that is not the plain case
	// all belong to the ordinary path.
	if fast := forgeWithRepo(t, "empty").fastReceiveAdvertisement(headOf(strings.Replace(advertise, "pushes", "empty", 1)), nil); fast != nil {
		t.Errorf("the fast path advertised an empty repository")
	}
	for name, raw := range map[string]string{
		"upload-pack":      strings.Replace(advertise, "git-receive-pack", "git-upload-pack", 1),
		"no credential":    strings.Replace(advertise, bearer+"\r\n", "", 1),
		"basic credential": strings.Replace(advertise, bearer, "authorization: Basic eDp5", 1),
		"unknown repo":     strings.Replace(advertise, "pushes", "nowhere", 1),
		"extra query":      strings.Replace(advertise, "git-receive-pack", "git-receive-pack&x=1", 1),
		"connection close": strings.Replace(advertise, "connection: keep-alive", "connection: close", 1),
	} {
		if fast := app.fastReceiveAdvertisement(headOf(raw), nil); fast != nil {
			t.Errorf("%s: the fast path answered %q", name, status(fast))
		}
	}
	repo := app.repos[defaultLogin+"/pushes"]
	repo.setRef("refs/tags/v1", tips[0])
	if fast := app.fastReceiveAdvertisement(headOf(advertise), nil); fast != nil {
		t.Errorf("the fast path advertised a repository with two refs")
	}
	two := replyBody(coldReply(t, app, advertise))
	wantTwo := pktFlush(pktLine(nil, "# service=git-receive-pack\n"))
	wantTwo = pktLine(wantTwo, tips[len(tips)-1].String()+" refs/heads/main\x00"+receivePackCaps+"\n")
	wantTwo = pktFlush(pktLine(wantTwo, tips[0].String()+" refs/tags/v1\n"))
	if !bytes.Equal(two, wantTwo) {
		t.Fatalf("two refs advertise\n%q\nwant\n%q", two, wantTwo)
	}
	// Every pushed object is in the store, whole.
	for i := range bodies {
		blob := filler(i, 1024)
		if o := repo.Objects.Get(hashObject(ObjBlob, blob)); o == nil || !bytes.Equal(o.Data, blob) {
			t.Fatalf("the blob of push %d is not in the store as pushed", i+1)
		}
	}
}

// A pull request is also an issue to GET /issues/{number}, with a stored reply of its own: a change
// made to it through GraphQL must show there too.
func TestPullRequestEditedThroughGraphQLReadsBackAsAnIssue(t *testing.T) {
	app := forgeWithRepo(t, "pulls")
	bodies, tips := pushChain(2)
	push := func(body []byte) {
		t.Helper()
		pushed := coldReply(t, app, gitRequest("POST", "/"+defaultLogin+"/pulls.git/git-receive-pack", body))
		if status(pushed) != "HTTP/1.1 200 OK" || !bytes.Contains(pushed, []byte("ok refs/heads/")) {
			t.Fatalf("push: %q", pushed)
		}
	}
	push(bodies[0])
	// The second commit goes to a branch of its own, on top of main.
	feature := bytes.Replace(bodies[1], []byte(tips[0].String()+" "+tips[1].String()+" refs/heads/main"), []byte(zeroHex+" "+tips[1].String()+" refs/heads/feat"), 1)
	push(feature)
	api := "/api/v3/repos/" + defaultLogin + "/pulls"
	opened := coldReply(t, app, request("POST", api+"/pulls", "forge.test:7100", []string{bearer}, `{"title":"first title","head":"feat","base":"main"}`))
	if status(opened) != "HTTP/1.1 201 Created" {
		t.Fatalf("opening the pull request: %s", opened)
	}
	var pull struct {
		Number int    `json:"number"`
		NodeID string `json:"node_id"`
	}
	if err := json.Unmarshal(replyBody(opened), &pull); err != nil || pull.NodeID == "" {
		t.Fatalf("reading the pull request: %v: %s", err, replyBody(opened))
	}
	get := request("GET", api+"/issues/"+strconv.Itoa(pull.Number), "forge.test:7100", []string{bearer}, "")
	if before := coldReply(t, app, get); !bytes.Contains(before, []byte(`"title":"first title"`)) {
		t.Fatalf("the pull request does not read back as an issue: %s", before)
	}
	if app.fastIssueGet(headOf(get)) == nil {
		t.Fatalf("reading the pull request as an issue stored no reply")
	}
	mutation := `{"query":"mutation { updatePullRequest(input: {pullRequestId: \"` + pull.NodeID + `\", title: \"second title\"}) { pullRequest { title } } }"}`
	edited := coldReply(t, app, request("POST", "/api/graphql", "forge.test:7100", []string{bearer}, mutation))
	if !bytes.Contains(edited, []byte(`"title":"second title"`)) {
		t.Fatalf("the GraphQL edit: %s", edited)
	}
	if stale := app.fastIssueGet(headOf(get)); stale != nil {
		t.Fatalf("the stored reply outlived the GraphQL edit: %s", replyBody(stale))
	}
	if after := coldReply(t, app, get); !bytes.Contains(after, []byte(`"title":"second title"`)) {
		t.Fatalf("the edit does not show on the issue: %s", after)
	}
}

// benchPushes times one operation per iteration against a repository that a fresh forge starts
// every few hundred pushes, so the store stays the size a trial's is.
func benchPushes(b *testing.B, advertise, receive bool) {
	const batch = 600
	bodies, _ := pushChain(batch)
	advert := gitRequest("GET", "/"+defaultLogin+"/pushes.git/info/refs?service=git-receive-pack", nil)
	advertHead := advert[:len(advert)-4]
	posts := make([]string, batch)
	for i, body := range bodies {
		posts[i] = gitRequest("POST", "/"+defaultLogin+"/pushes.git/git-receive-pack", body)
	}
	var app *App
	var req Request
	var fields [24]headerField
	var out []byte
	run := func(head string, body []byte) {
		framing, err := parseHead(head, &req, fields[:0])
		if err != nil {
			b.Fatal(err)
		}
		req.Body = body
		out = appendResponse(out[:0], answer(app, &req, framing.gzipped), framing.keepAlive, false)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%batch == 0 {
			b.StopTimer()
			app = forgeWithRepo(b, "pushes")
			b.StartTimer()
		}
		if advertise {
			run(advertHead, nil)
		}
		post := posts[i%batch]
		headEnd := strings.Index(post, "\r\n\r\n")
		if receive {
			run(post[:headEnd], []byte(post[headEnd+4:]))
			if !bytes.Contains(out, []byte("ok refs/heads/main")) {
				b.Fatalf("push refused: %q", out)
			}
		} else {
			b.StopTimer()
			run(post[:headEnd], []byte(post[headEnd+4:]))
			b.StartTimer()
		}
	}
}

func BenchmarkPushAdvertiseFast(b *testing.B) {
	app := forgeWithRepo(b, "pushes")
	bodies, _ := pushChain(1)
	coldReply(b, app, gitRequest("POST", "/"+defaultLogin+"/pushes.git/git-receive-pack", bodies[0]))
	head := headOf(gitRequest("GET", "/"+defaultLogin+"/pushes.git/info/refs?service=git-receive-pack", nil))
	var out []byte
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if out = app.fastReceiveAdvertisement(head, out[:0]); out == nil {
			b.Fatal("the fast path stood aside")
		}
	}
}

func BenchmarkPushAdvertise(b *testing.B) { benchPushes(b, true, false) }
func BenchmarkPushReceive(b *testing.B)   { benchPushes(b, false, true) }
func BenchmarkPushBoth(b *testing.B)      { benchPushes(b, true, true) }
