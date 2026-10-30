package sshtest

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// The sftp subsystem (ARCH-TEST-004, ADR-016): a pkg/sftp request server
// whose handlers work on an os.Root opened on the served directory, so no
// path, through ".." or a symlink, reaches anything outside it.

// SFTPHome is the remote user's home directory, the start directory of
// every SFTP channel. It is mapped inside the served directory.
const SFTPHome = "/home/pi"

// HostileNames are the entry names of SEC-SFTP-001 AC1 that the hostile
// directory lists.
var HostileNames = []string{
	"ok.txt", "../evil", "a/b", "x/..", ".", "..", "", "x\x00y", "bell\x07",
	"esc\x1b[31m", "c1\u009b", "del\x7f", "tab\tname",
}

// HostileStatus is a status message holding an OSC 52 clipboard write, an
// OSC 0 title, a CSI, a newline and 200 extra columns (SEC-SFTP-008 AC1).
var HostileStatus = "\x1b]52;c;cHduZWQ=\x07\x1b]0;pwned\x07\x1b[31mpermission denied\nsecond line " + strings.Repeat("x", 200)

// The hostile mode's virtual entries, inside SFTPHome.
const (
	HostileDir   = SFTPHome + "/hostile"      // lists HostileNames; every path below it reads as a small file
	ManyDir      = SFTPHome + "/many"         // lists SFTPHostile.Entries files
	DeepDir      = SFTPHome + "/deep"         // a chain of SFTPHostile.Depth directories named "d"
	OversizeFile = SFTPHome + "/oversize.bin" // reports SFTPHostile.Oversize bytes and returns data past them
	DeniedFile   = SFTPHome + "/denied"       // opening or listing it fails with SFTPHostile.Status
)

// HostileContent is what every file below HostileDir contains.
const HostileContent = "hostile\n"

// SFTPHostile configures the hostile SFTP mode (ARCH-TEST-004). Zero
// fields are off.
type SFTPHostile struct {
	Names    bool   // HostileDir lists HostileNames
	Entries  int    // ManyDir lists this many files (SEC-SFTP-004: 150,000)
	NameLen  int    // the length of each name in ManyDir; at least 8
	Depth    int    // DeepDir holds this many nested directories (SEC-SFTP-004: 70)
	Oversize int64  // OversizeFile's reported size; reads never end
	Status   string // DeniedFile's open and list fail with this message
	Realpath string // realpath of "." and "" answers this instead of SFTPHome
}

// FullHostile is the hostile mode with every SEC-SFTP case on.
func FullHostile() SFTPHostile {
	return SFTPHostile{
		Names: true, Entries: 150_000, Depth: 70, Oversize: 1 << 20,
		Status: HostileStatus, Realpath: SFTPHome + "/\x1b]0;title\x07\x1b[2Jhome",
	}
}

// FaultAction is what WithSFTPFault does once its byte count is reached.
type FaultAction int

// The fault actions.
const (
	FaultDrop FaultAction = iota // drop the connection, as a network failure would
	FaultFail                    // fail the read or write request
)

// ErrInjected is the error a FaultFail fault returns to the client.
var ErrInjected = errors.New("sshtest: injected failure")

type sftpFault struct {
	after  int64
	action FaultAction
	done   int64
	fired  bool
}

type sftpConfig struct {
	dir     string
	off     bool // WithoutSFTP
	hostile SFTPHostile
	faults  map[string]*sftpFault
	zeros   map[string]int64
}

func (s *Server) sftpCfg() *sftpConfig {
	if s.sftp == nil {
		s.sftp = &sftpConfig{faults: map[string]*sftpFault{}, zeros: map[string]int64{}}
	}
	return s.sftp
}

// remotePath makes name absolute: a relative name is inside SFTPHome.
func remotePath(name string) string {
	if path.IsAbs(name) {
		return path.Clean(name)
	}
	return path.Join(SFTPHome, name)
}

// WithSFTP serves the sftp subsystem from dir: the remote "/" is dir, and
// SFTPHome (created if missing) is the start directory. Every channel's
// subsystem request is recorded, and bytes read and written are counted
// per file (SFTPBytesRead, SFTPBytesWritten).
func WithSFTP(dir string) Option {
	return func(srv *Server) error {
		if err := os.MkdirAll(dir+SFTPHome, 0o700); err != nil {
			return err
		}
		srv.sftpCfg().dir = dir
		return nil
	}
}

// WithSFTPHostile adds the hostile entries of h to the served tree.
func WithSFTPHostile(h SFTPHostile) Option {
	return func(srv *Server) error {
		if h.NameLen < 8 {
			h.NameLen = 8
		}
		srv.sftpCfg().hostile = h
		return nil
	}
}

// WithSFTPFault makes the transfer of file (absolute, or relative to
// SFTPHome) fail once after at least after bytes were read or written:
// FaultDrop drops the connection, FaultFail fails that request. It fires
// once; later transfers of the file work.
func WithSFTPFault(file string, after int64, action FaultAction) Option {
	return func(srv *Server) error {
		srv.sftpCfg().faults[remotePath(file)] = &sftpFault{after: after, action: action}
		return nil
	}
}

// WithSFTPZeroFile serves a read-only virtual file of size zero bytes at
// name (absolute, or relative to SFTPHome), listed in its directory, for
// memory tests that need no disk.
func WithSFTPZeroFile(name string, size int64) Option {
	return func(srv *Server) error {
		srv.sftpCfg().zeros[remotePath(name)] = size
		return nil
	}
}

// WithoutSFTP refuses the sftp subsystem, also with WithSFTP.
func WithoutSFTP() Option {
	return func(srv *Server) error {
		srv.sftpCfg().off = true
		return nil
	}
}

// WithMaxSessions refuses session channels past n open at once on a
// connection, as OpenSSH's MaxSessions does.
func WithMaxSessions(n int) Option {
	return func(srv *Server) error {
		srv.maxSessions = n
		return nil
	}
}

// SFTPDir returns the local path of the remote path name (absolute, or
// relative to SFTPHome), for tests that set up or check the served tree.
func (s *Server) SFTPDir(name string) string {
	return s.sftp.dir + remotePath(name)
}

// SFTPBytesRead returns how many bytes clients have read from file
// (absolute, or relative to SFTPHome).
func (s *Server) SFTPBytesRead(file string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sftpRead[remotePath(file)]
}

// SFTPBytesWritten returns how many bytes clients have written to file
// (absolute, or relative to SFTPHome).
func (s *Server) SFTPBytesWritten(file string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sftpWritten[remotePath(file)]
}

// SFTPOpenHandles returns how many files are open on every SFTP channel.
func (s *Server) SFTPOpenHandles() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sftpHandles
}

// SFTPMaxOpenWriters returns the most files ever open for writing at once.
func (s *Server) SFTPMaxOpenWriters() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sftpMaxWriters
}

// sftpOn reports whether the subsystem is served.
func (s *Server) sftpOn() bool { return s.sftp != nil && !s.sftp.off && s.sftp.dir != "" }

// serveSFTP runs a request server on ch until the client closes it.
func (s *Server) serveSFTP(ch ssh.Channel, conn int) {
	root, err := os.OpenRoot(s.sftp.dir)
	if err != nil {
		return
	}
	defer func() { _ = root.Close() }()
	h := &sftpFS{srv: s, cfg: s.sftp, root: root, conn: conn}
	rs := sftp.NewRequestServer(ch, sftp.Handlers{FileGet: h, FilePut: h, FileCmd: h, FileList: h},
		sftp.WithStartDirectory(SFTPHome))
	_ = rs.Serve()
	_ = rs.Close()
}

// sftpFS implements the request server's handlers.
type sftpFS struct {
	srv  *Server
	cfg  *sftpConfig
	root *os.Root
	conn int
}

// rel turns a cleaned remote path into a path for the root.
func rel(p string) string {
	p = strings.TrimPrefix(path.Clean("/"+p), "/")
	if p == "" {
		return "."
	}
	return p
}

// info is a synthetic fs.FileInfo for virtual entries.
type info struct {
	name string
	size int64
	mode fs.FileMode
}

func (i info) Name() string       { return i.name }
func (i info) Size() int64        { return i.size }
func (i info) Mode() fs.FileMode  { return i.mode }
func (i info) ModTime() time.Time { return time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC) }
func (i info) IsDir() bool        { return i.mode.IsDir() }
func (i info) Sys() any           { return nil }

// virtual returns the virtual entry at p, if any: hostile entries and
// zero files.
func (h *sftpFS) virtual(p string) (info, bool) {
	hs := h.cfg.hostile
	name := path.Base(p)
	if size, ok := h.cfg.zeros[p]; ok {
		return info{name: name, size: size, mode: 0o644}, true
	}
	switch {
	case hs.Names && p == HostileDir:
		return info{name: name, mode: fs.ModeDir | 0o755}, true
	case hs.Names && strings.HasPrefix(p, HostileDir+"/"):
		return info{name: name, size: int64(len(HostileContent)), mode: 0o644}, true
	case hs.Entries > 0 && p == ManyDir:
		return info{name: name, mode: fs.ModeDir | 0o755}, true
	case hs.Entries > 0 && strings.HasPrefix(p, ManyDir+"/"):
		return info{name: name, mode: 0o644}, true
	case hs.Depth > 0 && (p == DeepDir || strings.HasPrefix(p, DeepDir+"/")):
		level := strings.Count(strings.TrimPrefix(p, DeepDir), "/d")
		if strings.TrimPrefix(p, DeepDir) != strings.Repeat("/d", level) || level > hs.Depth {
			return info{}, false
		}
		return info{name: name, mode: fs.ModeDir | 0o755}, true
	case hs.Oversize > 0 && p == OversizeFile:
		return info{name: name, size: hs.Oversize, mode: 0o644}, true
	case hs.Status != "" && p == DeniedFile:
		return info{name: name, mode: 0o644}, true
	}
	return info{}, false
}

// virtualChildren returns the virtual entries listed in dir.
func (h *sftpFS) virtualChildren(dir string) []fs.FileInfo {
	var out []fs.FileInfo
	for p := range h.cfg.zeros {
		if path.Dir(p) == dir {
			fi, _ := h.virtual(p)
			out = append(out, fi)
		}
	}
	if dir == SFTPHome {
		for _, p := range []string{HostileDir, ManyDir, DeepDir, OversizeFile, DeniedFile} {
			if fi, ok := h.virtual(p); ok {
				out = append(out, fi)
			}
		}
	}
	return out
}

// Fileread opens a file for reading.
func (h *sftpFS) Fileread(r *sftp.Request) (io.ReaderAt, error) {
	p := r.Filepath
	hs := h.cfg.hostile
	if fi, ok := h.virtual(p); ok {
		switch {
		case hs.Status != "" && p == DeniedFile:
			return nil, errors.New(hs.Status)
		case fi.IsDir():
			return nil, fmt.Errorf("%s: is a directory", p)
		case hs.Oversize > 0 && p == OversizeFile:
			return h.handle(p, endless{}, false), nil
		case strings.HasPrefix(p, HostileDir+"/"):
			return h.handle(p, strings.NewReader(HostileContent), false), nil
		default:
			return h.handle(p, zeros(fi.size), false), nil
		}
	}
	f, err := h.root.OpenFile(rel(p), os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	return h.handle(p, f, false), nil
}

// Filewrite opens a file for writing.
func (h *sftpFS) Filewrite(r *sftp.Request) (io.WriterAt, error) {
	f, err := h.open(r)
	if err != nil {
		return nil, err
	}
	return f, nil
}

// OpenFile opens a file for reading and writing.
func (h *sftpFS) OpenFile(r *sftp.Request) (sftp.WriterAtReaderAt, error) { return h.open(r) }

func (h *sftpFS) open(r *sftp.Request) (*handle, error) {
	p := r.Filepath
	if _, ok := h.virtual(p); ok {
		if h.cfg.hostile.Status != "" && p == DeniedFile {
			return nil, errors.New(h.cfg.hostile.Status)
		}
		return nil, os.ErrPermission
	}
	pf := r.Pflags()
	flags := os.O_WRONLY
	if pf.Read {
		flags = os.O_RDWR
	}
	if pf.Creat {
		flags |= os.O_CREATE
	}
	if pf.Trunc {
		flags |= os.O_TRUNC
	}
	if pf.Excl {
		flags |= os.O_EXCL
	}
	mode := fs.FileMode(0o644)
	if r.AttrFlags().Permissions {
		mode = r.Attributes().FileMode().Perm()
	}
	f, err := h.root.OpenFile(rel(p), flags, mode)
	if err != nil {
		return nil, err
	}
	return h.handle(p, f, true), nil
}

// Filecmd runs setstat, rename, posix-rename, remove, mkdir, rmdir and
// symlink.
func (h *sftpFS) Filecmd(r *sftp.Request) error {
	p := r.Filepath
	if _, ok := h.virtual(p); ok && r.Method != "Symlink" {
		return os.ErrPermission
	}
	switch r.Method {
	case "Setstat":
		return h.setstat(r)
	case "Rename":
		// SFTP v3 rename fails when the target exists, as on OpenSSH.
		if _, err := h.root.Lstat(rel(r.Target)); err == nil {
			return os.ErrExist
		}
		return h.root.Rename(rel(p), rel(r.Target))
	case "Remove":
		fi, err := h.root.Lstat(rel(p))
		if err != nil {
			return err
		}
		if fi.IsDir() {
			return fmt.Errorf("%s: is a directory", p)
		}
		return h.root.Remove(rel(p))
	case "Rmdir":
		fi, err := h.root.Lstat(rel(p))
		if err != nil {
			return err
		}
		if !fi.IsDir() {
			return fmt.Errorf("%s: not a directory", p)
		}
		return h.root.Remove(rel(p))
	case "Mkdir":
		return h.root.Mkdir(rel(p), 0o755)
	case "Symlink":
		// Filepath is the link's target, Target the link itself.
		return h.root.Symlink(r.Filepath, rel(r.Target))
	}
	return sftp.ErrSSHFxOpUnsupported
}

// PosixRename replaces the target (posix-rename@openssh.com).
func (h *sftpFS) PosixRename(r *sftp.Request) error {
	if _, ok := h.virtual(r.Filepath); ok {
		return os.ErrPermission
	}
	return h.root.Rename(rel(r.Filepath), rel(r.Target))
}

func (h *sftpFS) setstat(r *sftp.Request) error {
	p, af, a := rel(r.Filepath), r.AttrFlags(), r.Attributes()
	if af.Size {
		f, err := h.root.OpenFile(p, os.O_WRONLY, 0)
		if err != nil {
			return err
		}
		err = f.Truncate(int64(a.Size)) //nolint:gosec // a test server's sizes fit
		_ = f.Close()
		if err != nil {
			return err
		}
	}
	if af.Permissions {
		if err := h.root.Chmod(p, a.FileMode().Perm()); err != nil {
			return err
		}
	}
	if af.Acmodtime {
		if err := h.root.Chtimes(p, a.AccessTime(), a.ModTime()); err != nil {
			return err
		}
	}
	return nil
}

// Filelist lists a directory or stats a path, following links.
func (h *sftpFS) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	p := r.Filepath
	switch r.Method {
	case "List":
		return h.list(p)
	case "Stat":
		if fi, ok := h.virtual(p); ok {
			return lister{fi}, nil
		}
		fi, err := h.root.Stat(rel(p))
		if err != nil {
			return nil, err
		}
		return lister{fi}, nil
	}
	return nil, sftp.ErrSSHFxOpUnsupported
}

// Lstat stats a path without following a link.
func (h *sftpFS) Lstat(r *sftp.Request) (sftp.ListerAt, error) {
	if fi, ok := h.virtual(r.Filepath); ok {
		return lister{fi}, nil
	}
	fi, err := h.root.Lstat(rel(r.Filepath))
	if err != nil {
		return nil, err
	}
	return lister{fi}, nil
}

// Readlink returns a link's target as stored.
func (h *sftpFS) Readlink(p string) (string, error) { return h.root.Readlink(rel(p)) }

// RealPath resolves p against the start directory, without following
// links; the hostile mode answers "." with its Realpath.
func (h *sftpFS) RealPath(p string) (string, error) {
	if rp := h.cfg.hostile.Realpath; rp != "" && (p == "." || p == "") {
		return rp, nil
	}
	if !path.IsAbs(p) {
		p = path.Join(SFTPHome, p)
	}
	return path.Clean(p), nil
}

func (h *sftpFS) list(p string) (sftp.ListerAt, error) {
	hs := h.cfg.hostile
	switch {
	case hs.Status != "" && p == DeniedFile:
		return nil, errors.New(hs.Status)
	case hs.Names && p == HostileDir:
		out := make(lister, len(HostileNames))
		for i, n := range HostileNames {
			out[i] = info{name: n, size: int64(len(HostileContent)), mode: 0o644}
		}
		return out, nil
	case hs.Entries > 0 && p == ManyDir:
		return manyLister{n: hs.Entries, nameLen: hs.NameLen}, nil
	case hs.Depth > 0 && (p == DeepDir || strings.HasPrefix(p, DeepDir+"/")):
		if _, ok := h.virtual(p); !ok {
			return nil, os.ErrNotExist
		}
		if strings.Count(strings.TrimPrefix(p, DeepDir), "/d") == hs.Depth {
			return lister{info{name: "bottom.txt", mode: 0o644}}, nil
		}
		return lister{info{name: "d", mode: fs.ModeDir | 0o755}}, nil
	}
	if fi, ok := h.virtual(p); ok && !fi.IsDir() {
		return nil, fmt.Errorf("%s: not a directory", p)
	}
	d, err := h.root.Open(rel(p))
	if err != nil {
		return nil, err
	}
	defer func() { _ = d.Close() }()
	ents, err := d.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	out := make(lister, 0, len(ents))
	for _, e := range ents {
		fi, err := e.Info()
		if err != nil {
			continue // removed meanwhile
		}
		out = append(out, fi)
	}
	return append(out, h.virtualChildren(p)...), nil
}

// lister serves a fixed list of entries.
type lister []fs.FileInfo

func (l lister) ListAt(dst []fs.FileInfo, off int64) (int, error) {
	if off >= int64(len(l)) {
		return 0, io.EOF
	}
	n := copy(dst, l[off:])
	if n < len(dst) {
		return n, io.EOF
	}
	return n, nil
}

// manyLister makes the entries of ManyDir as they are listed.
type manyLister struct{ n, nameLen int }

func (m manyLister) ListAt(dst []fs.FileInfo, off int64) (int, error) {
	i := 0
	for ; i < len(dst) && int(off)+i < m.n; i++ {
		name := fmt.Sprintf("f%0*d", m.nameLen-1, int(off)+i)
		dst[i] = info{name: name, mode: 0o644}
	}
	if int(off)+i >= m.n {
		return i, io.EOF
	}
	return i, nil
}

// endless returns data forever: a file that grows past its size.
type endless struct{}

func (endless) ReadAt(p []byte, _ int64) (int, error) {
	for i := range p {
		p[i] = 'A'
	}
	return len(p), nil
}

// zeros is a file of n zero bytes.
type zeros int64

func (z zeros) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(z) {
		return 0, io.EOF
	}
	n := min(int64(len(p)), int64(z)-off)
	clear(p[:n])
	if n < int64(len(p)) {
		return int(n), io.EOF
	}
	return int(n), nil
}

// handle is an open file: it counts bytes, injects faults and tracks the
// open-handle count.
type handle struct {
	fs     *sftpFS
	path   string
	r      io.ReaderAt
	w      io.WriterAt
	c      io.Closer
	writer bool
	once   sync.Once
}

func (h *sftpFS) handle(p string, f io.ReaderAt, writer bool) *handle {
	hd := &handle{fs: h, path: p, r: f, writer: writer}
	if w, ok := f.(io.WriterAt); ok {
		hd.w = w
	}
	if c, ok := f.(io.Closer); ok {
		hd.c = c
	}
	s := h.srv
	s.mu.Lock()
	s.sftpHandles++
	if writer {
		s.sftpWriters++
		s.sftpMaxWriters = max(s.sftpMaxWriters, s.sftpWriters)
	}
	s.mu.Unlock()
	return hd
}

// fault fires the file's fault once its byte count is reached.
func (hd *handle) fault() error {
	s := hd.fs.srv
	s.mu.Lock()
	f := hd.fs.cfg.faults[hd.path]
	if f == nil || f.fired || f.done < f.after {
		s.mu.Unlock()
		return nil
	}
	f.fired = true
	conn := s.conns[hd.fs.conn]
	s.mu.Unlock()
	if f.action == FaultDrop && conn != nil {
		_ = conn.Close()
	}
	return ErrInjected
}

func (hd *handle) count(n int, write bool) {
	if n <= 0 {
		return
	}
	s := hd.fs.srv
	s.mu.Lock()
	defer s.mu.Unlock()
	if write {
		s.sftpWritten[hd.path] += int64(n)
	} else {
		s.sftpRead[hd.path] += int64(n)
	}
	if f := hd.fs.cfg.faults[hd.path]; f != nil {
		f.done += int64(n)
	}
}

func (hd *handle) ReadAt(p []byte, off int64) (int, error) {
	if err := hd.fault(); err != nil {
		return 0, err
	}
	n, err := hd.r.ReadAt(p, off)
	hd.count(n, false)
	return n, err
}

func (hd *handle) WriteAt(p []byte, off int64) (int, error) {
	if hd.w == nil {
		return 0, os.ErrPermission
	}
	if err := hd.fault(); err != nil {
		return 0, err
	}
	n, err := hd.w.WriteAt(p, off)
	hd.count(n, true)
	return n, err
}

// Close is called by the request server when the client closes the
// handle or the channel ends.
func (hd *handle) Close() error {
	var err error
	hd.once.Do(func() {
		if hd.c != nil {
			err = hd.c.Close()
		}
		s := hd.fs.srv
		s.mu.Lock()
		s.sftpHandles--
		if hd.writer {
			s.sftpWriters--
		}
		s.mu.Unlock()
	})
	return err
}
