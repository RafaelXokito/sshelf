# MVP manual acceptance checklist

(The M2 checklist follows the MVP sign-off at the end of this file.)

Status: Ready to run · Owner: product owner · Task: T-030

This is the hands-on part of the MVP gate (T-030). Everything else is
automated: `go test -race ./...`, the traceability gate
(`go run ./scripts/tracecheck`) and the coverage gate
(`go run ./scripts/covercheck`). These steps cover what only a person at a
real terminal can judge: rendering, full-screen programs, the OS keychain
UI and the terminal state after sshelf exits.

Run it top to bottom on **macOS** (required) and again on **Linux** if
available (section 11 lists the Linux differences). Tick **Pass** or
**Fail** for each step and write anything odd under it. Record the OS
version, terminal emulator and `sshelf --version` in the header below.

| | macOS | Linux |
|---|---|---|
| OS version | | |
| Terminal emulator | | |
| `sshelf --version` | | |
| Tester, date | | |

**What this run touches.** The config directory is redirected to a scratch
directory (`XDG_CONFIG_HOME`), so your real sshelf config is untouched. The
**secrets go to your real login Keychain** (that is what is being tested),
under the service `sshelf`; section 9 deletes them again. `~/.ssh/config` is
read by the import but never written. Nothing else is changed.

---

## 0. Setup

Every terminal you open for this checklist needs these lines first (paste
them as they are):

```sh
cd /path/to/sshelf            # the repository checkout
export SSHELF_ACC=/tmp/sshelf-acceptance
export XDG_CONFIG_HOME="$SSHELF_ACC/xdg"
export CFG="$XDG_CONFIG_HOME/sshelf"
```

On macOS sshelf uses `$XDG_CONFIG_HOME/sshelf` because the variable is set
explicitly; without it the config would be in
`~/Library/Application Support/sshelf`.

- [ ] **0.1 Start clean.**

  ```sh
  rm -rf "$SSHELF_ACC" && mkdir -m 700 -p "$SSHELF_ACC"
  ```

  Expected: no output. — Pass [ ] Fail [ ]

## 1. Build and run the binary

- [ ] **1.1 Build.**

  ```sh
  go build -o "$SSHELF_ACC/sshelf" ./cmd/sshelf && ls -l "$SSHELF_ACC/sshelf"
  ```

  Expected: the build succeeds and the binary exists. — Pass [ ] Fail [ ]

- [ ] **1.2 Version and help.**

  ```sh
  "$SSHELF_ACC/sshelf" --version; "$SSHELF_ACC/sshelf" --help; "$SSHELF_ACC/sshelf" --bogus; echo "exit $?"
  ```

  Expected: one line `sshelf <version> (commit <hash>, built <date>)`
  (for a local build the version and commit come from the Go build info);
  the help text lists only `--help` and `--version`; `--bogus` prints
  `flag provided but not defined` with the usage and `exit 2`. — Pass [ ] Fail [ ]

- [ ] **1.3 Not a terminal.**

  ```sh
  echo | "$SSHELF_ACC/sshelf"; echo "exit $?"
  ```

  Expected: `sshelf needs an interactive terminal` and `exit 1`, no
  garbage on the screen. — Pass [ ] Fail [ ]

## 2. Local SSH targets

Open two extra terminal windows (A and B), run the Setup lines of section 0
in each, then:

- [ ] **2.1 Target A: password and key file (port 2201).** In terminal A:

  ```sh
  go run ./internal/testutil/sshtest/cmd/sshtestd -dir "$SSHELF_ACC/target-a" -port 2201 -password acc-pw-A
  ```

  Expected: `sshtestd listening on 127.0.0.1:2201`, `accepted
  authentication: public key (the demo key); password: …`, a known_hosts
  line and the demo key path `$SSHELF_ACC/target-a/demo_ed25519`.
  — Pass [ ] Fail [ ]

- [ ] **2.2 Target B: password then one-time code (port 2202).** In
  terminal B:

  ```sh
  go run ./internal/testutil/sshtest/cmd/sshtestd -dir "$SSHELF_ACC/target-b" -port 2202 -password acc-pw-B -otp 424242
  ```

  Expected: `listening on 127.0.0.1:2202` and `keyboard-interactive: the
  -password, then the -otp code`. — Pass [ ] Fail [ ]

- [ ] **2.3 A real `sshd` for full-screen programs (section 6).** On macOS:
  System Settings → General → Sharing → **Remote Login** on (allow your
  user). On Linux: `sudo systemctl start ssh` (or `sshd`). Make sure `vim`,
  `htop` and `tmux` are installed on that host. A remote VM works as well.

  ```sh
  ssh -o BatchMode=yes -p 22 "$USER@127.0.0.1" true 2>&1 | head -1
  ```

  Expected: port 22 answers (a "Permission denied" from BatchMode is fine).
  — Pass [ ] Fail [ ] — N/A, using a remote server: ______________

sshtestd's shell is a stub: it understands only `echo <words>`, `size` and
`exit [N]`.

## 3. First run, Welcome screen, `~/.ssh/config` import

- [ ] **3.1 Terminal state before launch.** In your main terminal:

  ```sh
  stty -a > "$SSHELF_ACC/stty-before"
  shasum ~/.ssh/config 2>/dev/null > "$SSHELF_ACC/sshconfig-before"; echo done
  ```

  Expected: `done`. — Pass [ ] Fail [ ]

- [ ] **3.2 First run.**

  ```sh
  "$SSHELF_ACC/sshelf"
  ```

  Expected: the alternate screen opens with **Welcome to sshelf**, "You
  have no saved hosts yet", `[I] Import from ~/.ssh/config`, `[a] Add host`,
  and the footer `I import ~/.ssh/config • a add host • q quit`. In another
  terminal, `ls -ld "$CFG"; ls -l "$CFG"` shows the directory with mode
  `drwx------`. — Pass [ ] Fail [ ]

- [ ] **3.3 Import.** Press `I`.

  Expected: an import report: how many hosts and identities were created and
  any skipped entries (wildcard patterns, unsupported directives such as
  `ProxyJump`) with the reason. With no `~/.ssh/config`, the status says
  `no ~/.ssh/config found` and nothing crashes. Press `enter` to reach the host
  list; the imported hosts are listed by label. — Pass [ ] Fail [ ] — N/A,
  no `~/.ssh/config` [ ]

- [ ] **3.4 `~/.ssh/config` untouched.** In another terminal:

  ```sh
  shasum ~/.ssh/config 2>/dev/null | diff - "$SSHELF_ACC/sshconfig-before" && echo unchanged
  ```

  Expected: `unchanged`. — Pass [ ] Fail [ ]

- [ ] **3.5 No mouse capture.** Select some text on the sshelf screen with
  the mouse.

  Expected: the terminal's own selection works (sshelf does not capture the
  mouse). — Pass [ ] Fail [ ]

## 4. Add hosts

The host form fields are Label, Hostname, Port, Username, Identity,
Password, Key file, Stored key, Passphrase. `tab`/`shift+tab` move between
fields, `←`/`→` change the Identity choice, `ctrl+s` saves, `esc` cancels.

- [ ] **4.1 With a password.** On the host list press `a` and fill in:
  Label `acc-password`, Hostname `127.0.0.1`, Port `2201`, Username
  `alice`, Password `acc-pw-A`. Press `ctrl+s`.

  Expected: the Password field never shows the characters or their count
  (only "(set)" style text); back on the list, `acc-password` is selected
  and the status line says `acc-password: saved`. — Pass [ ] Fail [ ]

- [ ] **4.2 Secret not on disk.** In another terminal:

  ```sh
  grep -rl 'acc-pw-' "$CFG" || echo "no secret on disk"; ls -l "$CFG"
  ```

  Expected: `no secret on disk`; `config.json` is `-rw-------`.
  — Pass [ ] Fail [ ]

- [ ] **4.3 With a key file.** `a`, Label `acc-keyfile`, Hostname
  `127.0.0.1`, Port `2201`, Username `bob`, Key file
  `/tmp/sshelf-acceptance/target-a/demo_ed25519`, `ctrl+s`.

  Expected: `acc-keyfile: saved`. — Pass [ ] Fail [ ]

- [ ] **4.4 Key-file permission check.** In another terminal
  `chmod 644 "$SSHELF_ACC/target-a/demo_ed25519"`, then in sshelf press
  `enter` on `acc-keyfile` (accept the host key with `y` if asked, see 5.1).
  Afterwards run `chmod 600 "$SSHELF_ACC/target-a/demo_ed25519"`.

  Expected: the connection is refused with a message that the key file is
  readable by others (no session opens). — Pass [ ] Fail [ ]

- [ ] **4.5 With a stored key (identity, imported into the Keychain).**
  Press `i` (Identities), then `a`, and fill in: Name `acc-stored`,
  Username `carol`, Key file `/tmp/sshelf-acceptance/target-a/demo_ed25519`;
  with the cursor on Key file press `ctrl+o` ("store key in keychain"),
  then `ctrl+s`. Press `esc` to return to the host list, `a`, Label
  `acc-stored`, Hostname `127.0.0.1`, Port `2201`, Identity → `acc-stored`
  (with `→`), `ctrl+s`.

  Expected: the identity shows `Stored key set`; the host saves with
  `acc-stored: saved`. — Pass [ ] Fail [ ]

- [ ] **4.6 With a stored key pasted into the host form.** `a`, Label
  `acc-pasted`, Hostname `127.0.0.1`, Port `2201`, Username `dave`; on
  **Stored key** paste the key (in another terminal
  `cat "$SSHELF_ACC/target-a/demo_ed25519" | pbcopy` on macOS, or
  `xclip -sel clip` on Linux, then paste with ⌘V / ctrl+shift+V); `ctrl+s`.

  Expected: the field shows that a key is set, never the key text;
  `acc-pasted: saved`. — Pass [ ] Fail [ ]

- [ ] **4.7 Password + OTP host.** `a`, Label `acc-otp`, Hostname
  `127.0.0.1`, Port `2202`, Username `erin`, Password `acc-pw-B`, `ctrl+s`.

  Expected: `acc-otp: saved`. — Pass [ ] Fail [ ]

## 5. Trust, search and connect

- [ ] **5.1 TOFU.** Select `acc-keyfile` and press `enter`.

  Expected: a "Connecting to acc-keyfile…" box, then **Unknown host key**
  with host `127.0.0.1`, port `2201`, key type `ssh-ed25519` and a
  `SHA256:` fingerprint that matches
  `ssh-keygen -lf <(cut -d' ' -f2- <<< "<known_hosts line from terminal A>")`.
  `enter` and `esc` cancel; only `y` trusts. Press `esc` first: back on the
  list with `acc-keyfile: host key not trusted; connection cancelled` and
  **no** login on terminal A. — Pass [ ] Fail [ ]

- [ ] **5.2 Trust and save.** `enter` on `acc-keyfile` again, press `y`.

  Expected: tab `1 acc-keyfile` opens with a `$ ` prompt from the stub
  shell inside the frame. `grep 2201 "$CFG/known_hosts"` in another
  terminal shows one line. — Pass [ ] Fail [ ]

- [ ] **5.3 Exit back to the list.** In the session type `exit 3`, then
  press `x`.

  Expected: after `exit 3` the last screen stays and the status bar reads
  `✕ exited (3) · r reconnect · x close`; `x` closes the tab and the host
  list comes back with `acc-keyfile` still selected. The first key you
  press afterwards (e.g. `j`) is handled by the list. — Pass [ ] Fail [ ]

- [ ] **5.4 No second prompt.** `enter` on `acc-keyfile`, then `exit` and
  `x`.

  Expected: no host-key prompt this time; the ended tab shows
  `✕ exited (0)`. — Pass [ ] Fail [ ]

- [ ] **5.5 Password, stored key and pasted key connect.** Connect to
  `acc-password`, `acc-stored` and `acc-pasted` in turn (each `exit`, then
  `x`).

  Expected: each opens the stub shell in a tab without asking for anything
  (the host key of port 2201 is already trusted) and ends with
  `✕ exited (0)`. — Pass [ ] Fail [ ]

- [ ] **5.6 Fuzzy search.** Press `/` and type `akf`.

  Expected: the list filters to `acc-keyfile` (matched as you type, no
  noticeable lag); `enter` connects to it (`exit` and `x` again); `esc`
  clears the search and shows every host. — Pass [ ] Fail [ ]

- [ ] **5.7 Changed host key is blocked.** Stop target A with `ctrl+c` in
  terminal A and start it with a **different** host key on the same port:

  ```sh
  go run ./internal/testutil/sshtest/cmd/sshtestd -dir "$SSHELF_ACC/target-a-new" -port 2201 -password acc-pw-A
  ```

  In sshelf connect to `acc-password`.

  Expected: **WARNING: HOST KEY HAS CHANGED** with the presented and trusted
  fingerprints, the path and line number of the stale entry in
  `$CFG/known_hosts` and the `ssh-keygen -R` command; there is no key that
  accepts the new key; the status says `host key changed; connection
  blocked`; terminal A logged no password attempt. `esc` closes the block.
  — Pass [ ] Fail [ ]

- [ ] **5.8 Restore target A.** `ctrl+c` in terminal A, then run the 2.1
  command again (same `-dir`, original key). Connect to `acc-password`,
  `exit` and `x`.

  Expected: connects without a prompt. — Pass [ ] Fail [ ]

## 6. Full-screen programs, resize and exit (real `sshd`)

- [ ] **6.1 Add the real host.** `a`, Label `acc-real`, Hostname
  `127.0.0.1` (or your server), Port `22`, Username your login name,
  Password your login password (or Key file `~/.ssh/id_ed25519`), `ctrl+s`,
  then `enter` (accept the host key with `y` if asked).

  Expected: your normal remote shell prompt. — Pass [ ] Fail [ ]

- [ ] **6.2 TERM and size.** On the remote shell:

  ```sh
  echo "$TERM"; tput cols; tput lines
  ```

  Expected: `xterm-256color` (the terminal sshelf's emulator implements,
  whatever your local `$TERM`), your window's width, and its height minus
  2 (the header and the status bar). — Pass [ ] Fail [ ]

- [ ] **6.3 vim.** `vim /tmp/sshelf-acc.txt`, type `ihello`, `esc`, move with
  `h j k l` and arrow keys, `:wq`.

  Expected: vim draws correctly (no stray characters, status line at the
  bottom), keys and `esc` act immediately, and after `:wq` the shell prompt
  is clean. — Pass [ ] Fail [ ]

- [ ] **6.4 htop.** `htop`, use the arrow keys and `F10`/`q` to quit.

  Expected: colours and the full-screen layout render; it quits back to a
  clean prompt (FR-CONN-005: no remnants of htop's screen). — Pass [ ] Fail [ ]

- [ ] **6.5 tmux.** `tmux`, split with `ctrl+b %`, move with `ctrl+b →`,
  type in both panes, then `exit` both panes.

  Expected: the prefix key works (sshelf does not swallow `ctrl+b`), panes
  draw correctly, tmux exits to the shell. — Pass [ ] Fail [ ]

- [ ] **6.6 Window resize.** Run `tput cols; tput lines`, resize the
  terminal window, run it again. Also run `vim`, resize while it is open,
  and quit.

  Expected: the second output shows the new size; vim redraws to the new
  size. (On sshtestd the same check is `size`.) — Pass [ ] Fail [ ]

- [ ] **6.7 Ctrl+C reaches the remote.** Run `sleep 30` and press `ctrl+c`.

  Expected: only the remote `sleep` is interrupted; the session and sshelf
  keep running. — Pass [ ] Fail [ ]

- [ ] **6.8 Back to the list.** `exit`, then `x`.

  Expected: the ended tab shows `✕ exited (0)`; `x` redraws the host list
  cleanly with `acc-real` selected. — Pass [ ] Fail [ ]

## 7. Keyboard-interactive, one-time code and Esc

- [ ] **7.1 OTP.** Connect to `acc-otp` and accept its host key with `y`.

  Expected: the stored password answers the first round on its own; a box
  **Prompt from 127.0.0.1** with `erin@127.0.0.1`, the server text "Second
  factor" and `Verification code:` appears. Type `424242` (it echoes,
  because the server asked for echo) and `enter`: the stub shell opens.
  `exit`. — Pass [ ] Fail [ ]

- [ ] **7.2 Wrong code.** Connect to `acc-otp`, type `000000`, `enter`.

  Expected: sshelf asks again (the stored password is used only once per
  attempt, so the retry asks for the password and then the code), up to
  three failed tries in total; type wrong answers until the list shows
  `acc-otp: authentication failed`. A prompt never vanishes leaving
  "Connecting" stuck. — Pass [ ] Fail [ ]

- [ ] **7.3 Esc cancels a prompt.** Connect to `acc-otp`, and at the
  Verification code prompt press `esc`.

  Expected: back on the list at once with `acc-otp: authentication
  cancelled`; no further attempt on terminal B. — Pass [ ] Fail [ ]

- [ ] **7.4 Typed password, then retry.** Press `e` on `acc-password`, move
  to Password and clear it with `ctrl+x`, `ctrl+s`. Connect.

  Expected: **Password for alice@127.0.0.1**; typing shows nothing (not
  even dots). Type a wrong password: the prompt returns with "Permission
  denied, please try again."; type `acc-pw-A`: the shell opens. `exit`.
  — Pass [ ] Fail [ ]

- [ ] **7.5 Esc cancels a slow connect.** Add a host `acc-blackhole`,
  Hostname `10.255.255.1`, Port `22`, save, connect, and press `esc` within
  a few seconds.

  Expected: back on the list within a fraction of a second with
  `connection cancelled`. — Pass [ ] Fail [ ]

## 8. Keychain items (macOS)

- [ ] **8.1 Items exist under the `sshelf` service.** Open **Keychain
  Access**, select the **login** keychain, and search for `sshelf`.

  Expected: one "application password" item per stored secret, each with
  Name/Where `sshelf` and Account `<identity-uuid>/<field>`, where field is
  `password`, `private-key` or `key-passphrase`. For this run: passwords
  for `acc-otp` (and `acc-password` if 7.4 was skipped), private keys for
  `acc-stored` and `acc-pasted`. The UUIDs are the `identities[].id` values
  in `$CFG/config.json`. — Pass [ ] Fail [ ]

- [ ] **8.2 Same check from the shell (no secret printed).**

  ```sh
  grep -o '"id": "[^"]*"' "$CFG/config.json"
  security find-generic-password -s sshelf -a "<an identity id>/private-key" | head -12
  ```

  Expected: the item's attributes (`"svce"<blob>="sshelf"`,
  `"acct"<blob>="<uuid>/private-key"`); no secret value is printed (no
  `-w`). — Pass [ ] Fail [ ]

- [ ] **8.3 No Keychain dialog.** Throughout sections 4 to 7, note whether
  macOS showed any "sshelf wants to use your confidential information"
  dialog.

  Expected: none (go-keyring goes through `/usr/bin/security`, D-008).
  — Pass [ ] Fail [ ]

## 9. Delete host and identity, Keychain cleanup

- [ ] **9.1 Delete a host with its own credentials.** Select `acc-otp`,
  press `d`.

  Expected: "Delete host acc-otp?"; `n` keeps it; `d` then `y` deletes it
  and the status says `acc-otp: deleted`. In Keychain Access (refresh the
  search) its password item is gone. — Pass [ ] Fail [ ]

- [ ] **9.2 Delete an identity.** `i`, select `acc-stored`, `d`.

  Expected: "Delete identity acc-stored and its secrets? 1 linked host will
  be unlinked."; `y` deletes it; its `private-key` item is gone from
  Keychain Access; host `acc-stored` remains with Identity `(none)`.
  — Pass [ ] Fail [ ]

- [ ] **9.3 Remove the rest.** Delete the remaining `acc-*` hosts (`d`, `y`)
  and any remaining identity in `i`.

  Expected: Keychain Access shows no `sshelf` item created by this run.
  — Pass [ ] Fail [ ]

- [ ] **9.4 Quit and compare the terminal.** Press `q`, then:

  ```sh
  stty -a | diff - "$SSHELF_ACC/stty-before" && echo "terminal restored"
  ```

  Expected: `terminal restored`, the normal screen is back with your
  scrollback, the cursor is visible and typing echoes. — Pass [ ] Fail [ ]

- [ ] **9.5 Debug log has no secrets.**

  ```sh
  SSHELF_DEBUG=1 "$SSHELF_ACC/sshelf"   # connect to any acc-* host left, or add acc-password again and connect, then q
  log="$HOME/Library/Logs/sshelf/sshelf.log"   # Linux: ${XDG_STATE_HOME:-$HOME/.local/state}/sshelf/sshelf.log
  ls -l "$log"; grep -c 'acc-pw-' "$log"
  ```

  Expected: the log exists with mode `-rw-------`, shows connect stages and
  auth methods, and the count is `0`. Without `SSHELF_DEBUG` no log is
  written. Delete what you added in this step. — Pass [ ] Fail [ ]

## 10. Raw bytes and `stty` round trip (ADR-007)

Since M2 (T-037) sessions run inside sshelf's frame and the terminal is
never handed over, so the T-000 `devcheck` program is gone; this section
checks the same things with sshelf itself.

- [ ] **10.1 Keys reach the remote.**

  ```sh
  stty -a > "$SSHELF_ACC/stty-dev-before"
  "$SSHELF_ACC/sshelf"
  ```

  Connect to `acc-keyfile` and type `echo-hex` and `enter`. Press
  `ctrl+c`, `↑`, `q`.

  Expected: the stub prints `03 1b 5b 41 71` inside the frame; neither
  `ctrl+c` nor `q` quits sshelf. — Pass [ ] Fail [ ]

- [ ] **10.2 First keystroke after the session.** `ctrl+]` `q`, `y`, then
  press `j` once.

  Expected: the tab closes and the host list moves on that first
  keystroke. — Pass [ ] Fail [ ]

- [ ] **10.3 `stty` round trip.** Press `q`, then:

  ```sh
  stty -a | diff - "$SSHELF_ACC/stty-dev-before" && echo "stty unchanged"
  ```

  Expected: `stty unchanged`. — Pass [ ] Fail [ ]

## 11. Linux run

Repeat sections 0 to 10 on Linux, with these differences:

- Secrets go to the Secret Service (GNOME Keyring or KWallet). Section 8:
  open **Passwords and Keys** (Seahorse) or KWalletManager and look for
  items labelled `Password for '<uuid>/<field>' on 'sshelf'`, or run
  `secret-tool search service sshelf` (prints secrets; use only with these
  test values).
- Paste in 4.6 with `xclip -sel clip < "$SSHELF_ACC/target-a/demo_ed25519"`.
- Debug log: `${XDG_STATE_HOME:-$HOME/.local/state}/sshelf/sshelf.log`.

- [ ] **11.1 Keyring unlock prompt (FR-CRED-001).** Lock the login keyring
  (Seahorse: right-click **Login** → Lock), then connect to a host with a
  stored password.

  Expected: the desktop's keyring unlock dialog appears; after unlocking,
  the connection authenticates. Cancelling it gives a clear "access denied"
  status and no crash. — Pass [ ] Fail [ ]

- [ ] **11.2 No credential store (D-101).** In a shell without a session
  bus: `env -u DBUS_SESSION_BUS_ADDRESS "$SSHELF_ACC/sshelf"`.

  Expected: `! No Secret Service found: install or start GNOME Keyring or
  KWallet` above the list; in the host and identity forms Password and
  Stored key show `(disabled: no Secret Service)`; a key-file host
  (`acc-keyfile`) still connects. — Pass [ ] Fail [ ]

## 12. Release-only items

These need a second build or other terminals. They are also in
[`docs/release-checklist.md`](../docs/release-checklist.md).

- [ ] **12.1 SEC-CRED-009: no Keychain dialog after an upgrade (macOS).**

  ```sh
  go build -ldflags "-X main.version=N" -o "$SSHELF_ACC/sshelf" ./cmd/sshelf
  ```

  Add `acc-upgrade` (127.0.0.1:2201, user `alice`, Password `acc-pw-A`),
  connect, `exit`, `q`. Rebuild with `-X main.version=N+1` (an unsigned,
  different binary) and connect again.

  Expected: no Keychain "allow access" dialog; the stored password
  authenticates. Delete `acc-upgrade` afterwards. — Pass [ ] Fail [ ]

- [ ] **12.2 NFR-PORT-002: named terminals.** Repeat 3.2, 5.2, 5.3, 6.3 and
  9.4 in each emulator you have: iTerm2, Terminal.app, kitty, WezTerm,
  Alacritty, Ghostty, GNOME Terminal, Konsole, xterm, and inside tmux and
  screen.

  Expected: the list renders, the session works, the terminal is restored.
  Note which emulators were tried: ______________ — Pass [ ] Fail [ ]

## 13. Teardown

```sh
rm -rf /tmp/sshelf-acceptance /tmp/sshelf-acc.txt
```

Stop sshtestd in terminals A and B with `ctrl+c`, and turn Remote Login off
again if you turned it on for 2.3.

## Sign-off

| | macOS | Linux |
|---|---|---|
| All steps pass (or N/A with reason) | [ ] | [ ] |
| Failures (step numbers, issue links) | | |
| Signed off by | | |

---

# M2 manual acceptance checklist (framed sessions, tabs, mouse)

Status: Ready to run · Owner: product owner · Task: T-043

This is the hands-on part of the M2 gate. Everything else is automated
(`go test -race ./...`, `go run ./scripts/tracecheck`, which now covers MVP
and M2, and `go run ./scripts/covercheck`). These steps cover what only a
person at a real terminal can judge: real full-screen programs inside the
frame, how keys and the mouse feel, the terminal's own text selection and
copy, the clipboard, and the terminal state after sshelf exits.

The checklist is self-contained: it does not need the MVP run above. Run it
top to bottom on **macOS** (required; the clipboard steps use `pbcopy` and
`pbpaste`), in one terminal emulator first (iTerm2 or Terminal.app), and
then repeat the steps M2.12 lists in the other terminals you have. Linux
differences are in M2.13. Tick **Pass** or **Fail** for each step and
write anything odd under it.

| | macOS | Linux |
|---|---|---|
| OS version | | |
| Terminal emulator(s) | | |
| `sshelf --version` | | |
| Real host used (M2.0.5) | | |
| Tester, date | | |

**What this run touches.** The config directory is redirected to a scratch
directory (`XDG_CONFIG_HOME`), so your real sshelf config is untouched.
The passwords typed in M2.1 go to your login Keychain under the service
`sshelf`; M2.14 deletes them. `~/.ssh` is not read or written by any step.
The clipboard step M2.8 overwrites your clipboard with a test string.

**Keys used below.** `ctrl+]` is sshelf's prefix key: press `ctrl` and `]`
together, let go, then press the next key. `ctrl+]` `h` means "prefix, then
`h`". The prefix commands are: `1`…`9` tab, `h` or `0` Hosts tab, `n` next
tab, `p` previous tab, `[` scrollback, `q` disconnect, `?` help, `ctrl+]`
sends a literal `ctrl+]` to the remote, `esc` cancels.

---

## M2.0 Setup

Open **four** terminal windows: **main** (sshelf runs here), **A** and
**C** (two test servers) and **side** (for checks). Paste these lines into
each of them first:

```sh
cd /path/to/sshelf            # the repository checkout
export SSHELF_ACC=/tmp/sshelf-acceptance-m2
export XDG_CONFIG_HOME="$SSHELF_ACC/xdg"
export CFG="$XDG_CONFIG_HOME/sshelf"
```

- [ ] **M2.0.1 Start clean and build.** In **side**:

  ```sh
  rm -rf "$SSHELF_ACC" && mkdir -m 700 -p "$SSHELF_ACC"
  go build -o "$SSHELF_ACC/sshelf" ./cmd/sshelf && "$SSHELF_ACC/sshelf" --version
  ```

  Expected: one line `sshelf <version> (commit <hash>, built <date>)`.
  — Pass [ ] Fail [ ]

- [ ] **M2.0.2 Test server A (port 2201).** In **A**:

  ```sh
  go run ./internal/testutil/sshtest/cmd/sshtestd -dir "$SSHELF_ACC/target-a" -port 2201 -password acc-pw-A
  ```

  Expected: `sshtestd listening on 127.0.0.1:2201` and a known_hosts line.
  Leave it running. — Pass [ ] Fail [ ]

- [ ] **M2.0.3 Test server C (port 2203).** In **C**:

  ```sh
  go run ./internal/testutil/sshtest/cmd/sshtestd -dir "$SSHELF_ACC/target-c" -port 2203 -password acc-pw-C
  ```

  Expected: `sshtestd listening on 127.0.0.1:2203`. Leave it running.
  — Pass [ ] Fail [ ]

  sshtestd's shell is a stub: it understands only `echo <words>`, `size`
  and `exit [N]`.

- [ ] **M2.0.4 A real `sshd`.** On macOS: System Settings → General →
  Sharing → **Remote Login** on (allow your user), or use any server you
  can log in to. `vim`, `htop`, `less` and `tmux` must be installed there
  (on macOS: `brew install htop tmux`). In **side**:

  ```sh
  export REAL_HOST=127.0.0.1 REAL_PORT=22 REAL_USER="$USER"   # or your server
  ssh -o BatchMode=yes -p "$REAL_PORT" "$REAL_USER@$REAL_HOST" true 2>&1 | head -1; echo checked
  ```

  Expected: `checked` (a "Permission denied" line from BatchMode is fine;
  "Connection refused" is not). Write the host into the header table.
  — Pass [ ] Fail [ ]

- [ ] **M2.0.5 Terminal state and clipboard before launch.** In **main**:

  ```sh
  stty -a > "$SSHELF_ACC/stty-before"
  printf 'clipboard-before-sshelf' | pbcopy; pbpaste; echo
  ```

  Expected: `clipboard-before-sshelf`. — Pass [ ] Fail [ ]

## M2.1 Add the hosts with the mouse (forms and buttons)

In **main**, start sshelf and keep it running until M2.11:

```sh
"$SSHELF_ACC/sshelf"
```

- [ ] **M2.1.1 Welcome screen and header.** Look at the first screen.

  Expected: the first line is the header `sshelf │[Hosts]│`; below it the
  Welcome screen with `[I] Import from ~/.ssh/config` and `[a] Add host`.
  — Pass [ ] Fail [ ]

- [ ] **M2.1.2 Click a Welcome action.** Click `[a] Add host`.

  Expected: the host form opens with a `[ Save ]` and a `[ Cancel ]`
  button at the bottom. — Pass [ ] Fail [ ]

- [ ] **M2.1.3 Click fields and Save.** Click the **Label** field and type
  `acc-a`; click **Hostname**, type `127.0.0.1`; click **Port**, type
  `2201`; click **Username**, type `alice`; click **Password**, type
  `acc-pw-A`. Click `[ Save ]`.

  Expected: each click puts the cursor in the clicked field; the Password
  field never shows the characters; after the click on `[ Save ]` the host
  list shows `acc-a` selected and the status line says `acc-a: saved`.
  — Pass [ ] Fail [ ]

- [ ] **M2.1.4 Save errors and Cancel.** Press `a`. Click `[ Save ]` with
  every field empty. Then type `x` into Label and click `[ Cancel ]`; at
  the "discard changes?" question press `y`.

  Expected: `[ Save ]` shows the missing-label error and stays on the
  form; `[ Cancel ]` with a change asks before discarding; `y` returns to
  the list with no new host. — Pass [ ] Fail [ ]

- [ ] **M2.1.5 Choice arrows.** Press `a`, click the `›` next to
  **Identity**, then the `‹`, then click `[ Cancel ]`.

  Expected: each click changes the choice by one, exactly as `→`/`←` do;
  Cancel with no change closes the form at once. — Pass [ ] Fail [ ]

- [ ] **M2.1.6 Second test host, keyboard only.** `a`, then type
  `acc-c`, `tab`, `127.0.0.1`, `tab`, `2203`, `tab`, `carol`; `tab` to
  Password, type `acc-pw-C`; `tab` until `[ Save ]` is highlighted and
  press `enter`.

  Expected: `acc-c: saved` (the buttons are reachable with `tab`).
  — Pass [ ] Fail [ ]

- [ ] **M2.1.7 Real host.** `a`, Label `acc-real`, Hostname `$REAL_HOST`,
  Port `$REAL_PORT`, Username `$REAL_USER` (type the values, not the
  variable names), and either Password (your login password) or Key file
  (for example `~/.ssh/id_ed25519`); `ctrl+s`.

  Expected: `acc-real: saved`. — Pass [ ] Fail [ ]

## M2.2 Tabs: two sshtestd sessions and a real host

- [ ] **M2.2.1 Double-click to connect, trust on first use.**
  Double-click `acc-a` in the list. In the **Unknown host key** box click
  `[y] Trust and save`.

  Expected: a single click only selects; the double-click starts the
  connection; the TOFU box's buttons act like their keys. Tab
  `1 acc-a` opens focused, shown in brackets, with the stub shell's `$ `
  prompt between the header and a status bar reading
  `● connected · alice@127.0.0.1:2201 · 00:00:0x · Ctrl+] ? help · Ctrl+] h hosts`;
  the duration counts up. — Pass [ ] Fail [ ]

- [ ] **M2.2.2 Second sshtestd tab.** `ctrl+]` `h`, select `acc-c`,
  `enter`, `y` at the host-key box.

  Expected: tab `2 acc-c` opens; tab 1 is still in the bar.
  — Pass [ ] Fail [ ]

- [ ] **M2.2.3 Real host tab.** `ctrl+]` `h`, `enter` on `acc-real`,
  `y` at the host-key box if asked, and the password if prompted.

  Expected: tab `3 acc-real` with your remote shell prompt.
  — Pass [ ] Fail [ ]

- [ ] **M2.2.4 Same host twice.** `ctrl+]` `h`, `enter` on `acc-a`.

  Expected: tab `4 acc-a` opens with no host-key box; tab 1 is
  unaffected. — Pass [ ] Fail [ ]

- [ ] **M2.2.5 Switching with the prefix.** Press, in order: `ctrl+]` `1`,
  `ctrl+]` `3`, `ctrl+]` `n`, `ctrl+]` `n`, `ctrl+]` `p`, `ctrl+]` `0`.

  Expected: tab 1, tab 3, tab 4, Hosts (wraps), tab 4, Hosts; each tab
  shows what it showed before. — Pass [ ] Fail [ ]

- [ ] **M2.2.6 Click tabs on sshelf's screens.** On the Hosts tab click
  `3 acc-real` in the header.

  Expected: tab 3 is focused. — Pass [ ] Fail [ ]

- [ ] **M2.2.7 Activity marker.** In tab 3 run `sleep 3; echo done`, then
  at once `ctrl+]` `1`.

  Expected: after about 3 s the header shows `3 acc-real ●`; `ctrl+]` `3`
  clears the marker and shows `done`. — Pass [ ] Fail [ ]

- [ ] **M2.2.8 Help overlay.** `ctrl+]` `?`, then `esc`.

  Expected: an overlay lists every prefix command above; `esc` closes it
  and nothing was sent to the remote. — Pass [ ] Fail [ ]

- [ ] **M2.2.9 Unknown prefix key.** In tab 3 press `ctrl+]` `z`, then
  type `echo ok` and `enter`.

  Expected: `z` cancels the prefix and is not sent; the shell prints `ok`.
  — Pass [ ] Fail [ ]

## M2.3 Shell, colours and keys (tab 3, real host)

- [ ] **M2.3.1 TERM and size.** In tab 3:

  ```sh
  echo "$TERM"; tput cols; tput lines
  ```

  Expected: `xterm-256color`, your window's width, and its height minus 2
  (the header and the status bar). — Pass [ ] Fail [ ]

- [ ] **M2.3.2 Colours.** In tab 3:

  ```sh
  printf '\033[31mred\033[0m \033[1;32mbold-green\033[0m \033[38;5;208morange-256\033[0m \033[38;2;0;128;255mtruecolor-blue\033[0m \033[7mreverse\033[0m\n'
  ```

  Expected: each word in its colour or style; the rest of the line and
  the next prompt are not coloured. — Pass [ ] Fail [ ]

- [ ] **M2.3.3 Wide characters.** In tab 3:

  ```sh
  echo 'wide: 日本語 テスト 👍 end'
  printf '%*s日本\n' $(( $(tput cols) - 1 )) ''
  ```

  Expected: the first line is aligned (each wide character takes two
  columns, `end` is not overwritten); in the second, `日` does not fit in
  the last column, so `日本` starts at the start of the next line and no
  character is lost (D-015). — Pass [ ] Fail [ ]

- [ ] **M2.3.4 Line editing and modified keys.** Type
  `echo one two three` (do not press `enter`), then `ctrl+←` twice,
  `alt+b`, `ctrl+a`, `ctrl+e`, `ctrl+w`, then `ctrl+c`.

  Expected: word movement and deletion act as in your terminal without
  sshelf; `ctrl+c` interrupts only the remote line (sshelf keeps
  running). If `ctrl+←` or `alt+b` print `;5D` or `b`, set your terminal
  to send Option as Meta and note it. — Pass [ ] Fail [ ]

- [ ] **M2.3.5 sshelf's own keys go to the remote.** In tab 3 type
  `cat -v`, `enter`, then `q`, `x`, `r`, `ctrl+]` `ctrl+]`, `enter`, and
  finally `ctrl+d`.

  Expected: `cat` echoes `qxr^]`; none of them quits, closes or
  reconnects anything. — Pass [ ] Fail [ ]

- [ ] **M2.3.6 Paste.** In **side**, `printf 'line one\n    indented\nline three\n' | pbcopy`.
  In tab 3 run `cat`, paste with ⌘V, then `ctrl+d`.

  Expected: the three lines arrive intact, indentation kept.
  — Pass [ ] Fail [ ]

## M2.4 vim, htop, less and tmux inside the frame (tab 3)

- [ ] **M2.4.1 vim: colours, alternate screen, keys.** In tab 3:

  ```sh
  printf 'first\nsecond\nthird\n' > /tmp/sshelf-acc.txt; vim -c 'syntax on' -c 'set number' /tmp/sshelf-acc.txt
  ```

  Then `j`, `j`, `A`, type ` line`, `esc`, `:wq`.

  Expected: vim fills the area between the header and the status bar
  (both stay in place), line numbers and colours show, the cursor is on
  the right cell, `esc` acts at once, and after `:wq` the shell screen from
  before vim is back with no vim leftovers (alternate screen).
  — Pass [ ] Fail [ ]

- [ ] **M2.4.2 vim: resize.** Open `vim /tmp/sshelf-acc.txt` again, make
  the window narrower and shorter by dragging its corner, then wider again.

  Expected: vim redraws to the new size each time; the header and status
  bar stay one line each; nothing is drawn outside the frame.
  — Pass [ ] Fail [ ]

- [ ] **M2.4.3 vim: mouse.** In the same vim: `:set mouse=a`, `enter`.
  Click on the `t` of `third`; scroll the wheel; drag to select a few
  characters (vim shows `-- VISUAL --`); `esc`; click the header row;
  `:q!`.

  Expected: the cursor lands exactly on the clicked character (not one
  row off); the wheel scrolls; dragging selects in vim's visual mode;
  clicking the header does nothing (it is neither a vim click nor a tab
  switch while vim has the mouse). — Pass [ ] Fail [ ]

- [ ] **M2.4.4 htop: colours, resize, mouse.** Run `htop`. Resize the
  window once. Click a process row, then click the `CPU%` column header,
  then `q`.

  Expected: meters and colours render; htop redraws after the resize; the
  click selects the clicked row and the header click sorts by CPU%; `q`
  returns to a clean shell screen. — Pass [ ] Fail [ ]

- [ ] **M2.4.5 less.** `less /etc/services`, then `/tcp`, `enter`, `n`,
  `G`, `q`.

  Expected: the search highlight and less's status line render; `q`
  returns to a clean prompt. — Pass [ ] Fail [ ]

- [ ] **M2.4.6 tmux: prefix, panes, resize.** Run
  `tmux new -s acc`, then `ctrl+b` `%`, type `echo left` in one pane,
  `ctrl+b` `→`, `echo right`, and resize the window once.

  Expected: tmux's `ctrl+b` works and does not clash with sshelf's
  `ctrl+]`; both panes and tmux's green status line render and follow the
  resize. — Pass [ ] Fail [ ]

- [ ] **M2.4.7 tmux: mouse.** `ctrl+b` `:`, type `set -g mouse on`,
  `enter`. Click the other pane; scroll the wheel up in it; press `q`;
  then `exit` in both panes.

  Expected: the click focuses the clicked pane; the wheel enters tmux's
  copy mode and scrolls; after the last `exit` the shell prompt is clean.
  — Pass [ ] Fail [ ]

- [ ] **M2.4.8 Mouse is native again.** At the plain prompt of tab 3,
  drag over some text.

  Expected: your terminal selects the text itself (highlight from your
  terminal emulator, no clicks reach the remote). — Pass [ ] Fail [ ]

## M2.5 Text selection and copy in a plain shell (no mouse capture)

- [ ] **M2.5.1 Select and copy.** In tab 3 run `echo copy-me-12345`.
  Double-click `copy-me-12345` (or drag over it) and copy with ⌘C. In
  **side**: `pbpaste; echo`.

  Expected: `copy-me-12345`. — Pass [ ] Fail [ ]

- [ ] **M2.5.2 Same in an sshtestd tab.** `ctrl+]` `1`, type
  `echo stub-copy-678`, `enter`, select and copy `stub-copy-678`, then
  `pbpaste; echo` in **side**.

  Expected: `stub-copy-678`. — Pass [ ] Fail [ ]

- [ ] **M2.5.3 Mouse clicks do not reach sshelf in a plain shell.** In
  tab 1, click on the header and on the status bar.

  Expected: nothing changes in sshelf (tabs are switched with the
  keyboard while a session is focused, D-012); your terminal may start a
  selection. — Pass [ ] Fail [ ]

## M2.6 Resize with background tabs

- [ ] **M2.6.1 Every session gets the new size.** Focus tab 3. Resize
  the window to a clearly different size. Run `tput cols; tput lines` in
  tab 3; then `ctrl+]` `1` and type `size`, `enter`; then `ctrl+]` `2`,
  `size`, `enter`.

  Expected: all three report the same new session area: the window's
  width, and its height minus 2 (sshtestd prints `<cols>x<rows>`), even
  though tabs 1 and 2 were in the background during the resize.
  — Pass [ ] Fail [ ]

## M2.7 Scrollback mode

- [ ] **M2.7.1 Scroll back and return.** In tab 3 run `seq 1 3000`, then
  `ctrl+]` `[`. Press `↑`, `PgUp` three times, `g`, `G`, `PgUp`, `esc`.

  Expected: the status bar shows `scrollback <line>/<total>` with the key
  hints; `↑`/`PgUp` move up; `g` reaches the oldest kept line (a number
  around 1000: only the last 2,000 lines plus the screen are kept, D-013);
  `G` the end; `esc` returns to the live prompt. — Pass [ ] Fail [ ]

- [ ] **M2.7.2 The view stays put while output arrives.** In tab 3 run:

  ```sh
  for i in $(seq 1 6000); do echo "line $i"; sleep 0.002; done
  ```

  While it runs, `ctrl+]` `[`, `PgUp` twice, and watch for 5 seconds;
  then `esc`.

  Expected: in scrollback mode the visible lines do not move while new
  lines arrive and old ones are dropped at the 2,000-line cap (the
  `<line>/<total>` numbers may change, the text on screen does not);
  `esc` shows the live end of the output. — Pass [ ] Fail [ ]

- [ ] **M2.7.3 Copy from scrollback.** `ctrl+]` `[`, `PgUp`, select a
  few lines with the mouse, ⌘C, `esc`; in **side** `pbpaste`.

  Expected: your terminal selects normally (sshelf turns the mouse off in
  scrollback mode); the pasted text is those lines. — Pass [ ] Fail [ ]

## M2.8 Hostile output does not reach your terminal

- [ ] **M2.8.1 Clipboard write (OSC 52).** In **side**:
  `printf 'clipboard-before-osc52' | pbcopy`. In tab 3:

  ```sh
  printf '\033]52;c;SGVsbG8=\007'; echo osc52-sent
  ```

  Then in **side**: `pbpaste; echo`.

  Expected: tab 3 shows `osc52-sent`; `pbpaste` prints
  `clipboard-before-osc52`, **not** `Hello`. — Pass [ ] Fail [ ]

- [ ] **M2.8.2 Title, link and reset.** In tab 3:

  ```sh
  printf '\033]0;HACKED-TITLE\007\033]8;;https://example.invalid\007link\033]8;;\007 \033[21t\033c'; echo after-reset
  ```

  Expected: your window and tab titles do not change to `HACKED-TITLE`;
  `link` is plain text, not clickable; the remote screen may clear (the
  emulator's own reset) but sshelf's header and status bar stay and your
  terminal is not reset; `after-reset` is shown. The tab still reads
  `3 acc-real`. — Pass [ ] Fail [ ]

## M2.9 Disconnect, ended tabs and reconnect

- [ ] **M2.9.1 Remote exit.** `ctrl+]` `1` and type `exit 3`.

  Expected: the last screen stays; the status bar reads
  `✕ exited (3) · r reconnect · x close`; the tab reads `1 acc-a ✕`;
  typing letters other than `r` and `x` does nothing. — Pass [ ] Fail [ ]

- [ ] **M2.9.2 Reconnect.** Press `r`.

  Expected: a new session to `acc-a` opens in the same tab 1 with no
  prompts. — Pass [ ] Fail [ ]

- [ ] **M2.9.3 Server dropped while in the background.** Focus tab 3.
  In **A**, stop sshtestd with `ctrl+c`.

  Expected: tabs 1 and 4 (both `acc-a`) show `✕` in the header without
  taking the focus; tab 3 is unaffected. `ctrl+]` `1` shows
  `✕ connection lost: …` with a one-line reason. — Pass [ ] Fail [ ]

- [ ] **M2.9.4 Reconnect fails, then works.** In tab 1 press `r` while
  A is still down.

  Expected: the tab stays ended and the status bar shows the failure
  (connection refused). Now restart A in **A** with the M2.0.2 command
  and press `r` again: tab 1 reconnects. — Pass [ ] Fail [ ]

- [ ] **M2.9.5 Disconnect with the prefix.** `ctrl+]` `2`, then
  `ctrl+]` `q`, then `n`; then `ctrl+]` `q`, `y`.

  Expected: "Disconnect acc-c?"; `n` keeps the session; `y` closes tab 2
  and focuses the tab to its left. — Pass [ ] Fail [ ]

- [ ] **M2.9.6 Close an ended tab.** `ctrl+]` `4`, press `x`.

  Expected: tab 4 (ended in M2.9.3) closes and the focus moves to the
  tab on its left. — Pass [ ] Fail [ ]

- [ ] **M2.9.7 Close back to the list.** `ctrl+]` `1`, `exit`, `x`.

  Expected: if the Hosts tab is focused as a result, `acc-a` is the
  selected host. — Pass [ ] Fail [ ]

## M2.10 Modals and search with the mouse

- [ ] **M2.10.1 Delete prompt buttons.** On the Hosts tab select `acc-c`,
  press `d`, click `[n] No`; press `d` again and click outside the box.

  Expected: `[n] No` closes the box and keeps the host; a click outside
  the box does nothing. Press `esc` to close it. — Pass [ ] Fail [ ]

- [ ] **M2.10.2 Search and wheel.** Click the search field, type `acc`,
  then click a host in the list; scroll the wheel over the list.

  Expected: the click focuses search and the list filters; clicking the
  list leaves search; the wheel moves the selection. — Pass [ ] Fail [ ]

## M2.11 Quit confirmation and terminal state

- [ ] **M2.11.1 Quit with live sessions.** With at least tab 3 still
  live, focus the Hosts tab and press `q`, then `esc`; press `q`, then
  `y`.

  Expected: "Quit and close N sessions?" with the number of live tabs
  (ended tabs do not count); `esc` keeps everything; `y` quits.
  — Pass [ ] Fail [ ]

- [ ] **M2.11.2 Terminal restored.** In **main**:

  ```sh
  stty -a | diff - "$SSHELF_ACC/stty-before" && echo "terminal restored"
  ```

  Expected: `terminal restored`; clicking and dragging in this shell
  selects text normally and prints no escape codes such as `[<0;12;5M`.
  — Pass [ ] Fail [ ]

- [ ] **M2.11.3 Quit with no live session.** Start sshelf again, press
  `q`.

  Expected: sshelf quits at once, no question. — Pass [ ] Fail [ ]

## M2.12 Other terminals

- [ ] **M2.12.1 Named terminals.** Repeat M2.2.1, M2.3.3, M2.3.4, M2.4.3,
  M2.4.4, M2.5.1 and M2.8.1 in each terminal you have: Terminal.app,
  iTerm2, kitty, WezTerm, Alacritty, Ghostty, and inside a local tmux.

  Expected: the same results. Note which were tried and any difference:
  ______________ — Pass [ ] Fail [ ]

## M2.13 Linux run

Repeat M2.0 to M2.11 on Linux with these changes: replace `pbcopy` with
`xclip -sel clip` (or `wl-copy`) and `pbpaste` with `xclip -sel clip -o`
(or `wl-paste`); start the real `sshd` with `sudo systemctl start ssh`;
copy with ctrl+shift+C. Repeat M2.12 in GNOME Terminal, Konsole and xterm.

- [ ] **M2.13.1 Linux run done.** — Pass [ ] Fail [ ] — N/A [ ]

## M2.14 Teardown

- [ ] **M2.14.1 Delete the test hosts and their Keychain items.** In
  **side**, record the IDs this run created:

  ```sh
  grep -o '"id": "[^"]*"' "$CFG/config.json" | cut -d'"' -f4 > "$SSHELF_ACC/ids"; wc -l < "$SSHELF_ACC/ids"
  ```

  In **main**, `"$SSHELF_ACC/sshelf"`; for each of `acc-a`, `acc-c` and
  `acc-real` select it, press `d`, then `y`; then `q`. In **side**:

  ```sh
  for id in $(cat "$SSHELF_ACC/ids"); do for f in password private-key key-passphrase; do security find-generic-password -s sshelf -a "$id/$f" >/dev/null 2>&1 && echo "left: $id/$f"; done; done; echo checked
  ```

  Expected: each delete reports `<label>: deleted`; the loop prints only
  `checked` (no `left:` line). — Pass [ ] Fail [ ]

- [ ] **M2.14.2 Remove the scratch files.** Stop A and C with `ctrl+c`.
  In **side**:

  ```sh
  rm -rf "$SSHELF_ACC" /tmp/sshelf-acc.txt; echo removed
  ```

  Then turn Remote Login off again if you turned it on in M2.0.4.

  Expected: `removed`. — Pass [ ] Fail [ ]

## M2 sign-off

| | macOS | Linux |
|---|---|---|
| All M2 steps pass (or N/A with reason) | [ ] | [ ] |
| Failures (step numbers, issue links) | | |
| Signed off by | | |
