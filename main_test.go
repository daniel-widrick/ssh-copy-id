package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
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
