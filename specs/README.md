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
| `tasks.md` | Milestones and implementation tasks (`T-*`), traceability matrix, manual-only requirements |
| `acceptance.md` | Manual acceptance checklists (MVP, M2, M3) for macOS and Linux |
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

- **Priority** – `MVP` (needed for first usable release), `M2` (milestone M2:
  framed sessions, tabs and mouse, D-009 to D-012), `M3` (milestone M3: the
  SFTP tab, shared connections, transfers and edit-locally, D-017 to D-020),
  `v1` (first public release), `Later` (backlog). In order: `MVP` → `M2` →
  `M3` → `v1` → `Later`.
- **Acceptance criteria** – concrete, verifiable checks (Given/When/Then or bullets).
- **Rationale / source** – optional; e.g. "Termius parity", "threat T-3".

**Status values:** `Draft` → `Approved` → `Implemented` → `Verified`.

**Revisions of implemented requirements.** When a later milestone changes the
behaviour of a requirement that is already `Implemented`, the requirement keeps
its ID, priority line and status, and gets a revision block right after its
acceptance criteria:

> - **M2 revision** · Priority: M2 · Status: Draft · Source: D-009
>   - *Revised:* the new EARS sentence.
>   - *Acceptance:* the new criteria, which replace the old ones.

The original text stays authoritative for the shipped code until the
milestone's tasks implement the revision; they set the revision's status to
`Implemented`. The milestone's acceptance gate then folds each revision into
the requirement (new sentence, new criteria, `Priority: M2`) and removes the
block. This keeps `scripts/tracecheck` green on `main` while the spec runs
ahead of the code.

The same rule applies to every later milestone with its own name: M3 uses
**M3 revision** blocks (`Priority: M3`), folded in by the M3 gate.

## Scope boundaries

- Target platforms: macOS and Linux first; Windows is `Later`.
- Single-user, local-first. Cloud sync is out of scope for v1. Users can sync the
  config directory themselves (Git, Syncthing, iCloud); secrets stay in each
  machine's OS credential store and do not sync (D-108).
- Language/stack: Go, Bubble Tea, Lip Gloss, Bubbles (v2, D-109), `golang.org/x/crypto/ssh`,
  `zalando/go-keyring` (D-008).
