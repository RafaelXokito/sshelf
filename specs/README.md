# sshelf specs

sshelf is built spec-first: every feature is described here before it is
implemented, and code changes reference the requirement IDs they satisfy.

## Workflow

1. **Requirements** – what the system must do, written as testable statements.
2. **Design** – how it will be done: architecture, data model, formats, flows.
3. **Tasks** – an ordered implementation plan; each task links to requirement IDs
   and lists its acceptance tests.
4. **Implement** – one task per branch/PR. The PR description lists the IDs it
   covers. Tests are named after IDs where practical (`TestSEC_CRED_004_...`).
5. **Update the spec first** when behaviour changes. Code and spec never drift.

## Layout

| File | Owns |
|------|------|
| `product/requirements.md` | Feature catalogue and functional requirements (`FR-*`) |
| `product/research.md` | Termius / competitor analysis that informed the requirements |
| `security/requirements.md` | Security requirements (`SEC-*`) and threat model |
| `security/credential-storage.md` | `config.json` format, OS credential store (keychain) library and item naming, `secrets` API |
| `architecture/design.md` | Package layout, data model, UI state machine, SSH session flow (`ARCH-*`) |
| `nfr.md` | Non-functional requirements (`NFR-*`): performance, portability, UX, quality |
| `tasks.md` | Milestones and implementation tasks (`T-*`) |
| `glossary.md` | Shared terms |
| `decisions.md` | Binding product-owner decisions (`D-*`); wins any conflict |

## Conventions

**Requirement IDs.** `<PREFIX>-<AREA>-<NNN>`, e.g. `FR-HOST-001`, `SEC-CRED-004`.
IDs are never reused or renumbered. Once a requirement is `Approved`, retiring it
marks it `Deprecated`; a `Draft` requirement is simply deleted.

**Requirement syntax (EARS).** Each requirement is one sentence using one of:

- Ubiquitous: *The system shall …*
- Event: *When <trigger>, the system shall …*
- State: *While <state>, the system shall …*
- Unwanted: *If <condition>, then the system shall …*
- Optional: *Where <feature is enabled>, the system shall …*

**Each requirement has:**

- **Priority** – `MVP` (needed for first usable release), `v1` (first public
  release), `Later` (backlog).
- **Acceptance criteria** – concrete, verifiable checks (Given/When/Then or bullets).
- **Rationale / source** – optional; e.g. "Termius parity", "threat T-3".

**Status values:** `Draft` → `Approved` → `Implemented` → `Verified`.

## Scope boundaries

- Target platforms: macOS and Linux first; Windows is `Later`.
- Single-user, local-first. Cloud sync is out of scope for v1. Users can sync the
  config directory themselves (Git, Syncthing, iCloud); secrets stay in each
  machine's OS credential store and do not sync (D-108).
- Language/stack: Go, Bubble Tea, Lip Gloss, Bubbles (v2, D-109), `golang.org/x/crypto/ssh`,
  `zalando/go-keyring` (D-008).
