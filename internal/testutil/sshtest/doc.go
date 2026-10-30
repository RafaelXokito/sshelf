// Package sshtest is an in-process SSH server for tests (ARCH-TEST-001).
//
// It listens on 127.0.0.1 only and supports password, public-key and scripted
// keyboard-interactive auth, session channels with pty-req, window-change,
// env, shell and exec requests, and a scripted shell that understands
// "echo …", "size" and "exit N" and the M2 commands flood, stream, replay,
// modes, query, hostile and echo-hex (scripts.go). It records the client
// version, every auth attempt in order, every request, every byte a
// handler reads and every channel and connection close, and can drop its
// connections on demand.
//
// M3 (ARCH-TEST-004, sftp.go): WithSFTP serves the sftp subsystem from a
// directory through pkg/sftp's request server over an os.Root, with the
// hostile mode, fault injection, virtual zero files, WithoutSFTP,
// WithMaxSessions, per-file byte counters and the open-handle count.
// cmd/sshtestd runs it as a standalone server for demos.
package sshtest
