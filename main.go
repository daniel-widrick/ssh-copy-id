// Command ssh-copy-id installs public keys in a remote host's authorized_keys.
// It mirrors the OpenSSH script's flags but runs natively on Windows by
// driving the system ssh client, so ssh_config, the agent, known_hosts and
// password prompts all behave exactly as they do for plain ssh.
package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

var version = "dev"

// sshCommand is the client binary; tests point it at a fake.
var sshCommand = "ssh"

const usage = `Usage: ssh-copy-id [-f] [-n] [-x] [-i [identity_file]] [-t target_path] [-F ssh_config] [[-o ssh_option] ...] [-p port] [user@]hostname
	-f: force mode -- copy keys without checking if they are already installed
	-n: dry run    -- no keys are actually copied
	-x: debug      -- print the ssh command being run
	-i: identity   -- public key file to install (".pub" is appended if missing);
	                  default is the agent's keys, else the newest ~/.ssh/id*.pub
	-t: target     -- remote authorized_keys path (default .ssh/authorized_keys)
	-F, -o, -p     -- passed through to ssh
	-h, -?: print this help
	-V: print version`

type options struct {
	force, dryRun, debug bool
	identity             string
	identitySet          bool
	target               string
	sshArgs              []string
	host                 string
}

func main() {
	opts, err := parseArgs(os.Args[1:])
	if errors.Is(err, errHelp) {
		fmt.Println(usage)
		return
	}
	if errors.Is(err, errVersion) {
		fmt.Println("ssh-copy-id", version)
		return
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "ssh-copy-id: %v\n%s\n", err, usage)
		os.Exit(1)
	}
	if err := run(opts); err != nil {
		fmt.Fprintf(os.Stderr, "ssh-copy-id: ERROR: %v\n", err)
		os.Exit(1)
	}
}

var (
	errHelp    = errors.New("help")
	errVersion = errors.New("version")
)

// parseArgs handles getopt-style flags: bundling (-fn), attached values
// (-p2222) and an optional value for -i, which takes the next word only when
// another word remains after it for the host.
func parseArgs(args []string) (*options, error) {
	o := &options{target: ".ssh/authorized_keys"}
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			rest = append(rest, args[i+1:]...)
			i = len(args)
		case len(a) < 2 || a[0] != '-':
			rest = append(rest, a)
		default:
			used, err := o.parseFlags(a[1:], args[i+1:])
			if err != nil {
				return nil, err
			}
			i += used
		}
	}
	if len(rest) != 1 {
		return nil, errors.New("exactly one [user@]hostname is required")
	}
	o.host = rest[0]
	return o, nil
}

// parseFlags handles one bundle such as "fn" or "p2222" and returns how many
// of the following words it consumed as a value.
func (o *options) parseFlags(bundle string, next []string) (int, error) {
	for j := 0; j < len(bundle); j++ {
		switch c := bundle[j]; c {
		case 'f':
			o.force = true
		case 'n':
			o.dryRun = true
		case 'x':
			o.debug = true
		case 'h', '?':
			return 0, errHelp
		case 'V':
			return 0, errVersion
		case 'i', 't', 'F', 'o', 'p':
			return o.setValue(c, bundle[j+1:], next)
		default:
			return 0, fmt.Errorf("illegal option -- %c", c)
		}
	}
	return 0, nil
}

// setValue stores a flag's value, attached (-p2222) or taken from the next
// word. The value of -i is optional: it takes the next word only when another
// word remains after it for the host.
func (o *options) setValue(c byte, attached string, next []string) (int, error) {
	val, used := attached, 0
	if val == "" {
		if c == 'i' && len(next) < 2 {
			o.identitySet = true
			return 0, nil
		}
		if len(next) == 0 {
			return 0, fmt.Errorf("option -%c requires an argument", c)
		}
		val, used = next[0], 1
	}
	switch c {
	case 'i':
		o.identity, o.identitySet = val, true
	case 't':
		o.target = val
	default:
		o.sshArgs = append(o.sshArgs, "-"+string(c), val)
	}
	return used, nil
}

func run(o *options) error {
	keys, source, err := selectKeys(o)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "ssh-copy-id: INFO: Source of key(s) to be installed: %s\n", source)

	mode := "add"
	switch {
	case o.dryRun:
		mode = "dry"
	case o.force:
		mode = "force"
	}
	remote := "exec sh -c " + shQuote(remoteScript) + " sh " + shQuote(o.target) + " " + mode

	args := append(append([]string{}, o.sshArgs...), "--", o.host, remote)
	if o.debug {
		fmt.Fprintf(os.Stderr, "ssh-copy-id: DEBUG: ssh %s\n", strings.Join(args, " "))
	}
	cmd := exec.Command(sshCommand, args...)
	cmd.Stdin = strings.NewReader(strings.Join(keys, "\n") + "\n")
	cmd.Stderr = os.Stderr
	var out bytes.Buffer
	cmd.Stdout = io.MultiWriter(&out, debugWriter(o.debug))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ssh failed: %w", err)
	}

	added, skipped := 0, 0
	for _, line := range strings.Split(out.String(), "\n") {
		if n, ok := strings.CutPrefix(strings.TrimSpace(line), "SSH-COPY-ID "); ok {
			fmt.Sscanf(n, "%d %d", &added, &skipped)
		}
	}
	if skipped > 0 {
		fmt.Fprintf(os.Stderr, "ssh-copy-id: INFO: %d key(s) already installed, skipped\n", skipped)
	}
	switch {
	case added == 0:
		fmt.Fprintln(os.Stderr, "\nssh-copy-id: WARNING: All keys were skipped because they already exist on the remote system.")
	case o.dryRun:
		fmt.Fprintf(os.Stderr, "\n=-=-=-=-=-=-=-=\nWould have added the following key(s):\n\n%s\n=-=-=-=-=-=-=-=\n", strings.Join(keys, "\n"))
	default:
		fmt.Fprintf(os.Stderr, "\nNumber of key(s) added: %d\n\nNow try logging into the machine, with: \"ssh %s%s\"\nand check to make sure that only the key(s) you wanted were added.\n\n",
			added, sshHint(o.sshArgs), o.host)
	}
	return nil
}

func debugWriter(on bool) io.Writer {
	if on {
		return os.Stderr
	}
	return io.Discard
}

func sshHint(args []string) string {
	var b strings.Builder
	for _, a := range args {
		if strings.ContainsAny(a, " \t") {
			a = "'" + a + "'"
		}
		b.WriteString(a + " ")
	}
	return b.String()
}

// selectKeys follows the OpenSSH script: an explicit -i wins, then keys held
// by the agent, then the most recently modified ~/.ssh/id*.pub.
func selectKeys(o *options) ([]string, string, error) {
	if o.identitySet && o.identity != "" {
		path := expandHome(o.identity)
		if !strings.HasSuffix(path, ".pub") {
			path += ".pub"
		}
		keys, err := readKeyFile(path)
		return keys, path, err
	}
	if !o.identitySet {
		if out, err := exec.Command("ssh-add", "-L").Output(); err == nil {
			if keys := parseKeys(string(out)); len(keys) > 0 {
				return keys, `"ssh-add -L"`, nil
			}
		}
	}
	path, err := newestDefaultKey()
	if err != nil {
		return nil, "", err
	}
	keys, err := readKeyFile(path)
	return keys, path, err
}

func newestDefaultKey() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	matches, _ := filepath.Glob(filepath.Join(home, ".ssh", "id*.pub"))
	if len(matches) == 0 {
		return "", fmt.Errorf("no identities found in %s; run ssh-keygen first or pass -i", filepath.Join(home, ".ssh"))
	}
	mtime := func(p string) int64 {
		if fi, err := os.Stat(p); err == nil {
			return fi.ModTime().UnixNano()
		}
		return 0
	}
	sort.Slice(matches, func(i, j int) bool { return mtime(matches[i]) > mtime(matches[j]) })
	return matches[0], nil
}

func readKeyFile(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	keys := parseKeys(string(data))
	if len(keys) == 0 {
		return nil, fmt.Errorf("%s contains no public keys", path)
	}
	return keys, nil
}

// parseKeys returns the non-empty, non-comment lines, with CRLF stripped so a
// key saved by a Windows editor does not land on the remote with a stray \r.
func parseKeys(s string) []string {
	var keys []string
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		keys = append(keys, line)
	}
	return keys
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}

// shQuote single-quotes s for a POSIX shell. The login shell on the remote
// only has to understand single quotes and exec, which sh, bash, zsh, fish
// and csh all do.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// remoteScript runs under sh on the remote with $1 = target path and
// $2 = add|force|dry, reading one key per line on stdin. A key counts as
// installed when its "type base64" pair appears anywhere in the file, so
// options and comments on existing lines do not cause duplicates.
const remoteScript = `
umask 077
t=$1 mode=$2
d=$(dirname "$t")
if [ "$mode" != dry ]; then
	mkdir -p "$d" || exit 1
	touch "$t" || exit 1
	if [ -s "$t" ] && [ -n "$(tail -c1 "$t")" ]; then echo >> "$t"; fi
fi
added=0 skipped=0
while IFS= read -r k || [ -n "$k" ]; do
	[ -z "$k" ] && continue
	kb=$(printf '%s\n' "$k" | cut -d' ' -f1,2)
	if [ "$mode" != force ] && [ -f "$t" ] && grep -qF -- "$kb" "$t"; then
		skipped=$((skipped+1))
		continue
	fi
	[ "$mode" = dry ] || printf '%s\n' "$k" >> "$t" || exit 1
	added=$((added+1))
done
if [ "$mode" != dry ] && command -v restorecon >/dev/null 2>&1; then
	restorecon -F "$d" "$t" >/dev/null 2>&1
fi
echo "SSH-COPY-ID $added $skipped"
`
