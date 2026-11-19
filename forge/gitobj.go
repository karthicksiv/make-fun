package main

// Git's object model, held in memory: blobs, trees, commits and tags addressed by SHA-1, plus the
// helpers that build and read them. Nothing here touches the network or a disk.

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// SHA is a git object id.
type SHA [20]byte

var zeroSHA SHA

const zeroHex = "0000000000000000000000000000000000000000"

func (s SHA) String() string { return hex.EncodeToString(s[:]) }

func (s SHA) IsZero() bool { return s == zeroSHA }

// parseSHA reads 40 hex digits.
func parseSHA(text string) (SHA, bool) {
	var out SHA
	if len(text) != 40 {
		return out, false
	}
	if _, err := hex.Decode(out[:], []byte(text)); err != nil {
		return out, false
	}
	return out, true
}

// ObjType is a git object type, numbered as the pack format numbers them.
type ObjType uint8

const (
	ObjCommit ObjType = 1
	ObjTree   ObjType = 2
	ObjBlob   ObjType = 3
	ObjTag    ObjType = 4
)

func (t ObjType) String() string {
	switch t {
	case ObjCommit:
		return "commit"
	case ObjTree:
		return "tree"
	case ObjBlob:
		return "blob"
	case ObjTag:
		return "tag"
	}
	return "unknown"
}

// Object is one stored git object. Data is the object's content without the "<type> <len>\0" header.
type Object struct {
	Type ObjType
	ID   SHA
	Data []byte

	packMu sync.Mutex
	// packed is the object as a pack entry (type/size header followed by its zlib stream), built
	// once and reused for every pack this object is sent in.
	packed []byte

	commitOnce sync.Once
	commit     *Commit
	treeOnce   sync.Once
	tree       []TreeEntry
}

// hashObject computes the id git gives an object of this type and content.
func hashObject(t ObjType, data []byte) SHA {
	h := sha1.New()
	var head [32]byte
	b := append(head[:0], t.String()...)
	b = append(b, ' ')
	b = strconv.AppendInt(b, int64(len(data)), 10)
	b = append(b, 0)
	h.Write(b)
	h.Write(data)
	var out SHA
	h.Sum(out[:0])
	return out
}

// ObjectStore is one repository's objects.
type ObjectStore struct {
	mu      sync.RWMutex
	objects map[SHA]*Object
}

func newObjectStore() *ObjectStore {
	return &ObjectStore{objects: make(map[SHA]*Object, 64)}
}

func (s *ObjectStore) Get(id SHA) *Object {
	s.mu.RLock()
	o := s.objects[id]
	s.mu.RUnlock()
	return o
}

func (s *ObjectStore) Has(id SHA) bool {
	s.mu.RLock()
	_, ok := s.objects[id]
	s.mu.RUnlock()
	return ok
}

// Put stores an object (hashing it) and returns the stored instance.
func (s *ObjectStore) Put(t ObjType, data []byte) *Object {
	id := hashObject(t, data)
	s.mu.Lock()
	if existing, ok := s.objects[id]; ok {
		s.mu.Unlock()
		return existing
	}
	o := &Object{Type: t, ID: id, Data: data}
	s.objects[id] = o
	s.mu.Unlock()
	return o
}

// PutObject stores an already-hashed object, keeping any instance stored earlier under the same id.
func (s *ObjectStore) PutObject(o *Object) *Object {
	s.mu.Lock()
	if existing, ok := s.objects[o.ID]; ok {
		s.mu.Unlock()
		return existing
	}
	s.objects[o.ID] = o
	s.mu.Unlock()
	return o
}

func (s *ObjectStore) Len() int {
	s.mu.RLock()
	n := len(s.objects)
	s.mu.RUnlock()
	return n
}

// ---------------------------------------------------------------- commits

// Signature is an author or committer line.
type Signature struct {
	Name  string
	Email string
	When  int64  // seconds since the epoch
	Zone  string // "+0000"
}

func (s Signature) String() string {
	zone := s.Zone
	if zone == "" {
		zone = "+0000"
	}
	return s.Name + " <" + s.Email + "> " + strconv.FormatInt(s.When, 10) + " " + zone
}

func parseSignature(line string) Signature {
	var sig Signature
	lt := strings.LastIndexByte(line, '<')
	gt := strings.LastIndexByte(line, '>')
	if lt < 0 || gt < lt {
		sig.Name = strings.TrimSpace(line)
		return sig
	}
	sig.Name = strings.TrimSpace(line[:lt])
	sig.Email = line[lt+1 : gt]
	rest := strings.Fields(line[gt+1:])
	if len(rest) > 0 {
		sig.When, _ = strconv.ParseInt(rest[0], 10, 64)
	}
	if len(rest) > 1 {
		sig.Zone = rest[1]
	}
	return sig
}

// Commit is a parsed commit object.
type Commit struct {
	ID        SHA
	Tree      SHA
	Parents   []SHA
	Author    Signature
	Committer Signature
	Message   string
}

func parseCommit(id SHA, data []byte) (*Commit, error) {
	c := &Commit{ID: id}
	rest := data
	for len(rest) > 0 {
		nl := bytes.IndexByte(rest, '\n')
		var line []byte
		if nl < 0 {
			line, rest = rest, nil
		} else {
			line, rest = rest[:nl], rest[nl+1:]
		}
		if len(line) == 0 {
			c.Message = string(rest)
			break
		}
		if line[0] == ' ' {
			continue // a continuation line of a multi-line header (gpgsig)
		}
		sp := bytes.IndexByte(line, ' ')
		if sp < 0 {
			continue
		}
		key, value := string(line[:sp]), string(line[sp+1:])
		switch key {
		case "tree":
			t, ok := parseSHA(value)
			if !ok {
				return nil, errors.New("commit has a malformed tree line")
			}
			c.Tree = t
		case "parent":
			p, ok := parseSHA(value)
			if !ok {
				return nil, errors.New("commit has a malformed parent line")
			}
			c.Parents = append(c.Parents, p)
		case "author":
			c.Author = parseSignature(value)
		case "committer":
			c.Committer = parseSignature(value)
		}
	}
	if c.Tree.IsZero() {
		return nil, errors.New("commit names no tree")
	}
	return c, nil
}

// Commit returns the object parsed as a commit, or nil when it is not one.
func (o *Object) AsCommit() *Commit {
	if o == nil || o.Type != ObjCommit {
		return nil
	}
	o.commitOnce.Do(func() {
		c, err := parseCommit(o.ID, o.Data)
		if err == nil {
			o.commit = c
		}
	})
	return o.commit
}

// buildCommit serialises a commit object's content.
func buildCommit(tree SHA, parents []SHA, author, committer Signature, message string) []byte {
	var b bytes.Buffer
	b.Grow(256 + len(message))
	b.WriteString("tree ")
	b.WriteString(tree.String())
	b.WriteByte('\n')
	for _, p := range parents {
		b.WriteString("parent ")
		b.WriteString(p.String())
		b.WriteByte('\n')
	}
	b.WriteString("author ")
	b.WriteString(author.String())
	b.WriteByte('\n')
	b.WriteString("committer ")
	b.WriteString(committer.String())
	b.WriteString("\n\n")
	b.WriteString(message)
	return b.Bytes()
}

// ---------------------------------------------------------------- trees

// TreeEntry is one line of a tree object.
type TreeEntry struct {
	Mode string // "100644", "100755", "120000", "40000", "160000"
	Name string
	ID   SHA
}

func (e TreeEntry) IsTree() bool { return e.Mode == "40000" || e.Mode == "040000" }

// TypeName is the object type the entry points at, as the git trees API names it.
func (e TreeEntry) TypeName() string {
	if e.IsTree() {
		return "tree"
	}
	if e.Mode == "160000" {
		return "commit"
	}
	return "blob"
}

func parseTree(data []byte) ([]TreeEntry, error) {
	var entries []TreeEntry
	rest := data
	for len(rest) > 0 {
		sp := bytes.IndexByte(rest, ' ')
		if sp < 0 {
			return nil, errors.New("tree entry has no mode")
		}
		nul := bytes.IndexByte(rest[sp+1:], 0)
		if nul < 0 {
			return nil, errors.New("tree entry has no name")
		}
		nul += sp + 1
		if len(rest) < nul+21 {
			return nil, errors.New("tree entry is truncated")
		}
		var e TreeEntry
		e.Mode = string(rest[:sp])
		e.Name = string(rest[sp+1 : nul])
		copy(e.ID[:], rest[nul+1:nul+21])
		entries = append(entries, e)
		rest = rest[nul+21:]
	}
	return entries, nil
}

// AsTree returns the object's entries, or nil when it is not a tree.
func (o *Object) AsTree() []TreeEntry {
	if o == nil || o.Type != ObjTree {
		return nil
	}
	o.treeOnce.Do(func() {
		entries, err := parseTree(o.Data)
		if err == nil {
			o.tree = entries
		}
	})
	return o.tree
}

// treeSortKey orders entries as git does: by name, a directory comparing as if its name ended in "/".
func treeSortKey(e TreeEntry) string {
	if e.IsTree() {
		return e.Name + "/"
	}
	return e.Name
}

// buildTree serialises entries, sorted the way git requires.
func buildTree(entries []TreeEntry) []byte {
	sorted := make([]TreeEntry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool { return treeSortKey(sorted[i]) < treeSortKey(sorted[j]) })
	var b bytes.Buffer
	for _, e := range sorted {
		mode := e.Mode
		if mode == "040000" {
			mode = "40000"
		}
		b.WriteString(mode)
		b.WriteByte(' ')
		b.WriteString(e.Name)
		b.WriteByte(0)
		b.Write(e.ID[:])
	}
	return b.Bytes()
}

// ---------------------------------------------------------------- walking

// lookupPath resolves a slash-separated path inside a tree. An empty path is the tree itself.
func (s *ObjectStore) lookupPath(tree SHA, path string) (TreeEntry, bool) {
	path = strings.Trim(path, "/")
	if path == "" {
		return TreeEntry{Mode: "40000", ID: tree}, true
	}
	current := tree
	parts := strings.Split(path, "/")
	for i, part := range parts {
		entries := s.Get(current).AsTree()
		found := false
		for _, e := range entries {
			if e.Name == part {
				if i == len(parts)-1 {
					return e, true
				}
				if !e.IsTree() {
					return TreeEntry{}, false
				}
				current = e.ID
				found = true
				break
			}
		}
		if !found {
			return TreeEntry{}, false
		}
	}
	return TreeEntry{}, false
}

// flatFile is one blob of a flattened tree.
type flatFile struct {
	Path string
	Mode string
	ID   SHA
}

// flattenTree lists every blob under a tree, depth first, in path order.
func (s *ObjectStore) flattenTree(tree SHA, prefix string, out *[]flatFile) {
	for _, e := range s.Get(tree).AsTree() {
		if e.IsTree() {
			s.flattenTree(e.ID, prefix+e.Name+"/", out)
		} else {
			*out = append(*out, flatFile{Path: prefix + e.Name, Mode: e.Mode, ID: e.ID})
		}
	}
}

// writeTreeFromFiles builds (and stores) the nested trees for a flat list of files; it returns the
// root tree's id. An empty list gives git's empty tree.
func (s *ObjectStore) writeTreeFromFiles(files []flatFile) SHA {
	type dir struct {
		files   []TreeEntry
		subdirs map[string][]flatFile
		order   []string
	}
	root := dir{subdirs: map[string][]flatFile{}}
	for _, f := range files {
		slash := strings.IndexByte(f.Path, '/')
		if slash < 0 {
			root.files = append(root.files, TreeEntry{Mode: f.Mode, Name: f.Path, ID: f.ID})
			continue
		}
		name := f.Path[:slash]
		if _, ok := root.subdirs[name]; !ok {
			root.order = append(root.order, name)
		}
		root.subdirs[name] = append(root.subdirs[name], flatFile{Path: f.Path[slash+1:], Mode: f.Mode, ID: f.ID})
	}
	entries := root.files
	for _, name := range root.order {
		sub := s.writeTreeFromFiles(root.subdirs[name])
		entries = append(entries, TreeEntry{Mode: "40000", Name: name, ID: sub})
	}
	return s.Put(ObjTree, buildTree(entries)).ID
}

// isAncestor reports whether `ancestor` is reachable from `tip` through parent links.
func (s *ObjectStore) isAncestor(ancestor, tip SHA) bool {
	if ancestor == tip {
		return true
	}
	seen := map[SHA]struct{}{tip: {}}
	queue := []SHA{tip}
	for len(queue) > 0 {
		id := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		c := s.Get(id).AsCommit()
		if c == nil {
			continue
		}
		for _, p := range c.Parents {
			if p == ancestor {
				return true
			}
			if _, ok := seen[p]; !ok {
				seen[p] = struct{}{}
				queue = append(queue, p)
			}
		}
	}
	return false
}

// ancestors collects every commit reachable from tip (tip included).
func (s *ObjectStore) ancestors(tip SHA) map[SHA]struct{} {
	seen := map[SHA]struct{}{}
	if tip.IsZero() {
		return seen
	}
	queue := []SHA{tip}
	seen[tip] = struct{}{}
	for len(queue) > 0 {
		id := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		c := s.Get(id).AsCommit()
		if c == nil {
			continue
		}
		for _, p := range c.Parents {
			if _, ok := seen[p]; !ok {
				seen[p] = struct{}{}
				queue = append(queue, p)
			}
		}
	}
	return seen
}

// mergeBase finds a best common ancestor of two commits (zero when they share none).
func (s *ObjectStore) mergeBase(a, b SHA) SHA {
	if a.IsZero() || b.IsZero() {
		return zeroSHA
	}
	fromA := s.ancestors(a)
	// Breadth-first from b: the first commit also reachable from a is a nearest common ancestor.
	seen := map[SHA]struct{}{b: {}}
	queue := []SHA{b}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if _, ok := fromA[id]; ok {
			return id
		}
		c := s.Get(id).AsCommit()
		if c == nil {
			continue
		}
		for _, p := range c.Parents {
			if _, ok := seen[p]; !ok {
				seen[p] = struct{}{}
				queue = append(queue, p)
			}
		}
	}
	return zeroSHA
}

// commitLog lists commits reachable from tip, newest first by first-parent-biased topological walk.
func (s *ObjectStore) commitLog(tip SHA, limit int) []*Commit {
	var out []*Commit
	if tip.IsZero() {
		return out
	}
	seen := map[SHA]struct{}{tip: {}}
	queue := []SHA{tip}
	for len(queue) > 0 && (limit <= 0 || len(out) < limit) {
		// Take the queued commit with the newest committer time; ties keep queue order.
		best := 0
		var bestCommit *Commit
		for i, id := range queue {
			c := s.Get(id).AsCommit()
			if c == nil {
				continue
			}
			if bestCommit == nil || c.Committer.When > bestCommit.Committer.When {
				best, bestCommit = i, c
			}
		}
		if bestCommit == nil {
			break
		}
		queue = append(queue[:best], queue[best+1:]...)
		out = append(out, bestCommit)
		for _, p := range bestCommit.Parents {
			if _, ok := seen[p]; !ok {
				seen[p] = struct{}{}
				queue = append(queue, p)
			}
		}
	}
	return out
}
