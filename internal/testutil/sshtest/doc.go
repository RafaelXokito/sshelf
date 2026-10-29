// Package sshtest is an in-process SSH server for tests (ARCH-TEST-001).
//
// It listens on 127.0.0.1 only and supports password, public-key and scripted
// keyboard-interactive auth, session channels with pty-req, window-change,
// env, shell and exec requests, and a scripted shell that understands
// "echo …", "size" and "exit N". It records the client version, every auth
// attempt in order and every request, and can drop its connections on demand.
// cmd/sshtestd runs it as a standalone server for demos.
package sshtest
