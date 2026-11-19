package main

// Git smart HTTP: reference discovery, upload-pack (protocol v0 and v2, shallow included) and
// receive-pack (v0), served from a repository's in-memory objects and refs.

import (
	"bytes"
	"crypto/sha1"
	"strconv"
	"strings"
)

const gitAgent = "agent=forge/1.0"

// uploadPackCaps is what the v0 upload-pack advertisement offers.
const uploadPackCaps = "multi_ack multi_ack_detailed thin-pack side-band side-band-64k ofs-delta shallow deepen-since deepen-not deepen-relative no-progress include-tag allow-tip-sha1-in-want allow-reachable-sha1-in-want filter object-format=sha1 " + gitAgent

// receivePackCaps is what the receive-pack advertisement offers.
const receivePackCaps = "report-status report-status-v2 delete-refs side-band-64k ofs-delta quiet atomic object-format=sha1 " + gitAgent

type gitCacheEntry struct {
	gen  uint64
	body []byte
}

func gitResponse(status int, contentType string, body []byte) *Response {
	return &Response{
		Status:      status,
		ContentType: contentType,
		Headers:     []string{"Cache-Control", "no-cache, max-age=0, must-revalidate", "Expires", "Fri, 01 Jan 1980 00:00:00 GMT", "Pragma", "no-cache"},
		Body:        body,
	}
}

func gitText(status int, text string) *Response {
	return &Response{Status: status, ContentType: "text/plain; charset=utf-8", Body: []byte(text)}
}

// wantsV2 reads the Git-Protocol header.
func wantsV2(req *Request) bool {
	for _, part := range strings.Split(req.Header.Get("Git-Protocol"), ":") {
		if strings.TrimSpace(part) == "version=2" {
			return true
		}
	}
	return false
}

// handleGit serves /{owner}/{repo}.git/{rest}.
func (a *App) handleGit(req *Request, owner, name, rest string) *Response {
	repo := a.repo(owner, name)
	switch {
	case rest == "info/refs" && (req.Method == "GET" || req.Method == "HEAD"):
		service := req.Query().Get("service")
		if service != "git-upload-pack" && service != "git-receive-pack" {
			if repo == nil {
				return gitText(404, "Repository not found.\n")
			}
			return gitText(403, "Please upgrade your git client: the dumb HTTP protocol is not served.\n")
		}
		if repo == nil {
			return gitText(404, "Repository not found.\n")
		}
		if service == "git-upload-pack" {
			if wantsV2(req) {
				return gitResponse(200, "application/x-git-upload-pack-advertisement", a.cachedGit(repo, "adv2", nil, func() []byte { return advertiseV2() }))
			}
			return gitResponse(200, "application/x-git-upload-pack-advertisement", a.cachedGit(repo, "adv0", nil, func() []byte {
				return advertiseV0(repo, "git-upload-pack", uploadPackCaps+" symref=HEAD:"+repo.headRef())
			}))
		}
		return gitResponse(200, "application/x-git-receive-pack-advertisement", advertiseV0(repo, "git-receive-pack", receivePackCaps))
	case rest == "git-upload-pack" && req.Method == "POST":
		if repo == nil {
			return gitText(404, "Repository not found.\n")
		}
		if wantsV2(req) {
			body := a.cachedGit(repo, "up2", req.Body, func() []byte { return uploadPackV2(repo, req.Body) })
			return gitResponse(200, "application/x-git-upload-pack-result", body)
		}
		body := a.cachedGit(repo, "up0", req.Body, func() []byte { return uploadPackV0(repo, req.Body) })
		return gitResponse(200, "application/x-git-upload-pack-result", body)
	case rest == "git-receive-pack" && req.Method == "POST":
		if repo == nil {
			return gitText(404, "Repository not found.\n")
		}
		return gitResponse(200, "application/x-git-receive-pack-result", a.receivePack(repo, req.Body))
	}
	if repo == nil {
		return gitText(404, "Repository not found.\n")
	}
	return gitText(404, "Not Found\n")
}

// cachedGit memoises a read-only git answer for as long as the repository's refs stay put: the same
// request against the same refs has the same answer, byte for byte.
func (a *App) cachedGit(repo *Repo, kind string, request []byte, build func() []byte) []byte {
	var key string
	if len(request) <= 256 {
		key = kind + ":" + string(request)
	} else {
		sum := sha1.Sum(request)
		key = kind + "#" + string(sum[:])
	}
	gen := repo.gen.Load()
	if cached, ok := repo.gitCache.Load(key); ok {
		if entry := cached.(*gitCacheEntry); entry.gen == gen {
			return entry.body
		}
	}
	body := build()
	if repo.gen.Load() == gen {
		repo.gitCache.Store(key, &gitCacheEntry{gen: gen, body: body})
	}
	return body
}

// ---------------------------------------------------------------- advertisements

func advertiseV2() []byte {
	out := make([]byte, 0, 256)
	out = pktLine(out, "version 2\n")
	out = pktLine(out, gitAgent+"\n")
	out = pktLine(out, "ls-refs=unborn\n")
	out = pktLine(out, "fetch=shallow wait-for-done filter\n")
	out = pktLine(out, "server-option\n")
	out = pktLine(out, "object-format=sha1\n")
	return pktFlush(out)
}

// advertiseV0 writes the v0/v1 reference advertisement of a service.
func advertiseV0(repo *Repo, service string, caps string) []byte {
	out := make([]byte, 0, 1024)
	out = pktLine(out, "# service="+service+"\n")
	out = pktFlush(out)
	refs := repo.sortedRefs()
	head := repo.Ref(repo.headRef())
	first := true
	emit := func(sha SHA, name string) {
		line := sha.String() + " " + name
		if first {
			line += "\x00" + caps
			first = false
		}
		out = pktLine(out, line+"\n")
	}
	if service == "git-upload-pack" && !head.IsZero() {
		emit(head, "HEAD")
	}
	for _, ref := range refs {
		emit(ref.SHA, ref.Name)
		if service == "git-upload-pack" && strings.HasPrefix(ref.Name, "refs/tags/") {
			if peeled := repo.peel(ref.SHA); peeled != ref.SHA {
				emit(peeled, ref.Name+"^{}")
			}
		}
	}
	if first {
		out = pktLine(out, zeroHex+" capabilities^{}\x00"+caps+"\n")
	}
	return pktFlush(out)
}

// ---------------------------------------------------------------- what to send

type fetchRequest struct {
	wants        []SHA
	haves        []SHA
	shallows     []SHA // commits the client holds as shallow
	depth        int   // 0: no limit
	deepenRel    bool
	done         bool
	caps         map[string]bool
	filterBlobs  bool // filter blob:none
	includeTag   bool
	deepenGiven  bool
	waitForDone  bool
	sidebandAll  bool
	deepenSince  int64
	hasDeepenArg bool
}

type fetchPlan struct {
	objects   []*Object
	shallow   []SHA // commits that become shallow boundaries for the client
	unshallow []SHA // client shallows whose parents are now being sent
}

// plan works out which objects answer a fetch: everything reachable from the wants, cut at the
// requested depth, minus what the client's haves already reach.
func (repo *Repo) plan(fr *fetchRequest, common []SHA) *fetchPlan {
	store := repo.Objects
	p := &fetchPlan{}

	clientShallow := make(map[SHA]bool, len(fr.shallows))
	for _, s := range fr.shallows {
		clientShallow[s] = true
	}

	// Commits the client has: everything reachable from the common haves, stopping at its shallows.
	have := map[SHA]bool{}
	stack := append([]SHA(nil), common...)
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if have[id] {
			continue
		}
		have[id] = true
		if clientShallow[id] {
			continue
		}
		if c := store.Get(id).AsCommit(); c != nil {
			stack = append(stack, c.Parents...)
		}
	}

	depth := fr.depth
	if fr.deepenRel && depth > 0 {
		// Relative deepening: the new boundary is `depth` commits below the current shallow points.
		depth = 0
	}

	type queued struct {
		id    SHA
		depth int
	}
	var commits []*Commit
	seenCommit := map[SHA]int{}
	sentObjects := map[SHA]bool{}
	newShallow := map[SHA]bool{}
	var order []*Object
	var boundary []SHA // commits the client has that border what is sent: their trees need not travel

	queue := make([]queued, 0, len(fr.wants))
	for _, w := range fr.wants {
		// A want may be an annotated tag: send the tag object and walk on from what it names.
		id := w
		for i := 0; i < 8; i++ {
			o := store.Get(id)
			if o == nil || o.Type != ObjTag {
				break
			}
			if !sentObjects[id] {
				sentObjects[id] = true
				order = append(order, o)
			}
			target, ok := tagTarget(o.Data)
			if !ok {
				break
			}
			id = target
		}
		queue = append(queue, queued{id, 1})
	}
	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]
		if prev, ok := seenCommit[item.id]; ok && prev <= item.depth {
			continue
		}
		o := store.Get(item.id)
		c := o.AsCommit()
		if c == nil {
			// A want that is a tree or a blob is sent as it is.
			if o != nil && !sentObjects[item.id] {
				repo.addTree(o, sentObjects, &order, nil, fr.filterBlobs)
			}
			continue
		}
		_, revisit := seenCommit[item.id]
		seenCommit[item.id] = item.depth
		if have[item.id] && !clientShallow[item.id] {
			boundary = append(boundary, item.id)
			continue
		}
		if !revisit {
			commits = append(commits, c)
		}
		if depth > 0 && item.depth >= depth {
			if len(c.Parents) > 0 {
				newShallow[item.id] = true
			}
			continue
		}
		delete(newShallow, item.id)
		if clientShallow[item.id] && len(c.Parents) > 0 && (depth == 0 || item.depth < depth) {
			p.unshallow = append(p.unshallow, item.id)
		}
		for _, parent := range c.Parents {
			queue = append(queue, queued{parent, item.depth + 1})
		}
	}

	// Trees and blobs the client already holds through the bordering commits it has.
	skip := map[SHA]bool{}
	for _, id := range boundary {
		if c := store.Get(id).AsCommit(); c != nil {
			repo.markTree(c.Tree, skip)
		}
	}
	for _, c := range commits {
		if have[c.ID] {
			// A client shallow being deepened: the commit itself is already there.
			continue
		}
		if !sentObjects[c.ID] {
			sentObjects[c.ID] = true
			order = append(order, store.Get(c.ID))
		}
	}
	for _, c := range commits {
		if have[c.ID] {
			continue
		}
		if tree := store.Get(c.Tree); tree != nil {
			repo.addTree(tree, sentObjects, &order, skip, fr.filterBlobs)
		}
	}
	for id := range newShallow {
		if !clientShallow[id] {
			p.shallow = append(p.shallow, id)
		}
	}
	sortSHAs(p.shallow)
	sortSHAs(p.unshallow)
	p.objects = order
	return p
}

func sortSHAs(ids []SHA) {
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && bytes.Compare(ids[j][:], ids[j-1][:]) < 0; j-- {
			ids[j], ids[j-1] = ids[j-1], ids[j]
		}
	}
}

// addTree appends an object and, for a tree, everything under it that is not sent or skipped yet.
func (repo *Repo) addTree(o *Object, sent map[SHA]bool, order *[]*Object, skip map[SHA]bool, noBlobs bool) {
	if o == nil || sent[o.ID] || skip[o.ID] {
		return
	}
	if o.Type == ObjBlob && noBlobs {
		return
	}
	sent[o.ID] = true
	*order = append(*order, o)
	if o.Type != ObjTree {
		return
	}
	for _, e := range o.AsTree() {
		if e.Mode == "160000" {
			continue // a submodule's commit lives in another repository
		}
		repo.addTree(repo.Objects.Get(e.ID), sent, order, skip, noBlobs)
	}
}

// markTree records a tree and everything under it.
func (repo *Repo) markTree(id SHA, marked map[SHA]bool) {
	if marked[id] {
		return
	}
	marked[id] = true
	o := repo.Objects.Get(id)
	if o == nil || o.Type != ObjTree {
		return
	}
	for _, e := range o.AsTree() {
		if e.IsTree() {
			repo.markTree(e.ID, marked)
		} else {
			marked[e.ID] = true
		}
	}
}

// commonHaves keeps the haves this repository holds as commits.
func (repo *Repo) commonHaves(haves []SHA) []SHA {
	var out []SHA
	for _, h := range haves {
		if o := repo.Objects.Get(h); o != nil {
			out = append(out, h)
		}
	}
	return out
}

// ---------------------------------------------------------------- upload-pack, protocol v0

func parseCaps(text string) map[string]bool {
	caps := map[string]bool{}
	for _, c := range strings.Fields(text) {
		caps[c] = true
	}
	return caps
}

func uploadPackV0(repo *Repo, body []byte) []byte {
	fr := &fetchRequest{caps: map[string]bool{}}
	rest := body
	for len(rest) > 0 {
		pkt, next, err := readPacket(rest)
		if err != nil {
			break
		}
		rest = next
		if pkt.kind != pktData {
			continue
		}
		line := trimLF(pkt.payload)
		switch {
		case strings.HasPrefix(line, "want "):
			fields := line[5:]
			if len(fields) >= 40 {
				if sha, ok := parseSHA(fields[:40]); ok {
					fr.wants = append(fr.wants, sha)
				}
				if len(fields) > 40 {
					for cap := range parseCaps(fields[40:]) {
						fr.caps[cap] = true
					}
				}
			}
		case strings.HasPrefix(line, "have "):
			if sha, ok := parseSHA(strings.TrimSpace(line[5:])); ok {
				fr.haves = append(fr.haves, sha)
			}
		case strings.HasPrefix(line, "shallow "):
			if sha, ok := parseSHA(strings.TrimSpace(line[8:])); ok {
				fr.shallows = append(fr.shallows, sha)
			}
		case strings.HasPrefix(line, "deepen "):
			fr.depth, _ = strconv.Atoi(strings.TrimSpace(line[7:]))
			fr.hasDeepenArg = true
		case strings.HasPrefix(line, "deepen-since "), strings.HasPrefix(line, "deepen-not "):
			fr.hasDeepenArg = true
		case strings.HasPrefix(line, "filter "):
			fr.filterBlobs = strings.TrimSpace(line[7:]) == "blob:none"
		case line == "done":
			fr.done = true
		}
	}
	if fr.caps["deepen-relative"] && fr.hasDeepenArg {
		fr.deepenRel = len(fr.shallows) > 0
	}
	out := make([]byte, 0, 4096)
	if len(fr.wants) == 0 {
		return pktFlush(out)
	}
	for _, w := range fr.wants {
		if !repo.Objects.Has(w) {
			return pktLine(out, "ERR upload-pack: not our ref "+w.String()+"\n")
		}
	}
	common := repo.commonHaves(fr.haves)
	plan := repo.plan(fr, common)

	if fr.hasDeepenArg || len(fr.shallows) > 0 {
		for _, s := range plan.shallow {
			out = pktLine(out, "shallow "+s.String()+"\n")
		}
		for _, s := range plan.unshallow {
			out = pktLine(out, "unshallow "+s.String()+"\n")
		}
		out = pktFlush(out)
	}

	detailed := fr.caps["multi_ack_detailed"]
	multi := fr.caps["multi_ack"]
	switch {
	case detailed:
		for _, c := range common {
			out = pktLine(out, "ACK "+c.String()+" common\n")
		}
	case multi:
		for _, c := range common {
			out = pktLine(out, "ACK "+c.String()+" continue\n")
		}
	}
	if !fr.done {
		// The client is still negotiating: say what is common and wait for its next round.
		if !detailed && !multi && len(common) > 0 {
			return pktLine(out, "ACK "+common[0].String()+"\n")
		}
		if detailed && len(common) > 0 {
			out = pktLine(out, "ACK "+common[len(common)-1].String()+" ready\n")
		}
		return pktLine(out, "NAK\n")
	}
	if len(common) > 0 {
		last := common[len(common)-1]
		if !detailed && !multi {
			last = common[0]
		}
		out = pktLine(out, "ACK "+last.String()+"\n")
	} else {
		out = pktLine(out, "NAK\n")
	}

	pack := writePack(plan.objects)
	switch {
	case fr.caps["side-band-64k"]:
		out = sideBand(out, 1, pack, true)
		out = pktFlush(out)
	case fr.caps["side-band"]:
		out = sideBand(out, 1, pack, false)
		out = pktFlush(out)
	default:
		out = append(out, pack...)
	}
	return out
}

// ---------------------------------------------------------------- upload-pack, protocol v2

func uploadPackV2(repo *Repo, body []byte) []byte {
	command := ""
	var args []string
	rest := body
	inArgs := false
	for len(rest) > 0 {
		pkt, next, err := readPacket(rest)
		if err != nil {
			break
		}
		rest = next
		if pkt.kind == pktDelimKind {
			inArgs = true
			continue
		}
		if pkt.kind == pktFlushKind {
			break
		}
		if pkt.kind != pktData {
			continue
		}
		line := trimLF(pkt.payload)
		if !inArgs {
			if strings.HasPrefix(line, "command=") {
				command = line[len("command="):]
			}
			continue
		}
		args = append(args, line)
	}
	switch command {
	case "ls-refs":
		return lsRefsV2(repo, args)
	case "fetch":
		return fetchV2(repo, args)
	case "object-info":
		out := pktLine(nil, "size\n")
		for _, arg := range args {
			if strings.HasPrefix(arg, "oid ") {
				if sha, ok := parseSHA(strings.TrimSpace(arg[4:])); ok {
					if o := repo.Objects.Get(sha); o != nil {
						out = pktLine(out, sha.String()+" "+strconv.Itoa(len(o.Data))+"\n")
					}
				}
			}
		}
		return pktFlush(out)
	}
	return pktLine(nil, "ERR unknown command '"+command+"'\n")
}

func lsRefsV2(repo *Repo, args []string) []byte {
	var prefixes []string
	symrefs, peel, unborn := false, false, false
	for _, arg := range args {
		switch {
		case arg == "symrefs":
			symrefs = true
		case arg == "peel":
			peel = true
		case arg == "unborn":
			unborn = true
		case strings.HasPrefix(arg, "ref-prefix "):
			prefixes = append(prefixes, arg[len("ref-prefix "):])
		}
	}
	matches := func(name string) bool {
		if len(prefixes) == 0 {
			return true
		}
		for _, p := range prefixes {
			if strings.HasPrefix(name, p) {
				return true
			}
		}
		return false
	}
	out := make([]byte, 0, 1024)
	if matches("HEAD") {
		head := repo.Ref(repo.headRef())
		if !head.IsZero() {
			line := head.String() + " HEAD"
			if symrefs {
				line += " symref-target:" + repo.headRef()
			}
			out = pktLine(out, line+"\n")
		} else if unborn && symrefs {
			out = pktLine(out, "unborn HEAD symref-target:"+repo.headRef()+"\n")
		}
	}
	for _, ref := range repo.sortedRefs() {
		if !matches(ref.Name) {
			continue
		}
		line := ref.SHA.String() + " " + ref.Name
		if peel && strings.HasPrefix(ref.Name, "refs/tags/") {
			if peeled := repo.peel(ref.SHA); peeled != ref.SHA {
				line += " peeled:" + peeled.String()
			}
		}
		out = pktLine(out, line+"\n")
	}
	return pktFlush(out)
}

func fetchV2(repo *Repo, args []string) []byte {
	fr := &fetchRequest{caps: map[string]bool{}}
	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "want "):
			if sha, ok := parseSHA(strings.TrimSpace(arg[5:])); ok {
				fr.wants = append(fr.wants, sha)
			}
		case strings.HasPrefix(arg, "want-ref "):
			if sha := repo.Ref(strings.TrimSpace(arg[9:])); !sha.IsZero() {
				fr.wants = append(fr.wants, sha)
			}
		case strings.HasPrefix(arg, "have "):
			if sha, ok := parseSHA(strings.TrimSpace(arg[5:])); ok {
				fr.haves = append(fr.haves, sha)
			}
		case strings.HasPrefix(arg, "shallow "):
			if sha, ok := parseSHA(strings.TrimSpace(arg[8:])); ok {
				fr.shallows = append(fr.shallows, sha)
			}
		case strings.HasPrefix(arg, "deepen "):
			fr.depth, _ = strconv.Atoi(strings.TrimSpace(arg[7:]))
			fr.hasDeepenArg = true
		case strings.HasPrefix(arg, "deepen-since "), strings.HasPrefix(arg, "deepen-not "):
			fr.hasDeepenArg = true
		case arg == "deepen-relative":
			fr.deepenRel = true
		case strings.HasPrefix(arg, "filter "):
			fr.filterBlobs = strings.TrimSpace(arg[7:]) == "blob:none"
		case arg == "done":
			fr.done = true
		case arg == "wait-for-done":
			fr.waitForDone = true
		case arg == "include-tag":
			fr.includeTag = true
		case arg == "no-progress", arg == "thin-pack", arg == "ofs-delta", arg == "sideband-all":
			fr.caps[arg] = true
		}
	}
	out := make([]byte, 0, 4096)
	for _, w := range fr.wants {
		if !repo.Objects.Has(w) {
			return pktLine(out, "ERR upload-pack: not our ref "+w.String()+"\n")
		}
	}
	common := repo.commonHaves(fr.haves)
	if !fr.done {
		out = pktLine(out, "acknowledgments\n")
		if len(common) == 0 {
			out = pktLine(out, "NAK\n")
		}
		for _, c := range common {
			out = pktLine(out, "ACK "+c.String()+"\n")
		}
		ready := len(common) > 0 && !fr.waitForDone
		if !ready {
			return pktFlush(out)
		}
		out = pktLine(out, "ready\n")
		out = pktDelim(out)
	}
	if len(fr.wants) == 0 {
		return pktFlush(out)
	}
	if fr.deepenRel && len(fr.shallows) == 0 {
		fr.deepenRel = false
	}
	plan := repo.plan(fr, common)
	if fr.hasDeepenArg || len(fr.shallows) > 0 {
		out = pktLine(out, "shallow-info\n")
		for _, s := range plan.shallow {
			out = pktLine(out, "shallow "+s.String()+"\n")
		}
		for _, s := range plan.unshallow {
			out = pktLine(out, "unshallow "+s.String()+"\n")
		}
		out = pktDelim(out)
	}
	out = pktLine(out, "packfile\n")
	out = sideBand(out, 1, writePack(plan.objects), true)
	return pktFlush(out)
}

// ---------------------------------------------------------------- receive-pack

type refCommand struct {
	oldID SHA
	newID SHA
	name  string
}

// receivePack applies a push: it unpacks the objects, checks every ref update, moves the refs that
// pass, and answers with a report-status.
func (a *App) receivePack(repo *Repo, body []byte) []byte {
	var commands []refCommand
	caps := map[string]bool{}
	rest := body
	first := true
	for len(rest) > 0 {
		pkt, next, err := readPacket(rest)
		if err != nil {
			break
		}
		rest = next
		if pkt.kind == pktFlushKind {
			break
		}
		if pkt.kind != pktData {
			continue
		}
		line := string(pkt.payload)
		if first {
			first = false
			if nul := strings.IndexByte(line, 0); nul >= 0 {
				caps = parseCaps(line[nul+1:])
				line = line[:nul]
			}
		}
		line = strings.TrimRight(line, "\n")
		if strings.HasPrefix(line, "shallow ") || strings.HasPrefix(line, "push-cert") {
			continue
		}
		fields := strings.SplitN(line, " ", 3)
		if len(fields) != 3 {
			continue
		}
		oldID, ok1 := parseSHA(fields[0])
		newID, ok2 := parseSHA(fields[1])
		if !ok1 || !ok2 {
			continue
		}
		commands = append(commands, refCommand{oldID, newID, fields[2]})
	}

	unpack := "ok"
	var incoming []*Object
	if len(rest) >= 12 && string(rest[:4]) == "PACK" {
		objects, err := parsePack(rest, repo.Objects.Get)
		if err != nil {
			unpack = err.Error()
		} else {
			incoming = objects
		}
	} else {
		needsPack := false
		for _, c := range commands {
			if !c.newID.IsZero() && !repo.Objects.Has(c.newID) {
				needsPack = true
			}
		}
		if needsPack {
			unpack = "missing pack"
		}
	}
	if unpack == "ok" {
		for _, o := range incoming {
			repo.Objects.PutObject(o)
		}
	}

	report := make([]byte, 0, 256)
	report = pktLine(report, "unpack "+unpack+"\n")
	changed := false
	for _, c := range commands {
		reason := ""
		if unpack != "ok" {
			reason = "unpacker error"
		} else {
			reason = a.applyRefCommand(repo, c)
		}
		if reason == "" {
			report = pktLine(report, "ok "+c.name+"\n")
			changed = true
		} else {
			report = pktLine(report, "ng "+c.name+" "+reason+"\n")
		}
	}
	report = pktFlush(report)
	if changed {
		a.mu.Lock()
		repo.PushedAt = a.now()
		repo.UpdatedAt = repo.PushedAt
		a.mu.Unlock()
	}
	if caps["side-band-64k"] || caps["side-band"] {
		out := sideBand(make([]byte, 0, len(report)+16), 1, report, caps["side-band-64k"])
		return pktFlush(out)
	}
	return report
}

// applyRefCommand checks and performs one ref update; it returns the reason it was refused, or "".
func (a *App) applyRefCommand(repo *Repo, c refCommand) string {
	if !strings.HasPrefix(c.name, "refs/") || strings.Contains(c.name, "..") || strings.HasSuffix(c.name, "/") || strings.ContainsAny(c.name, " ~^:?*[\\") {
		return "funny refname"
	}
	repo.refMu.Lock()
	current := repo.refs[c.name]
	if c.newID.IsZero() {
		if current.IsZero() {
			repo.refMu.Unlock()
			return "no such ref"
		}
		delete(repo.refs, c.name)
		repo.refMu.Unlock()
		repo.gen.Add(1)
		return ""
	}
	if !repo.Objects.Has(c.newID) {
		repo.refMu.Unlock()
		return "missing necessary objects"
	}
	if current != c.oldID {
		// The pusher's idea of the ref is stale. A push that would still fast-forward the real tip
		// is taken; anything else is refused.
		if !current.IsZero() && !repo.Objects.isAncestor(current, c.newID) {
			repo.refMu.Unlock()
			if c.oldID.IsZero() {
				return "already exists"
			}
			return "non-fast-forward"
		}
	}
	if !current.IsZero() && strings.HasPrefix(c.name, "refs/heads/") && !repo.Objects.isAncestor(current, c.newID) {
		repo.refMu.Unlock()
		return "non-fast-forward"
	}
	if !current.IsZero() && strings.HasPrefix(c.name, "refs/tags/") && current != c.newID {
		repo.refMu.Unlock()
		return "already exists"
	}
	repo.refs[c.name] = c.newID
	// The first branch pushed to an empty repository becomes its default when the configured default
	// does not exist.
	if strings.HasPrefix(c.name, "refs/heads/") {
		if _, ok := repo.refs[repo.headRef()]; !ok {
			repo.DefaultBranch = strings.TrimPrefix(c.name, "refs/heads/")
		}
	}
	repo.refMu.Unlock()
	repo.gen.Add(1)
	return ""
}
