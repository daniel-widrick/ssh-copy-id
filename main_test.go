package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseArgs(t *testing.T) {
	tests := []struct {
		args []string
		want options
	}{
		{[]string{"host"}, options{target: ".ssh/authorized_keys", host: "host"}},
		{[]string{"-fn", "-p2222", "u@h"}, options{force: true, dryRun: true, target: ".ssh/authorized_keys", sshArgs: []string{"-p", "2222"}, host: "u@h"}},
		{[]string{"-i", "k", "-o", "A=b", "h"}, options{identity: "k", identitySet: true, target: ".ssh/authorized_keys", sshArgs: []string{"-o", "A=b"}, host: "h"}},
		{[]string{"-i", "h"}, options{identitySet: true, target: ".ssh/authorized_keys", host: "h"}},
		{[]string{"h", "-t", "/x/ak"}, options{target: "/x/ak", host: "h"}},
	}
	for _, tc := range tests {
		got, err := parseArgs(tc.args)
		if err != nil {
			t.Fatalf("%v: %v", tc.args, err)
		}
		if !reflect.DeepEqual(*got, tc.want) {
			t.Errorf("%v:\n got %+v\nwant %+v", tc.args, *got, tc.want)
		}
	}
	for _, bad := range [][]string{{}, {"a", "b"}, {"-z", "h"}, {"-p"}} {
		if _, err := parseArgs(bad); err == nil {
			t.Errorf("%v: expected error", bad)
		}
	}
}

func TestParseKeys(t *testing.T) {
	got := parseKeys("ssh-ed25519 AAAA a@b\r\n\r\n# c\nssh-rsa BBBB\n")
	want := []string{"ssh-ed25519 AAAA a@b", "ssh-rsa BBBB"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q want %q", got, want)
	}
}

// TestRemoteScript runs the remote side under a local sh when one exists.
func TestRemoteScript(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh on PATH")
	}
	target := filepath.ToSlash(filepath.Join(t.TempDir(), "sub", "authorized_keys"))
	runScript := func(mode, stdin string) string {
		cmd := exec.Command(sh, "-c", remoteScript, "sh", target, mode)
		cmd.Stdin = strings.NewReader(stdin)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", mode, err, out)
		}
		return strings.TrimSpace(string(out))
	}

	if got := runScript("dry", "ssh-ed25519 AAAA x\n"); got != "SSH-COPY-ID 1 0" {
		t.Errorf("dry: %q", got)
	}
	if _, err := os.Stat(target); err == nil {
		t.Error("dry run created the file")
	}
	if got := runScript("add", "ssh-ed25519 AAAA x\nssh-rsa BBBB y\n"); got != "SSH-COPY-ID 2 0" {
		t.Errorf("add: %q", got)
	}
	// Same key with a different comment is a duplicate; missing trailing newline is handled.
	f, _ := os.OpenFile(target, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`from="10.0.0.1" ssh-ecdsa CCCC z`)
	f.Close()
	if got := runScript("add", "ssh-ed25519 AAAA other\nssh-ecdsa CCCC z\nssh-ed25519 DDDD new"); got != "SSH-COPY-ID 1 2" {
		t.Errorf("dedupe: %q", got)
	}
	data, _ := os.ReadFile(target)
	want := "ssh-ed25519 AAAA x\nssh-rsa BBBB y\nfrom=\"10.0.0.1\" ssh-ecdsa CCCC z\nssh-ed25519 DDDD new\n"
	if string(data) != want {
		t.Errorf("file:\n%s\nwant:\n%s", data, want)
	}
	if got := runScript("force", "ssh-rsa BBBB y\n"); got != "SSH-COPY-ID 1 0" {
		t.Errorf("force: %q", got)
	}
}

// TestMain lets the test binary stand in for ssh: run the remote command,
// which is always the last argument, under the local sh.
func TestMain(m *testing.M) {
	if os.Getenv("SSHCOPYID_FAKE_SSH") == "1" {
		cmd := exec.Command("sh", "-c", os.Args[len(os.Args)-1])
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestRun(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh on PATH")
	}
	t.Setenv("SSHCOPYID_FAKE_SSH", "1")
	old := sshCommand
	sshCommand = os.Args[0]
	t.Cleanup(func() { sshCommand = old })

	dir := t.TempDir()
	key := filepath.Join(dir, "id_test")
	os.WriteFile(key+".pub", []byte("ssh-ed25519 AAAA test@host\r\n"), 0o600)
	target := filepath.ToSlash(filepath.Join(dir, "remote", "authorized_keys"))

	for _, args := range [][]string{
		{"-n", "-i", key, "-t", target, "host"},
		{"-x", "-i", key, "-t", target, "-p", "22", "host"},
		{"-i", key, "-t", target, "host"},
	} {
		o, err := parseArgs(args)
		if err != nil {
			t.Fatal(err)
		}
		if err := run(o); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "ssh-ed25519 AAAA test@host\n" {
		t.Errorf("authorized_keys = %q, %v", data, err)
	}

	o, _ := parseArgs([]string{"-i", filepath.Join(dir, "missing"), "host"})
	if err := run(o); err == nil {
		t.Error("missing key file: expected error")
	}
	sshCommand = filepath.Join(dir, "no-such-ssh")
	o, _ = parseArgs([]string{"-i", key, "host"})
	if err := run(o); err == nil {
		t.Error("missing ssh: expected error")
	}
}

func TestNewestDefaultKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	o := &options{identitySet: true} // bare -i: skip the agent
	if _, _, err := selectKeys(o); err == nil {
		t.Error("no keys: expected error")
	}
	ssh := filepath.Join(home, ".ssh")
	os.Mkdir(ssh, 0o700)
	old, newer := filepath.Join(ssh, "id_rsa.pub"), filepath.Join(ssh, "id_ed25519.pub")
	os.WriteFile(old, []byte("ssh-rsa OLD\n"), 0o600)
	os.WriteFile(newer, []byte("ssh-ed25519 NEW\n"), 0o600)
	past := time.Now().Add(-time.Hour)
	os.Chtimes(old, past, past)
	keys, src, err := selectKeys(o)
	if err != nil || src != newer || keys[0] != "ssh-ed25519 NEW" {
		t.Errorf("got %q from %s, %v", keys, src, err)
	}

	os.WriteFile(newer, []byte("# only a comment\n"), 0o600)
	if _, _, err := selectKeys(&options{identitySet: true, identity: "~/.ssh/id_ed25519.pub"}); err == nil {
		t.Error("empty key file: expected error")
	}
}

func TestHelpers(t *testing.T) {
	if got := shQuote("it's"); got != `'it'\''s'` {
		t.Errorf("shQuote = %s", got)
	}
	if got := sshHint([]string{"-o", "A B", "-p", "2"}); got != "-o 'A B' -p 2 " {
		t.Errorf("sshHint = %q", got)
	}
	if got := expandHome("/abs"); got != "/abs" {
		t.Errorf("expandHome = %q", got)
	}
	for _, a := range []string{"-h", "-V"} {
		if _, err := parseArgs([]string{a}); err != errHelp && err != errVersion {
			t.Errorf("%s: %v", a, err)
		}
	}
}
