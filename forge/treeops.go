package main

// Work on trees and histories that the API does on a repository's behalf: committing file changes,
// comparing two trees, three-way merging for pull requests, and line diffs for their files.

import (
	"bytes"
	"errors"
	"sort"
	"strconv"
	"strings"
)

// fileChange is one file written or removed by a commit.
type fileChange struct {
	Path   string
	Blob   SHA
	Mode   string
	Delete bool
}

// filesOf lists the files of a commit's tree, by path.
func (repo *Repo) filesOf(commit SHA) map[string]flatFile {
	out := map[string]flatFile{}
	c := repo.Objects.Get(commit).AsCommit()
	if c == nil {
		return out
	}
	var files []flatFile
	repo.Objects.flattenTree(c.Tree, "", &files)
	for _, f := range files {
		out[f.Path] = f
	}
	return out
}

// treeWith applies changes to a set of files and stores the resulting tree.
func (repo *Repo) treeWith(files map[string]flatFile, changes []fileChange) SHA {
	for _, ch := range changes {
		if ch.Delete {
			delete(files, ch.Path)
			continue
		}
		mode := ch.Mode
		if mode == "" {
			if existing, ok := files[ch.Path]; ok {
				mode = existing.Mode
			} else {
				mode = "100644"
			}
		}
		files[ch.Path] = flatFile{Path: ch.Path, Mode: mode, ID: ch.Blob}
	}
	list := make([]flatFile, 0, len(files))
	for _, f := range files {
		list = append(list, f)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Path < list[j].Path })
	return repo.Objects.writeTreeFromFiles(list)
}

// commit stores a commit object and returns its id.
func (repo *Repo) commit(tree SHA, parents []SHA, author, committer Signature, message string) SHA {
	return repo.Objects.Put(ObjCommit, buildCommit(tree, parents, author, committer, message)).ID
}

// changedFile is one path that differs between two trees.
type changedFile struct {
	Path      string
	Status    string // added, removed, modified
	Old       SHA
	New       SHA
	OldMode   string
	NewMode   string
	Additions int
	Deletions int
	Patch     string
	Binary    bool
}

// diffTrees lists the paths that differ between two commits' trees (either may be zero: no tree).
func (repo *Repo) diffTrees(from, to SHA, withPatch bool) []changedFile {
	a := repo.filesOf(from)
	b := repo.filesOf(to)
	paths := make([]string, 0, len(a)+len(b))
	for p := range a {
		paths = append(paths, p)
	}
	for p := range b {
		if _, ok := a[p]; !ok {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	var out []changedFile
	for _, p := range paths {
		oldFile, hadOld := a[p]
		newFile, hasNew := b[p]
		if hadOld && hasNew && oldFile.ID == newFile.ID && oldFile.Mode == newFile.Mode {
			continue
		}
		cf := changedFile{Path: p}
		var oldData, newData []byte
		switch {
		case !hadOld:
			cf.Status = "added"
			cf.New, cf.NewMode = newFile.ID, newFile.Mode
		case !hasNew:
			cf.Status = "removed"
			cf.Old, cf.OldMode = oldFile.ID, oldFile.Mode
		default:
			cf.Status = "modified"
			cf.Old, cf.OldMode, cf.New, cf.NewMode = oldFile.ID, oldFile.Mode, newFile.ID, newFile.Mode
		}
		if hadOld {
			if o := repo.Objects.Get(oldFile.ID); o != nil {
				oldData = o.Data
			}
		}
		if hasNew {
			if o := repo.Objects.Get(newFile.ID); o != nil {
				newData = o.Data
			}
		}
		if isBinary(oldData) || isBinary(newData) {
			cf.Binary = true
		} else {
			cf.Additions, cf.Deletions, cf.Patch = lineDiff(oldData, newData, withPatch)
		}
		out = append(out, cf)
	}
	return out
}

func isBinary(data []byte) bool {
	if len(data) > 8000 {
		data = data[:8000]
	}
	return bytes.IndexByte(data, 0) >= 0
}

// splitLines cuts text into lines, each keeping its newline; the last may lack one.
func splitLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	var lines []string
	for len(data) > 0 {
		nl := bytes.IndexByte(data, '\n')
		if nl < 0 {
			lines = append(lines, string(data))
			break
		}
		lines = append(lines, string(data[:nl+1]))
		data = data[nl+1:]
	}
	return lines
}

type diffOp struct {
	kind byte // ' ', '+', '-'
	text string
}

// diffLines computes a shortest edit script between two line lists (Myers' O(ND) algorithm).
func diffLines(a, b []string) []diffOp {
	n, m := len(a), len(b)
	// Trim the common prefix and suffix first: most edits touch a small part of a file.
	prefix := 0
	for prefix < n && prefix < m && a[prefix] == b[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < n-prefix && suffix < m-prefix && a[n-1-suffix] == b[m-1-suffix] {
		suffix++
	}
	ops := make([]diffOp, 0, n+m)
	for i := 0; i < prefix; i++ {
		ops = append(ops, diffOp{' ', a[i]})
	}
	ops = append(ops, myers(a[prefix:n-suffix], b[prefix:m-suffix])...)
	for i := n - suffix; i < n; i++ {
		ops = append(ops, diffOp{' ', a[i]})
	}
	return ops
}

func myers(a, b []string) []diffOp {
	n, m := len(a), len(b)
	if n == 0 {
		ops := make([]diffOp, m)
		for i, line := range b {
			ops[i] = diffOp{'+', line}
		}
		return ops
	}
	if m == 0 {
		ops := make([]diffOp, n)
		for i, line := range a {
			ops[i] = diffOp{'-', line}
		}
		return ops
	}
	max := n + m
	offset := max
	v := make([]int, 2*max+2)
	var trace [][]int
	found := false
	for d := 0; d <= max && !found; d++ {
		snapshot := make([]int, len(v))
		copy(snapshot, v)
		trace = append(trace, snapshot)
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[offset+k-1] < v[offset+k+1]) {
				x = v[offset+k+1]
			} else {
				x = v[offset+k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			v[offset+k] = x
			if x >= n && y >= m {
				found = true
				break
			}
		}
	}
	// Walk the trace backwards to recover the script.
	var reversed []diffOp
	x, y := n, m
	for d := len(trace) - 1; d >= 0; d-- {
		vd := trace[d]
		k := x - y
		var prevK int
		if k == -d || (k != d && vd[offset+k-1] < vd[offset+k+1]) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}
		prevX := vd[offset+prevK]
		prevY := prevX - prevK
		for x > prevX && y > prevY {
			x--
			y--
			reversed = append(reversed, diffOp{' ', a[x]})
		}
		if d > 0 {
			if x == prevX {
				y--
				reversed = append(reversed, diffOp{'+', b[y]})
			} else {
				x--
				reversed = append(reversed, diffOp{'-', a[x]})
			}
		}
	}
	ops := make([]diffOp, len(reversed))
	for i, op := range reversed {
		ops[len(reversed)-1-i] = op
	}
	return ops
}

// lineDiff counts added and removed lines between two texts and, when asked, renders the unified
// diff hunks the pull-request files API calls a patch.
func lineDiff(oldData, newData []byte, withPatch bool) (additions, deletions int, patch string) {
	ops := diffLines(splitLines(oldData), splitLines(newData))
	for _, op := range ops {
		switch op.kind {
		case '+':
			additions++
		case '-':
			deletions++
		}
	}
	if !withPatch || additions+deletions == 0 {
		return additions, deletions, ""
	}
	const context = 3
	var b strings.Builder
	i := 0
	oldLine, newLine := 1, 1
	for i < len(ops) {
		// Find the next change.
		j := i
		for j < len(ops) && ops[j].kind == ' ' {
			j++
		}
		if j == len(ops) {
			break
		}
		start := j - context
		if start < i {
			start = i
		}
		for k := i; k < start; k++ {
			oldLine++
			newLine++
		}
		// Extend the hunk while changes keep arriving within 2*context lines.
		end := j
		last := j
		for end < len(ops) {
			if ops[end].kind != ' ' {
				last = end
			} else if end-last > 2*context {
				break
			}
			end++
		}
		stop := last + context + 1
		if stop > len(ops) {
			stop = len(ops)
		}
		oldCount, newCount := 0, 0
		for k := start; k < stop; k++ {
			switch ops[k].kind {
			case ' ':
				oldCount++
				newCount++
			case '-':
				oldCount++
			case '+':
				newCount++
			}
		}
		oldStart, newStart := oldLine, newLine
		if oldCount == 0 {
			oldStart--
		}
		if newCount == 0 {
			newStart--
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("@@ -" + strconv.Itoa(oldStart) + "," + strconv.Itoa(oldCount) + " +" + strconv.Itoa(newStart) + "," + strconv.Itoa(newCount) + " @@")
		for k := start; k < stop; k++ {
			text := ops[k].text
			b.WriteByte('\n')
			b.WriteByte(ops[k].kind)
			if strings.HasSuffix(text, "\n") {
				b.WriteString(text[:len(text)-1])
			} else {
				b.WriteString(text)
				b.WriteString("\n\\ No newline at end of file")
			}
			switch ops[k].kind {
			case ' ':
				oldLine++
				newLine++
			case '-':
				oldLine++
			case '+':
				newLine++
			}
		}
		i = stop
	}
	return additions, deletions, b.String()
}

var errMergeConflict = errors.New("merge conflict")

// mergeTrees three-way merges head into base at file granularity: a path both sides changed in
// different ways is a conflict. It returns the merged tree.
func (repo *Repo) mergeTrees(baseTip, headTip SHA) (SHA, error) {
	ancestor := repo.Objects.mergeBase(baseTip, headTip)
	origin := repo.filesOf(ancestor)
	ours := repo.filesOf(baseTip)
	theirs := repo.filesOf(headTip)
	merged := make(map[string]flatFile, len(ours))
	for p, f := range ours {
		merged[p] = f
	}
	same := func(a flatFile, aok bool, b flatFile, bok bool) bool {
		if aok != bok {
			return false
		}
		return !aok || (a.ID == b.ID && a.Mode == b.Mode)
	}
	paths := map[string]struct{}{}
	for p := range origin {
		paths[p] = struct{}{}
	}
	for p := range theirs {
		paths[p] = struct{}{}
	}
	for p := range paths {
		o, ook := origin[p]
		u, uok := ours[p]
		t, tok := theirs[p]
		if same(o, ook, t, tok) {
			continue // head did not touch this path
		}
		if same(u, uok, t, tok) {
			continue // both sides ended at the same content
		}
		if !same(o, ook, u, uok) {
			return zeroSHA, errMergeConflict
		}
		if tok {
			merged[p] = t
		} else {
			delete(merged, p)
		}
	}
	list := make([]flatFile, 0, len(merged))
	for _, f := range merged {
		list = append(list, f)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Path < list[j].Path })
	return repo.Objects.writeTreeFromFiles(list), nil
}

// commitsBetween lists the commits reachable from head and not from base, oldest first.
func (repo *Repo) commitsBetween(base, head SHA) []*Commit {
	exclude := repo.Objects.ancestors(base)
	var out []*Commit
	for _, c := range repo.Objects.commitLog(head, 0) {
		if _, skip := exclude[c.ID]; !skip {
			out = append(out, c)
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}
