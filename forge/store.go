package main

// The forge's whole state, in memory: users, repositories with their git objects and refs, labels,
// issues, pull requests and comments. One App is one forge.

import (
	"encoding/base64"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// defaultLogin is the account the trial's token names when MAKEFUN_FORGE_LOGIN does not say.
const defaultLogin = "adaiversen"

type User struct {
	ID    int64
	Login string
	Name  string
	Email string
}

type Label struct {
	ID          int64
	Name        string
	Color       string
	Description string
	Default     bool
}

type Comment struct {
	ID        int64
	Body      string
	User      *User
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Pull is the pull-request half of an issue: every pull request is an issue with one of these.
type Pull struct {
	ID       int64
	HeadRef  string // branch name, without refs/heads/
	BaseRef  string
	HeadSHA  SHA // the head branch's tip, frozen once the pull request is merged or closed
	BaseSHA  SHA
	Draft    bool
	Merged   bool
	MergedAt time.Time
	MergedBy *User
	MergeSHA SHA
}

type Issue struct {
	ID          int64
	Number      int
	Title       string
	Body        string
	HasBody     bool
	State       string // "open" or "closed"
	StateReason string
	Labels      []*Label
	Assignees   []*User
	Comments    []*Comment
	User        *User
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ClosedAt    time.Time
	Locked      bool
	Pull        *Pull

	// cached is the issue's whole GET reply, rendered once and dropped at any change to the issue.
	cached atomic.Pointer[issueReply]
}

// issueReply is the stored answer to GET /repos/{owner}/{repo}/issues/{number}: the reply exactly
// as a keep-alive connection takes it. The event loop writes wire as it stands (fastpath.go); the
// ordinary path reuses its JSON.
type issueReply struct {
	origin string // the origin the reply's URLs name; it only suits a request that dialed that origin
	head   int    // wire[:head] is the status line and headers, wire[head:] the JSON
	wire   []byte
}

// json is the reply's body.
func (r *issueReply) json() []byte { return r.wire[r.head:] }

type Repo struct {
	ID            int64
	Owner         *User
	Name          string
	Description   string
	HasDesc       bool
	Homepage      string
	Private       bool
	DefaultBranch string
	HasIssues     bool
	HasWiki       bool
	HasProjects   bool
	Archived      bool
	Topics        []string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	PushedAt      time.Time

	Objects *ObjectStore

	refMu sync.RWMutex
	refs  map[string]SHA // full names: refs/heads/main, refs/tags/v1
	// gen counts ref changes; anything derived from the refs is cached under it.
	gen atomic.Uint64

	Labels     []*Label
	Issues     []*Issue // issues and pull requests share one number sequence
	nextNumber int
	// issueTemplate is the constant text of a new issue's JSON (issuetemplate.go). Guarded by
	// the forge's lock, held for writing.
	issueTemplate *issueTemplate

	gitCache sync.Map // request fingerprint -> *gitCacheEntry
}

func (r *Repo) FullName() string { return r.Owner.Login + "/" + r.Name }

// Ref returns a ref's target (zero when it does not exist).
func (r *Repo) Ref(name string) SHA {
	r.refMu.RLock()
	sha := r.refs[name]
	r.refMu.RUnlock()
	return sha
}

// Branch returns the tip of a branch by its short name.
func (r *Repo) Branch(name string) SHA { return r.Ref("refs/heads/" + name) }

// setRef moves (or, given the zero id, deletes) a ref.
func (r *Repo) setRef(name string, sha SHA) {
	r.refMu.Lock()
	if sha.IsZero() {
		delete(r.refs, name)
	} else {
		r.refs[name] = sha
	}
	r.refMu.Unlock()
	r.gen.Add(1)
}

type refEntry struct {
	Name string
	SHA  SHA
}

// sortedRefs lists every ref in name order.
func (r *Repo) sortedRefs() []refEntry {
	r.refMu.RLock()
	out := make([]refEntry, 0, len(r.refs))
	for name, sha := range r.refs {
		out = append(out, refEntry{name, sha})
	}
	r.refMu.RUnlock()
	sortRefs(out)
	return out
}

func sortRefs(refs []refEntry) {
	for i := 1; i < len(refs); i++ {
		for j := i; j > 0 && refs[j].Name < refs[j-1].Name; j-- {
			refs[j], refs[j-1] = refs[j-1], refs[j]
		}
	}
}

// headRef names the ref HEAD points at.
func (r *Repo) headRef() string { return "refs/heads/" + r.DefaultBranch }

// resolveRev turns a branch name, tag name, full ref name or full object id into a commit id.
func (r *Repo) resolveRev(rev string) (SHA, bool) {
	if rev == "" {
		rev = r.DefaultBranch
	}
	if rev == "HEAD" {
		rev = r.DefaultBranch
	}
	candidates := []string{"refs/heads/" + rev, "refs/tags/" + rev, rev}
	if strings.HasPrefix(rev, "heads/") || strings.HasPrefix(rev, "tags/") {
		candidates = append([]string{"refs/" + rev}, candidates...)
	}
	r.refMu.RLock()
	for _, name := range candidates {
		if sha, ok := r.refs[name]; ok {
			r.refMu.RUnlock()
			return r.peel(sha), true
		}
	}
	r.refMu.RUnlock()
	if sha, ok := parseSHA(rev); ok && r.Objects.Has(sha) {
		return r.peel(sha), true
	}
	return zeroSHA, false
}

// peel follows annotated tags to the object they name.
func (r *Repo) peel(sha SHA) SHA {
	for i := 0; i < 8; i++ {
		o := r.Objects.Get(sha)
		if o == nil || o.Type != ObjTag {
			return sha
		}
		target, ok := tagTarget(o.Data)
		if !ok {
			return sha
		}
		sha = target
	}
	return sha
}

// tagTarget reads the "object <sha>" line of an annotated tag.
func tagTarget(data []byte) (SHA, bool) {
	text := string(data)
	if !strings.HasPrefix(text, "object ") || len(text) < 47 {
		return zeroSHA, false
	}
	return parseSHA(text[7:47])
}

// App is the forge.
type App struct {
	token string
	// bearer is the Authorization value the suite sends, "Bearer <token>", for the fast paths to
	// compare against as bytes. It is nil when no token is set, and the fast paths then stand aside.
	bearer []byte
	base   time.Time // the instance's opening time: every timestamp is this plus a tick count

	mu        sync.RWMutex
	viewer    *User
	users     map[string]*User // by lower-case login
	repos     map[string]*Repo // by lower-case "owner/name"
	repoByID  map[int64]*Repo
	repoOrder []*Repo

	nextUserID    int64
	nextRepoID    int64
	nextIssueID   int64
	nextPullID    int64
	nextCommentID int64
	nextLabelID   int64
	ticks         int64

	// fastView is the view the create fast path last rendered with, kept so a run of requests to
	// one origin shares it. Guarded by mu.
	fastView *view

	requests atomic.Uint64
	schema   *gqlSchema
	mcpTools []byte // the tools/list result, rendered once
}

func newApp(token, trialNow string) *App {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if trialNow != "" {
		if t, err := time.Parse(time.RFC3339, trialNow); err == nil {
			base = t.UTC().Truncate(time.Second)
		} else if ms, err := strconv.ParseInt(trialNow, 10, 64); err == nil {
			base = time.UnixMilli(ms).UTC().Truncate(time.Second)
		}
	}
	a := &App{
		token:         token,
		base:          base,
		users:         map[string]*User{},
		repos:         map[string]*Repo{},
		repoByID:      map[int64]*Repo{},
		nextUserID:    1000,
		nextRepoID:    1,
		nextIssueID:   1,
		nextPullID:    1,
		nextCommentID: 1,
		nextLabelID:   1,
	}
	if token != "" {
		a.bearer = []byte("Bearer " + token)
	}
	login := envOr("MAKEFUN_FORGE_LOGIN", defaultLogin)
	a.viewer = a.userLocked(login)
	a.schema = buildGraphQLSchema()
	a.mcpTools = renderMCPTools()
	return a
}

// now returns the next timestamp: the forge's clock moves one second per stamped event, so what it
// writes depends only on the order of the requests it served.
func (a *App) now() time.Time {
	a.ticks++
	return a.base.Add(time.Duration(a.ticks) * time.Second)
}

// userLocked finds or makes the user with this login. The caller holds a.mu (or is the constructor).
func (a *App) userLocked(login string) *User {
	key := strings.ToLower(login)
	if u, ok := a.users[key]; ok {
		return u
	}
	u := &User{ID: a.nextUserID, Login: login}
	a.nextUserID++
	a.users[key] = u
	return u
}

func repoKey(owner, name string) string {
	return strings.ToLower(owner) + "/" + strings.ToLower(name)
}

// repo looks a repository up by owner and name.
func (a *App) repo(owner, name string) *Repo {
	a.mu.RLock()
	r := a.repos[repoKey(owner, name)]
	a.mu.RUnlock()
	return r
}

// createRepoLocked makes an empty repository. The caller holds a.mu for writing.
func (a *App) createRepoLocked(owner *User, name string) *Repo {
	now := a.now()
	r := &Repo{
		ID:            a.nextRepoID,
		Owner:         owner,
		Name:          name,
		DefaultBranch: "main",
		HasIssues:     true,
		HasWiki:       true,
		HasProjects:   true,
		CreatedAt:     now,
		UpdatedAt:     now,
		PushedAt:      now,
		Objects:       newObjectStore(),
		refs:          map[string]SHA{},
		nextNumber:    1,
	}
	a.nextRepoID++
	a.repos[repoKey(owner.Login, name)] = r
	a.repoByID[r.ID] = r
	a.repoOrder = append(a.repoOrder, r)
	return r
}

// deleteRepoLocked removes a repository. The caller holds a.mu for writing.
func (a *App) deleteRepoLocked(r *Repo) {
	delete(a.repos, repoKey(r.Owner.Login, r.Name))
	delete(a.repoByID, r.ID)
	for i, other := range a.repoOrder {
		if other == r {
			a.repoOrder = append(a.repoOrder[:i], a.repoOrder[i+1:]...)
			break
		}
	}
}

func (r *Repo) label(name string) *Label {
	for _, l := range r.Labels {
		if strings.EqualFold(l.Name, name) {
			return l
		}
	}
	return nil
}

// issue finds an issue or pull request by number.
func (r *Repo) issue(number int) *Issue {
	if number < 1 || number > len(r.Issues) {
		return nil
	}
	return r.Issues[number-1]
}

// nodeID is the opaque global id GraphQL and REST share for an object: base64 of "<Type>:<id>".
func nodeID(kind string, id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(kind + ":" + strconv.FormatInt(id, 10)))
}

// parseNodeID is nodeID's inverse.
func parseNodeID(node string) (kind string, id int64, ok bool) {
	raw, err := base64.RawURLEncoding.DecodeString(node)
	if err != nil {
		return "", 0, false
	}
	colon := strings.IndexByte(string(raw), ':')
	if colon < 0 {
		return "", 0, false
	}
	id, err = strconv.ParseInt(string(raw[colon+1:]), 10, 64)
	if err != nil {
		return "", 0, false
	}
	return string(raw[:colon]), id, true
}

func formatTime(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05Z") }
