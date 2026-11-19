package main

// The capability strings the forge advertises are constants: nothing derives them when a trial
// starts, because a trial's pod has no git to ask. These tests hold them in place instead. The first
// pins every string to the bytes that passed the suite and names the capabilities the rules require;
// the second asks a real git, where the machine running the tests has one, for what it advertises,
// and requires that the forge offers nothing git itself does not know.

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// capabilitySet splits a v0 capability list into names and values ("agent=x" is agent -> x).
func capabilitySet(list string) map[string]string {
	set := map[string]string{}
	for _, item := range strings.Fields(list) {
		name, value, _ := strings.Cut(item, "=")
		set[name] = value
	}
	return set
}

// packetLines returns the data packets of a pkt-line stream, as text without the trailing newline.
func packetLines(t *testing.T, stream []byte) []string {
	t.Helper()
	var lines []string
	for len(stream) > 0 {
		p, rest, err := readPacket(stream)
		if err != nil {
			t.Fatalf("reading packets: %v", err)
		}
		if p.kind == pktData {
			lines = append(lines, strings.TrimSuffix(string(p.payload), "\n"))
		}
		stream = rest
	}
	return lines
}

// v2Capabilities reads a protocol v2 advertisement into capability -> value.
func v2Capabilities(t *testing.T, stream []byte) (first string, set map[string]string) {
	t.Helper()
	lines := packetLines(t, stream)
	if len(lines) == 0 {
		t.Fatalf("an empty v2 advertisement")
	}
	set = map[string]string{}
	for _, line := range lines[1:] {
		name, value, _ := strings.Cut(line, "=")
		set[name] = value
	}
	return lines[0], set
}

func TestCapabilityStringsArePinned(t *testing.T) {
	const (
		wantUpload  = "multi_ack multi_ack_detailed thin-pack side-band side-band-64k ofs-delta shallow deepen-since deepen-not deepen-relative no-progress include-tag allow-tip-sha1-in-want allow-reachable-sha1-in-want filter object-format=sha1 agent=forge/1.0"
		wantReceive = "report-status report-status-v2 delete-refs side-band-64k ofs-delta quiet atomic object-format=sha1 agent=forge/1.0"
	)
	if uploadPackCaps != wantUpload {
		t.Errorf("the upload-pack capabilities changed:\n got %q\nwant %q", uploadPackCaps, wantUpload)
	}
	if receivePackCaps != wantReceive {
		t.Errorf("the receive-pack capabilities changed:\n got %q\nwant %q", receivePackCaps, wantReceive)
	}
	wantV2 := pktLine(nil, "version 2\n")
	wantV2 = pktLine(wantV2, "agent=forge/1.0\n")
	wantV2 = pktLine(wantV2, "ls-refs=unborn\n")
	wantV2 = pktLine(wantV2, "fetch=shallow wait-for-done filter\n")
	wantV2 = pktLine(wantV2, "server-option\n")
	wantV2 = pktFlush(pktLine(wantV2, "object-format=sha1\n"))
	if got := advertiseV2(); !bytes.Equal(got, wantV2) {
		t.Errorf("the v2 advertisement changed:\n got %q\nwant %q", got, wantV2)
	}

	// What the bounty's rules ask of each advertisement, whatever else it carries.
	receive := capabilitySet(receivePackCaps)
	for _, name := range []string{"report-status", "side-band-64k", "delete-refs"} {
		if _, ok := receive[name]; !ok {
			t.Errorf("receive-pack does not advertise %s, which the rules require", name)
		}
	}
	upload := capabilitySet(uploadPackCaps)
	for _, name := range []string{"side-band-64k", "shallow"} {
		if _, ok := upload[name]; !ok {
			t.Errorf("upload-pack does not advertise %s, which a v0 fetch needs", name)
		}
	}
	if upload["object-format"] != "sha1" || receive["object-format"] != "sha1" {
		t.Errorf("an advertisement names an object format other than sha1")
	}
	first, v2 := v2Capabilities(t, advertiseV2())
	if first != "version 2" {
		t.Errorf("the v2 advertisement opens with %q", first)
	}
	if _, ok := v2["ls-refs"]; !ok {
		t.Errorf("the v2 advertisement has no ls-refs")
	}
	if !strings.Contains(" "+v2["fetch"]+" ", " shallow ") {
		t.Errorf("the v2 fetch command does not offer shallow (%q)", v2["fetch"])
	}
	if v2["object-format"] != "sha1" {
		t.Errorf("the v2 advertisement names object format %q", v2["object-format"])
	}

	// The constant openings and the first packets built from the strings are what goes on the wire.
	app := forgeWithRepo(t, "caps")
	repo := app.repos[defaultLogin+"/caps"]
	for service, caps := range map[string]string{"git-upload-pack": uploadPackCaps, "git-receive-pack": receivePackCaps} {
		want := pktFlush(pktLine(nil, "# service="+service+"\n"))
		want = pktFlush(pktLine(want, zeroHex+" capabilities^{}\x00"+caps+"\n"))
		if got := advertiseV0(repo, service, caps); !bytes.Equal(got, want) {
			t.Errorf("an empty repository's %s advertisement:\n got %q\nwant %q", service, got, want)
		}
	}
}

// gitAdvertisement runs a real git's advertisement for a service against an empty bare repository
// set up to offer everything optional the forge offers.
func gitAdvertisement(t *testing.T, dir, service string, v2 bool) []byte {
	t.Helper()
	cmd := exec.Command("git", strings.TrimPrefix(service, "git-"), "--advertise-refs", dir)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+dir, "GIT_CONFIG_GLOBAL="+os.DevNull)
	if v2 {
		cmd.Env = append(cmd.Env, "GIT_PROTOCOL=version=2")
	}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s --advertise-refs: %v", service, err)
	}
	return out
}

func TestCapabilitiesAreOnesGitKnows(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git on this machine to compare with")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "--bare", "-q", dir},
		{"-C", dir, "config", "uploadpack.allowFilter", "true"},
		{"-C", dir, "config", "uploadpack.allowTipSHA1InWant", "true"},
		{"-C", dir, "config", "uploadpack.allowReachableSHA1InWant", "true"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+dir, "GIT_CONFIG_GLOBAL="+os.DevNull)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
	}
	// v0: the capabilities ride behind a NUL on the first packet.
	for service, ours := range map[string]string{"git-upload-pack": uploadPackCaps, "git-receive-pack": receivePackCaps} {
		lines := packetLines(t, gitAdvertisement(t, dir, service, false))
		if len(lines) == 0 {
			t.Fatalf("git advertised nothing for %s", service)
		}
		_, list, found := strings.Cut(lines[0], "\x00")
		if !found {
			t.Fatalf("git's first %s packet carries no capabilities: %q", service, lines[0])
		}
		known := capabilitySet(list)
		for name := range capabilitySet(ours) {
			if _, ok := known[name]; !ok {
				t.Errorf("%s: the forge advertises %s, which git does not (git: %s)", service, name, list)
			}
		}
		if known["object-format"] != "sha1" {
			t.Errorf("%s: git names object format %q here", service, known["object-format"])
		}
	}
	// v2: one capability a packet, commands with their features behind "=".
	first, known := v2Capabilities(t, gitAdvertisement(t, dir, "git-upload-pack", true))
	if first != "version 2" {
		t.Fatalf("git did not answer in protocol v2: %q", first)
	}
	_, ours := v2Capabilities(t, advertiseV2())
	for name, value := range ours {
		theirs, ok := known[name]
		if !ok {
			t.Errorf("v2: the forge advertises %s, which git does not", name)
			continue
		}
		if name == "agent" || name == "object-format" {
			continue
		}
		for _, feature := range strings.Fields(value) {
			if !strings.Contains(" "+theirs+" ", " "+feature+" ") {
				t.Errorf("v2: the forge offers %s=%s, which git does not (git: %s=%s)", name, feature, name, theirs)
			}
		}
	}
}
