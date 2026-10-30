# sshelf: credential storage design

Status: **Draft** · Owner: security · Last updated: 2026-10-29
Implements: SEC-CRED-*, SEC-FILE-*, SEC-MEM-*, SEC-HOSTKEY-002 (see [`requirements.md`](requirements.md)).
Binding inputs: D-002, D-003, D-004, D-007, D-008, D-101, D-102, D-106 and D-108 in [`../decisions.md`](../decisions.md).

## 1. Model in one paragraph

There is no master password and no encrypted vault. Host metadata goes in `config.json`, a plain file with mode
0600 that never contains a secret. Each secret (password, private key or key passphrase) is one item in the OS
credential store, keyed by the identity's UUID. sshelf reads a secret only when a connection needs it. If no store
is available, sshelf works for key-file and agent hosts and refuses to save secrets (D-101).

---

## 2. Credential-store library

### 2.1 Comparison

| | **zalando/go-keyring** | **99designs/keyring** |
|---|---|---|
| macOS mechanism | Runs `/usr/bin/security`. `Set` writes `add-generic-password -U -s … -a … -w …` to `security -i` over **stdin**, so the secret is not in argv. `Get` reads `find-generic-password -w` from stdout. ([source](https://github.com/zalando/go-keyring/blob/master/keyring_darwin.go)) | Security.framework through `github.com/99designs/go-keychain`. The backend file is `//go:build darwin && cgo`. ([source](https://github.com/99designs/keyring/blob/master/keychain.go)) |
| cgo | **No** on all platforms. Static, cross-compilable binaries. | **Yes on macOS.** No Keychain backend without cgo. Cross-compiling darwin from Linux needs a macOS SDK toolchain. |
| macOS item ACL | The creating app is `/usr/bin/security`, so it is the trusted app. Any same-user process that runs `security find-generic-password` reads the item **without a prompt**. | With `KeychainTrustApplication`, the trusted app is the sshelf binary. Other apps get an "allow access" prompt. This is closer to "only sshelf can read it". |
| Prompts after rebuild or update | None (the ACL is bound to `security`, which never changes). | An unsigned or ad-hoc-signed binary's identity changes with every build, so macOS re-prompts after each update. Stable only with Developer ID signing (a stable designated requirement). |
| Linux | Secret Service over D-Bus (pure Go). Works with GNOME Keyring, and with KWallet when it exposes the Secret Service API. | Secret Service, KWallet (native D-Bus), `pass`, `keyctl`, and an encrypted-file backend that needs a password. |
| Size limit | macOS: the whole `security -i` command must fit in **4096 bytes**, otherwise `ErrSetDataTooBig`. The value is stored as `go-keyring-base64:<b64>`. | No practical limit. |
| API | `Set(service, user, password string)`, `Get(...) (string, error)`, `Delete`, `DeleteAll(service)`, `ErrNotFound`, and a built-in mock (`MockInit`). Secrets are passed as `string`. | `Open(Config)` → `Keyring{Get,Set,Remove,Keys}`. `Item.Data` is `[]byte`. |
| Maintenance | Actively used (for example by GitHub CLI `gh`). Small surface. | Releases have been infrequent (latest tag v1.2.2). Many backends. |
| Fit with D-007 | Small API and no cgo. | Broader than we need. cgo and signing work. |

### 2.2 Decision: zalando/go-keyring (D-008)

Use **`github.com/zalando/go-keyring`**, pinned to an exact version and reviewed on each bump, behind sshelf's own
`secrets.Store` interface (§5).

Reasons: no cgo (one `go build` and goreleaser matrix for macOS and Linux, amd64 and arm64), no Keychain re-prompts
after updates (SEC-CRED-009), a tiny API, and a mock that is easy to replace with our fake.

What we accept, stated plainly:

1. **macOS:** any process running as the user can read sshelf items through `/usr/bin/security` without a prompt
   (T-3). The Keychain still protects the items at rest (they are encrypted under the login password) and from
   other OS users.
2. **macOS 4 KiB limit.** We enforce a uniform **2800-byte** maximum secret on every OS (SEC-CRED-003). base64
   adds about 33 % and the command adds about 150 bytes of overhead, so 2800 bytes stays under 4096. Ed25519,
   ECDSA and RSA ≤ 3072 OpenSSH keys fit. RSA-4096 keys (about 3.4 KB) must be referenced as key files.
3. **`string` API.** One immutable copy of each secret per `Get` or `Set` call. This is accepted (§6).

Upgrade path (`Later` per D-008): a `darwin && cgo` backend behind the same interface, enabled only once releases
are Developer ID-signed. It would give per-app ACLs, at the cost of cgo and the signing work.

### 2.3 Touch ID and user presence on macOS

Not achievable for sshelf as shipped. Keychain items protected by `SecAccessControl` user presence or biometry
must live in the **data protection keychain**. Using that keychain needs a keychain-access-group or app-identifier
entitlement authorized by a provisioning profile. A bare command-line binary cannot embed a provisioning profile,
so Apple DTS advises wrapping it in an app-like bundle
([Apple forums 684453](https://developer.apple.com/forums/thread/684453),
[696431 "On Mac Keychains"](https://developer.apple.com/forums/thread/696431),
[836816](https://developer.apple.com/forums/thread/836816)). That needs cgo, signing and a bundle, which are all
excluded by D-007 for now. Calling LocalAuthentication (`LAContext`) through cgo before reading an item would only
be a UI gate: other processes could still read the item directly, so it is not real access control. **Not
planned** (D-008). Gating comes from the OS login and the keychain being unlocked, as D-003 states.

### 2.4 Linux specifics

- go-keyring talks to `org.freedesktop.secrets` on the **session** bus. With no session bus or no provider (SSH
  login into a headless box, minimal WM, containers), calls fail. The adapter maps that to
  `secrets.ErrUnavailable` (D-101, SEC-CRED-005).
- If the default collection is locked, the provider shows its own unlock prompt. If the user cancels, the adapter
  returns `secrets.ErrDenied` (SEC-CRED-006).
- An unlocked collection is readable by every process of the user over D-Bus. Secret Service has no per-app ACL.
  This is accepted (D-003, T-3).

---

## 3. Config file

### 3.1 Location

Following ARCH ADR-005: `$XDG_CONFIG_HOME/sshelf/` (default `~/.config/sshelf/`) on Linux, and
`~/Library/Application Support/sshelf/` on macOS (or `$XDG_CONFIG_HOME/sshelf/` if that variable is explicitly
set). `--config <dir>` overrides it (D-001). The directory is 0700 and
contains:

| File | Content | Mode |
|------|---------|------|
| `config.json` | settings, groups, identities (metadata only), hosts | 0600 |
| `known_hosts` | host keys accepted in sshelf, OpenSSH format (D-102) | 0600 |

### 3.2 Format: JSON (D-106)

- **Stdlib only** (`encoding/json`), with no extra parser dependency (D-007, T-20).
- **sshelf writes the file.** Every UI edit rewrites it, and TOML or YAML libraries do not preserve comments on a
  rewrite anyway, so their hand-editing advantage mostly disappears.
- **YAML rejected:** large parser surface, implicit typing (`no` → false, `0755` → octal), and anchors or aliases
  (billion-laughs). **TOML** is reasonable but adds a dependency for no security gain.

Decoding is tolerant of unknown fields (forward compatibility, FR-CFG-003). The file is capped at
16 MiB when read.

### 3.3 Schema (version 1)

```jsonc
{
  "version": 1,
  "settings": { "theme": "auto" },
  "groups": [{ "id": "uuid", "name": "prod", "parent_id": null }],
  "identities": [{
    "id": "9b1c…",                 // UUIDv7 (ADR-008), the keychain account prefix
    "name": "ops",
    "username": "deploy",
    "key_path": "~/.ssh/id_ed25519", // optional key-file reference (not secret)
    "has_password": true,          // flags only: tell the UI which items to expect
    "has_private_key": false,
    "has_key_passphrase": true,
    "use_agent": false             // v1
  }],
  "hosts": [{
    "id": "uuid", "label": "db 1", "hostname": "db1.example.com", "port": 22,
    "user": "", "identity_id": "9b1c…", "group_id": "uuid", "tags": ["pg"],
    "jump_host_ids": [], "forward_agent": false,
    "last_connected_at": "2026-10-30T09:00:00Z" // optional (FR-NAV-018)
  }]
}
```

`last_connected_at` is optional: the time of the host's last established
session, as RFC 3339 in UTC with whole seconds, written on each one
(FR-NAV-018, D-016). It is absent for a host never connected, including every
imported host. It was added without a version bump: it is additive, a file
without it loads as before, and an older build ignores it (tolerant decoding)
but drops it when it saves. A value that does not parse as RFC 3339 is
ignored with a warning, never refused. It is not a secret.

`domain.Host` (`internal/domain`, see `../architecture/design.md` §7) maps onto `hosts[]`. The `has_*` flags are hints, not proof: the store is
the source of truth, and a missing item means "not set on this device" (SEC-CRED-007).

### 3.4 Writes

Atomic write for `config.json` and `known_hosts` (SEC-FILE-003):
`EvalSymlinks` → `OpenFile(dir/.config.json.tmp-<rand>, O_WRONLY|O_CREATE|O_EXCL, 0600)` → write → `Sync()`
(Go uses `F_FULLFSYNC` on macOS) → `Close` → `Rename` → `fsync(dir)`.
Before writing, compare the file's mtime with the value recorded at load time. On a mismatch, refuse to save and
offer to reload (SEC-FILE-005, v1; ADR-005).

---

## 4. Keychain keying (SEC-CRED-002)

| Attribute | Value |
|-----------|-------|
| service | `sshelf` (tests use `sshelf-test-<random>`) |
| account (`user`) | `<identity-uuid>/password` · `<identity-uuid>/private-key` · `<identity-uuid>/key-passphrase` |
| value | the raw secret bytes. Private keys are stored in OpenSSH PEM form exactly as pasted or imported. |

- One item per field keeps updates independent and deletes precise, and it is what `security` and Seahorse show
  users.
- UUIDs are random, so two config directories (`--config`) never collide.
- Items are not marked synchronizable. Secrets stay on the device. A synced `config.json` on another machine
  shows "not set on this device" (D-108).

---

## 5. Go API: `internal/secrets`

```go
package secrets

var (
	ErrNotFound    = errors.New("secrets: not found")
	ErrUnavailable = errors.New("secrets: no credential store available")
	ErrDenied      = errors.New("secrets: access denied or cancelled")
	ErrTooLarge    = errors.New("secrets: secret exceeds 2800 bytes")
)

const MaxSecretSize = 2800 // SEC-CRED-003

// ErrTooLarge's text names only the limit. The "reference the key file instead"
// hint of SEC-CRED-003 is the UI message app.MsgKeyTooLarge, shown for an
// oversized private key: the sentinel also covers passwords and passphrases,
// for which a key file is no alternative.

type Field string

const (
	FieldPassword      Field = "password"
	FieldPrivateKey    Field = "private-key"
	FieldKeyPassphrase Field = "key-passphrase"
)

type Ref struct {
	IdentityID string
	Field      Field
}
func (r Ref) Account() string // "<id>/<field>"
func ParseRef(account string) (Ref, error)

// Secret holds secret bytes; it never prints its contents.
type Secret struct{ /* b []byte */ }
func NewSecret(b []byte) Secret       // takes ownership of b
func (s Secret) Bytes() []byte        // borrowed; valid until Destroy
func (s Secret) Len() int
func (s *Secret) Destroy()            // zeroes
func (Secret) String() string         // "[REDACTED]"
func (Secret) GoString() string
func (Secret) Format(fmt.State, rune)
func (Secret) MarshalJSON() ([]byte, error)
func (Secret) LogValue() slog.Value

// Store is the only path to the OS credential store.
type Store interface {
	Get(ctx context.Context, ref Ref) (Secret, error)      // ErrNotFound, ErrDenied, ErrUnavailable
	Set(ctx context.Context, ref Ref, s Secret) error       // ErrTooLarge, ErrDenied, ErrUnavailable
	Delete(ctx context.Context, ref Ref) error              // nil if already absent
	Available(ctx context.Context) error                    // cheap probe at startup
}

func NewOSStore(service string) Store            // zalando/go-keyring adapter
func NewMemStore() *MemStore                     // fake for tests

type MemStore struct{ /* map + injectable failures + call counters */ }
func (m *MemStore) FailNext(op string, err error) // "get" | "set" | "delete" | "available"
func (m *MemStore) Calls(op string) int
func (m *MemStore) Keys() []Ref
```

The adapter maps go-keyring and D-Bus errors to the four sentinel errors: `keyring.ErrNotFound` → `ErrNotFound`,
`ErrSetDataTooBig` → `ErrTooLarge`, a missing bus or service → `ErrUnavailable`, and a cancelled prompt or a
user-canceled `security` exit status → `ErrDenied`. Contexts bound each call with a timeout (default 30 s, to
allow for an OS prompt). go-keyring itself has no context support, so the adapter runs it in a goroutine and
abandons the call on timeout.

---

## 6. Secrets in memory

- `secrets.Secret` wraps `[]byte` and zeroes on `Destroy`. The go-keyring boundary is `string`-based, so the
  adapter converts once and the `string` copy is left for the GC. This is accepted, because Go cannot wipe
  strings.
- Secrets are fetched just in time (SEC-MEM-009). The SSH layer parses keys into an `ssh.Signer` per connection
  attempt and drops it after authentication. `big.Int` and `ed25519.PrivateKey` internals cannot be wiped
  reliably, which is also accepted.
- Startup sets `RLIMIT_CORE=0` (`syscall.Setrlimit`) and, on Linux, `PR_SET_DUMPABLE=0` (`x/sys/unix`)
  (SEC-MEM-003). No mlock, no in-memory sealing (D-007).

---

## 7. Flows

**Startup.** Load `config.json` (tolerant decode, mode fix-up SEC-FILE-002). Call `store.Available()`. If it
fails, set the "no credential store" banner state.

**Create identity.** Validate the metadata. For each secret field the user filled in: check
`len ≤ MaxSecretSize`, then `store.Set`. Set the `has_*` flags only for fields that were stored, then write
`config.json` atomically. If `Set` fails part-way, delete any items already written for this new UUID, write no
config, and show the error. With the store unavailable, the secret fields are disabled and a key-file or agent
identity can still be saved (SEC-CRED-005).

**Edit identity.** A changed field → `Set` (overwrite). A cleared field → `Delete`, and on failure show a warning naming
the orphaned item (SEC-CRED-004). An unchanged field is never read back, so the form shows only "set" or "not set" (SEC-LOG-005).

**Delete identity.** Confirm (and unlink hosts, FR-IDENT-001). Remove the identity from the config and write it.
Then `Delete` each of the three refs, showing a warning that names any item that could not be deleted
(SEC-CRED-004). Config first means a
crash leaves at worst an orphan item, never a dangling reference.

**Connect.** Resolve host → identity. Handshake. Verify the host key (SEC-HOSTKEY-*), with each hop first for jump
chains. **Only then** `Get` the needed fields (SEC-SESSION-001). On `ErrNotFound`, prompt for the value (SEC-CRED-007);
offering to save it is v1 (FR-IDENT-005). On `ErrDenied`, abort with a message (SEC-CRED-006). On `ErrUnavailable` (no store, D-101), skip a stored key and prompt for a password or passphrase for this connection only, without saving it (SEC-CRED-005, SEC-CRED-006). Build the auth methods,
authenticate, then `Destroy` the secrets and drop the signers (SEC-MEM-009). A key-file identity reads the file
(after the 0600 check, SEC-FILE-009) and gets the passphrase from the store if one is set.

**Import from `~/.ssh/config`** (D-103, ARCH-DATA-003). Copy the host metadata once. `IdentityFile` becomes an
identity with `key_path`, and no key content is copied by default. "Store key in keychain" (D-004) is a separate
per-identity action: read the file (0600 check), check the size, `Set` `private-key`, and leave the original file
untouched. `ProxyCommand`, `LocalCommand` and `Match exec` are reported as unsupported and never run.

---

## 8. Testing strategy

| Layer | Approach |
|-------|----------|
| Unit (all logic) | `secrets.MemStore`. Inject `ErrUnavailable`, `ErrDenied`, `ErrNotFound`, `ErrTooLarge` and partial failures through `FailNext`, and count calls to prove secrets are fetched just in time and never fetched before host-key verification. Tests are named after IDs (`TestSEC_CRED_004_DeleteFailureWarns`). |
| Config | Golden JSON round-trip. Tolerant decoding of unknown fields. A reflection test that the schema has no secret-typed fields (SEC-CRED-001). An `fs` fake with failure injection at each atomic-write step (SEC-FILE-003). `umask 000` mode test. |
| Canary scan | End-to-end with `MemStore` and an in-process `x/crypto/ssh` test server: canary secrets never appear in files, logs, stdout, stderr or the environment (SEC-CRED-001, SEC-LOG-001). `archtest` bans `os/exec` and clipboard packages (SEC-MEM-008, SEC-CLIP-005). |
| SSH | In-process server fixtures for TOFU, mismatch, `@revoked`, algorithm pinning, jump hosts, and keyboard-interactive (SEC-HOSTKEY-*, SEC-SESSION-*). |
| Fuzz | `FuzzSanitize`, `FuzzParseRef`, `FuzzLoadConfig`. |
| OS-store integration | Build tag `integration_keyring`, run in CI only. **macOS runner:** `security create-keychain -p x ci.keychain`, `security default-keychain -s ci.keychain`, `security unlock-keychain -p x ci.keychain`, then set, get and delete through `NewOSStore("sshelf-test-<rand>")`, plus a 2800/2801-byte boundary test. **Linux runner:** `dbus-run-session -- sh -c 'echo x \| gnome-keyring-daemon --unlock --components=secrets; go test -tags integration_keyring ./internal/secrets'`, plus a run with no session bus asserting `ErrUnavailable`. The user's real login keychain is never touched. |
| Manual release check | SEC-CRED-009: after upgrading an unsigned build on macOS, no Keychain dialog appears. |
