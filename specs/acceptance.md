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

  Expected: a `$ ` prompt from the stub shell. `grep 2201 "$CFG/known_hosts"`
  in another terminal shows one line. — Pass [ ] Fail [ ]

- [ ] **5.3 Exit back to the list.** In the session type `exit 3`.

  Expected: the host list comes back at once, `acc-keyfile` still selected,
  status `acc-keyfile: exited (3)`. The first key you press afterwards
  (e.g. `j`) is handled by the list. — Pass [ ] Fail [ ]

- [ ] **5.4 No second prompt.** `enter` on `acc-keyfile`, then `exit`.

  Expected: no host-key prompt this time; `acc-keyfile: exited (0)`.
  — Pass [ ] Fail [ ]

- [ ] **5.5 Password, stored key and pasted key connect.** Connect to
  `acc-password`, `acc-stored` and `acc-pasted` in turn (each `exit`).

  Expected: each opens the stub shell without asking for anything (the
  host key of port 2201 is already trusted) and returns with `exited (0)`.
  — Pass [ ] Fail [ ]

- [ ] **5.6 Fuzzy search.** Press `/` and type `akf`.

  Expected: the list filters to `acc-keyfile` (matched as you type, no
  noticeable lag); `enter` connects to it (`exit` again); `esc` clears the
  search and shows every host. — Pass [ ] Fail [ ]

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
  command again (same `-dir`, original key). Connect to `acc-password` and
  `exit`.

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

  Expected: your local `$TERM` and the current window size.
  — Pass [ ] Fail [ ]

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

- [ ] **6.8 Back to the list.** `exit`.

  Expected: the host list is redrawn cleanly, `acc-real` selected,
  `acc-real: exited (0)`. — Pass [ ] Fail [ ]

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

## 10. T-000 devcheck: raw mode and `stty` round trip (ADR-007)

- [ ] **10.1 Raw handover.**

  ```sh
  stty -a > "$SSHELF_ACC/stty-dev-before"
  go run -tags devcheck ./internal/session/devcheck
  ```

  The TUI opens in the alternate screen. Press `e`; the stub prints the
  received bytes in hex. Press `ctrl+c`, type a few keys, then `q`.

  Expected: `ctrl+c` prints `03` and does not quit; the alternate screen
  comes back and the TUI shows `stub runs: 1`. — Pass [ ] Fail [ ]

- [ ] **10.2 First keystroke after return.** Press `x` once.

  Expected: "last key since return" shows `x` on that first keystroke.
  — Pass [ ] Fail [ ]

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

Status: Draft, to run at the M2 gate · Owner: product owner · Task: T-043

This is the hands-on part of the M2 gate. It covers what only a person can
judge: how real full-screen programs look inside the frame, how keys and the
mouse feel, and that the terminal's own text selection still works. Run it
on **macOS** and **Linux** with the M2 build, in the same scratch setup as
the MVP checklist: do section 0, 1.1 and 2 above first (targets A and B and a
real `sshd`), then add `acc-password` (4.1), `acc-otp` (4.7) and `acc-real`
(6.1) if this is a fresh directory. Record the same header as above.

| | macOS | Linux |
|---|---|---|
| OS version | | |
| Terminal emulator | | |
| `sshelf --version` | | |
| Tester, date | | |

## M2.1 Frame and tabs

- [ ] **M2.1.1 Header on every screen.** Start `"$SSHELF_ACC/sshelf"`.

  Expected: the first line reads `sshelf │[Hosts]│`; the host list,
  status line and footer are below it. — Pass [ ] Fail [ ]

- [ ] **M2.1.2 First tab.** `enter` on `acc-password`.

  Expected: a new tab `1 acc-password` is focused (shown highlighted and
  in brackets); the sshtestd shell prompt fills the area between the header
  and a status bar reading `● connected · alice@127.0.0.1:2201 · 00:00:0x ·
  Ctrl+] ? help · Ctrl+] h hosts`, and the duration counts up.
  — Pass [ ] Fail [ ]

- [ ] **M2.1.3 Back to the hosts while the session runs.** `ctrl+]` then
  `h`. Select `acc-real`, `enter`.

  Expected: the host list returns with tab 1 still in the bar; a second
  tab `2 acc-real` opens with your real shell. — Pass [ ] Fail [ ]

- [ ] **M2.1.4 Same host twice.** `ctrl+]` `h`, `enter` on `acc-password`
  again.

  Expected: tab `3 acc-password` opens; tab 1 is unaffected.
  — Pass [ ] Fail [ ]

- [ ] **M2.1.5 Switching.** Try `ctrl+]` `1`, `ctrl+]` `2`, `ctrl+]` `n`,
  `ctrl+]` `p`, `ctrl+]` `0`.

  Expected: each key focuses the right tab at once, `n`/`p` wrap around
  through `Hosts`, and each tab shows what it showed before.
  — Pass [ ] Fail [ ]

- [ ] **M2.1.6 Activity marker.** In tab 2 run `sleep 3; echo done`, then
  immediately `ctrl+]` `1`.

  Expected: after about 3 s, tab 2 shows `●`; focusing it clears the
  marker and shows `done`. — Pass [ ] Fail [ ]

- [ ] **M2.1.7 Help.** `ctrl+]` `?`, then `esc`.

  Expected: an overlay lists every prefix command; `esc` closes it.
  — Pass [ ] Fail [ ]

## M2.2 Full-screen programs in the frame (real `sshd`, tab 2)

- [ ] **M2.2.1 TERM and size.** `echo "$TERM"; tput cols; tput lines`.

  Expected: `xterm-256color`, your terminal's width, and its height minus 2.
  — Pass [ ] Fail [ ]

- [ ] **M2.2.2 vim.** `vim /tmp/sshelf-acc.txt`, type `ihello`, `esc`,
  move with `h j k l`, arrows, `ctrl+←`/`ctrl+→` in insert mode, `:wq`.

  Expected: vim draws inside the frame (header and status bar stay in
  place), colours and the cursor are right, `esc` acts immediately, and the
  shell prompt is clean after `:wq`. — Pass [ ] Fail [ ]

- [ ] **M2.2.3 htop.** `htop`, arrows, `F10` or `q`.

  Expected: colours and meters render; the screen returns to the shell
  with no leftovers. — Pass [ ] Fail [ ]

- [ ] **M2.2.4 less.** `less /etc/services`, `/tcp`, `n`, `G`, `q`.

  Expected: search highlighting and the status line render; `q` returns
  to a clean prompt. — Pass [ ] Fail [ ]

- [ ] **M2.2.5 tmux.** `tmux`, `ctrl+b %`, `ctrl+b →`, type in both panes,
  `exit` both.

  Expected: tmux's prefix `ctrl+b` works and does not collide with
  sshelf's `ctrl+]`; panes and the tmux status line render.
  — Pass [ ] Fail [ ]

- [ ] **M2.2.6 Keys.** At the shell prompt type `echo one two three`, then
  `ctrl+←` twice, `alt+b`, `ctrl+a`, `ctrl+e`, `ctrl+c`; then `ctrl+]`
  `ctrl+]` followed by `cat -v` and `enter` (then `ctrl+]` `ctrl+]`,
  `enter`, `ctrl+d`).

  Expected: word movement and line editing work as in your terminal;
  `ctrl+c` interrupts only the remote; `cat -v` prints `^]`.
  — Pass [ ] Fail [ ]

- [ ] **M2.2.7 Paste.** Copy a few lines of text with your terminal, then
  paste into `cat` and into vim insert mode.

  Expected: the text arrives intact; vim does not auto-indent it
  (bracketed paste). — Pass [ ] Fail [ ]

- [ ] **M2.2.8 Resize with tabs.** Resize the window, run `tput cols;
  tput lines` in tab 2, then switch to tab 1 and run `size`.

  Expected: both show the new session area (height minus 2) without
  having been focused during the resize. — Pass [ ] Fail [ ]

- [ ] **M2.2.9 Hostile output stays in the frame.** In tab 2 run
  `printf '\033]0;HACKED\007\033]52;c;aGFja2Vk\007done\n'`.

  Expected: `done` is printed; your terminal window title does not become
  `HACKED`; your clipboard does not contain `hacked`. — Pass [ ] Fail [ ]

## M2.3 Scrollback

- [ ] **M2.3.1 Scrollback mode.** In tab 2 run `seq 1 3000`, then
  `ctrl+]` `[`, `pgup` a few times, `g`, `G`, `esc`.

  Expected: the status bar shows `scrollback …/2000`; `g` reaches line
  1001 (the oldest kept line), `G` the end; `esc` returns to the live
  prompt. — Pass [ ] Fail [ ]

- [ ] **M2.3.2 Copy from scrollback.** In scrollback mode, select a few
  numbers with the mouse and copy them with your terminal's copy shortcut.

  Expected: your terminal selects text normally (sshelf has the mouse off
  here); the copied text is the numbers. — Pass [ ] Fail [ ]

## M2.4 Mouse

- [ ] **M2.4.1 Host list.** `ctrl+]` `h`. Click a host, double-click
  `acc-password`, then go back with `ctrl+]` `h` and use the wheel over the
  list; click the search field and type `acc`.

  Expected: a click selects; a double-click opens a new tab; the wheel
  moves the selection; the click focuses search. — Pass [ ] Fail [ ]

- [ ] **M2.4.2 Tab bar.** On the Hosts tab click `2 acc-real`.

  Expected: tab 2 is focused. — Pass [ ] Fail [ ]

- [ ] **M2.4.3 Forms.** `ctrl+]` `h`, `a`. Click the Hostname field and
  type; click `›` on Identity; click `[ Cancel ]`, then `y` to discard.
  Repeat and click `[ Save ]` with an empty label.

  Expected: clicks focus fields and change the choice; `[ Cancel ]` asks
  about unsaved changes; `[ Save ]` shows the label error.
  — Pass [ ] Fail [ ]

- [ ] **M2.4.4 Modals.** Select a host, `d`, then click `[n] No`.
  Connect to an untrusted target if one is left (or delete its line from
  `$CFG/known_hosts`) and click `[enter/esc] Cancel` in the TOFU box.

  Expected: each click acts like its key; clicking outside a modal does
  nothing. — Pass [ ] Fail [ ]

- [ ] **M2.4.5 Native selection in a plain shell.** Focus tab 2 (real
  shell, no full-screen program) and select text with the mouse.

  Expected: your terminal selects and copies text as usual; clicking does
  not move anything in sshelf. — Pass [ ] Fail [ ]

- [ ] **M2.4.6 Mouse in vim and htop.** In tab 2 run `vim -c 'set
  mouse=a' /etc/hosts` and click on another line; quit; run `htop` and
  click a process row and a column header; quit.

  Expected: vim moves the cursor to the clicked cell (not one row off);
  htop selects the clicked row and sorts by the clicked column. After
  quitting, selection with the mouse works natively again (M2.4.5).
  — Pass [ ] Fail [ ]

## M2.5 Ending, reconnecting, quitting

- [ ] **M2.5.1 Ended tab.** In tab 1 (sshtestd) type `exit 3`.

  Expected: the last screen stays; the status bar shows `✕ exited (3) · r
  reconnect · x close`; the tab shows `✕`. Typing letters does nothing.
  — Pass [ ] Fail [ ]

- [ ] **M2.5.2 Reconnect.** Press `r`.

  Expected: a new session to `acc-password` opens in the same tab 1.
  — Pass [ ] Fail [ ]

- [ ] **M2.5.3 Drop in the background.** Focus tab 2, then stop sshtestd
  target A with `ctrl+c` in its terminal and restart it with the same
  command.

  Expected: tabs 1 and 3 show `✕` without stealing focus; focusing one
  shows `✕ connection lost: …`; `r` reconnects. — Pass [ ] Fail [ ]

- [ ] **M2.5.4 Disconnect.** In tab 3 press `ctrl+]` `q`, then `n`; then
  `ctrl+]` `q`, `y`.

  Expected: `n` keeps the session; `y` closes tab 3 and focuses the tab to
  its left. — Pass [ ] Fail [ ]

- [ ] **M2.5.5 Close.** End tab 1 with `exit`, press `x`.

  Expected: the tab closes. When the Hosts tab is focused as a result, the
  host is selected in the list. — Pass [ ] Fail [ ]

- [ ] **M2.5.6 Quit with sessions.** On the Hosts tab press `q`, then
  `esc`; then `q`, `y`.

  Expected: "Quit and close 1 session?" (or the current count); `esc`
  keeps everything; `y` quits. — Pass [ ] Fail [ ]

- [ ] **M2.5.7 Terminal state.** After quitting:

  ```sh
  stty -a | diff - "$SSHELF_ACC/stty-before" && echo "terminal restored"
  ```

  Expected: `terminal restored` (the file is from MVP step 3.1; run 3.1
  first if this is a fresh directory); typing and mouse selection in
  your shell behave normally; no mouse escape codes appear when you click.
  — Pass [ ] Fail [ ]

## M2.6 Other terminals

- [ ] **M2.6.1 Named terminals.** Repeat M2.1.2, M2.2.2, M2.2.6, M2.4.1 and
  M2.4.6 in each terminal you have (iTerm2, Terminal.app, kitty, WezTerm,
  Alacritty, Ghostty, GNOME Terminal, Konsole, xterm, and inside tmux).

  Expected: the same results. Note which were tried: ______________
  — Pass [ ] Fail [ ]

## M2 sign-off

| | macOS | Linux |
|---|---|---|
| All M2 steps pass (or N/A with reason) | [ ] | [ ] |
| Failures (step numbers, issue links) | | |
| Signed off by | | |
