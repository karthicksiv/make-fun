package main

// A new issue's JSON, written from a template rather than through the generic renderer.
//
// The template is not a second description of what an issue looks like. It is cut from view.issue,
// the one renderer there is: a probe issue with marker values is rendered through it, and the text
// is split at the markers. What lies between them is the same for every new issue of a repository
// seen from one origin (its URLs, its author, the fields a new issue has not got yet). Before a
// template is used, a second probe with other values is rendered both ways and the two results must
// be the same bytes; if they are not, the repository goes without and view.issue renders as before.

import (
	"bytes"
	"encoding/base64"
	"strconv"
	"time"
)

// issueTemplate is the constant text of a new issue's JSON, with the slots between its parts.
type issueTemplate struct {
	// What the constant text depends on; a template is cut again when any of it changes.
	base  string
	owner *User
	name  string
	user  *User

	parts [][]byte // nil when the cut did not pass its check
	slots []byte   // slots[i] is what goes between parts[i] and parts[i+1]
	size  int      // the JSON's length but for its title and body, generously
}

// The slots of a template: the values that differ from one new issue to the next.
const (
	slotNumber = 'n'
	slotID     = 'i'
	slotNodeID = 'd'
	slotTitle  = 't'
	slotBody   = 'b'
	slotTime   = 'c' // created_at and updated_at, which a new issue has equal
)

const timeLayout = "2006-01-02T15:04:05Z"

// untouched reports whether an issue is still exactly as createIssue leaves one made from a title
// and a body: the only issues a template may render.
func (i *Issue) untouched() bool {
	return i.Pull == nil && len(i.Labels) == 0 && len(i.Assignees) == 0 && len(i.Comments) == 0 &&
		i.State == "open" && i.StateReason == "" && !i.Locked && i.ClosedAt.IsZero() && i.UpdatedAt.Equal(i.CreatedAt)
}

// newIssueTemplate returns the template for new issues of repo by user as this view shows them,
// cutting it on first use and again whenever the repository's name, owner or origin has changed.
// It returns nil when no template passed its check. The caller holds the forge's lock for writing.
func (v *view) newIssueTemplate(repo *Repo, user *User) *issueTemplate {
	t := repo.issueTemplate
	if t == nil || t.base != v.base || t.owner != repo.Owner || t.name != repo.Name || t.user != user {
		t = v.cutIssueTemplate(repo, user)
		repo.issueTemplate = t
	}
	if t.parts == nil {
		return nil
	}
	return t
}

// cutIssueTemplate renders a probe issue through view.issue and splits the text at its markers.
func (v *view) cutIssueTemplate(repo *Repo, user *User) *issueTemplate {
	t := &issueTemplate{base: v.base, owner: repo.Owner, name: repo.Name, user: user}
	probe := func(id int64, number int, title, body string, at time.Time) *Issue {
		return &Issue{ID: id, Number: number, Title: title, Body: body, HasBody: true, State: "open", User: user, CreatedAt: at, UpdatedAt: at}
	}
	const (
		markID     = 8111111111111111118
		markNumber = 922222229
		markTitle  = "\x02title\x03"
		markBody   = "\x02body\x03"
	)
	markTime := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
	markers := []struct {
		text []byte
		slot byte
	}{
		{appendJSONString(nil, markTitle), slotTitle},
		{appendJSONString(nil, markBody), slotBody},
		{appendNodeID(nil, "Issue", markID), slotNodeID},
		{strconv.AppendInt(nil, markID, 10), slotID},
		{strconv.AppendInt(nil, markNumber, 10), slotNumber},
		{markTime.AppendFormat(nil, timeLayout), slotTime},
	}
	text := appendJSON(nil, v.issue(repo, probe(markID, markNumber, markTitle, markBody, markTime)))
	var parts [][]byte
	var slots []byte
	for {
		at, which := -1, 0
		for m := range markers {
			if i := bytes.Index(text, markers[m].text); i >= 0 && (at < 0 || i < at) {
				at, which = i, m
			}
		}
		if at < 0 {
			parts = append(parts, text)
			break
		}
		parts = append(parts, text[:at])
		slots = append(slots, markers[which].slot)
		text = text[at+len(markers[which].text):]
	}
	t.parts, t.slots = parts, slots
	for _, part := range parts {
		t.size += len(part)
	}
	t.size += 32 * len(slots) // room for every number, id and time a slot writes
	// The check: an issue with quite different values must come out the same from both renderers.
	other := probe(27182818, 31415, "t \"q\" \\ / \n\t\x01 é ✓ <&> 922222229", "b\r\n\x1f 日本 \x02title\x03", time.Date(2031, 12, 30, 23, 59, 58, 0, time.UTC))
	if !bytes.Equal(t.append(nil, other), appendJSON(nil, v.issue(repo, other))) {
		t.parts, t.slots = nil, nil
	}
	return t
}

// append writes a new issue's JSON.
func (t *issueTemplate) append(dst []byte, issue *Issue) []byte {
	for i, part := range t.parts {
		dst = append(dst, part...)
		if i == len(t.slots) {
			break
		}
		switch t.slots[i] {
		case slotNumber:
			dst = strconv.AppendInt(dst, int64(issue.Number), 10)
		case slotID:
			dst = strconv.AppendInt(dst, issue.ID, 10)
		case slotNodeID:
			dst = appendNodeID(dst, "Issue", issue.ID)
		case slotTitle:
			dst = appendJSONString(dst, issue.Title)
		case slotBody:
			dst = appendJSONString(dst, issue.Body)
		case slotTime:
			dst = issue.CreatedAt.UTC().AppendFormat(dst, timeLayout)
		}
	}
	return dst
}

// appendNodeID writes what nodeID returns, without building the string.
func appendNodeID(dst []byte, kind string, id int64) []byte {
	var raw [48]byte
	plain := append(raw[:0], kind...)
	plain = append(plain, ':')
	plain = strconv.AppendInt(plain, id, 10)
	return base64.RawURLEncoding.AppendEncode(dst, plain)
}
