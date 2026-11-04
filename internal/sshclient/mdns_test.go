package sshclient

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"
	"golang.org/x/net/dns/dnsmessage"
)

// query is a query the fake responder received.
type query struct {
	id uint16
	q  dnsmessage.Question
}

// fakeResponder is an mDNS responder on 127.0.0.1. For the nth query it
// receives (from 1) it sends back the packets reply returns.
type fakeResponder struct {
	conn    *net.UDPConn
	queries atomic.Int32
	got     chan query
}

func startResponder(t *testing.T, reply func(n int, q query) [][]byte) *fakeResponder {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	r := &fakeResponder{conn: conn, got: make(chan query, 16)}
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, mdnsMaxPacket)
		for {
			n, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			var p dnsmessage.Parser
			h, err := p.Start(buf[:n])
			if err != nil {
				continue
			}
			dq, err := p.Question()
			if err != nil {
				continue
			}
			q := query{id: h.ID, q: dq}
			r.got <- q
			for _, pkt := range reply(int(r.queries.Add(1)), q) {
				_, _ = conn.WriteToUDP(pkt, from)
			}
		}
	}()
	t.Cleanup(func() {
		_ = conn.Close()
		<-done
	})
	return r
}

func (r *fakeResponder) resolver(timeout time.Duration) mdnsResolver {
	return mdnsResolver{group: r.conn.LocalAddr().String(), timeout: timeout}
}

// aAnswer builds a response to the query with ID id giving name the
// address ip, with the class's cache-flush bit set as real responders do.
func aAnswer(t *testing.T, id uint16, name string, ip netip.Addr) []byte {
	t.Helper()
	return answer(t, dnsmessage.Header{ID: id, Response: true, Authoritative: true}, name, func(b *dnsmessage.Builder, h dnsmessage.ResourceHeader) error {
		return b.AResource(h, dnsmessage.AResource{A: ip.As4()})
	})
}

func answer(t *testing.T, hdr dnsmessage.Header, name string, add func(*dnsmessage.Builder, dnsmessage.ResourceHeader) error) []byte {
	t.Helper()
	b := dnsmessage.NewBuilder(nil, hdr)
	if err := b.StartAnswers(); err != nil {
		t.Fatal(err)
	}
	h := dnsmessage.ResourceHeader{Name: dnsmessage.MustNewName(name), Class: dnsmessage.ClassINET | mdnsCacheFlush, TTL: 120}
	if err := add(&b, h); err != nil {
		t.Fatal(err)
	}
	msg, err := b.Finish()
	if err != nil {
		t.Fatal(err)
	}
	return msg
}

var piAddr = netip.MustParseAddr("192.168.1.204")

func TestFR_CONN_018_ResolvesLocalName(t *testing.T) {
	r := startResponder(t, func(_ int, q query) [][]byte {
		return [][]byte{aAnswer(t, q.id, q.q.Name.String(), piAddr)}
	})

	got, err := r.resolver(time.Second).lookup(context.Background(), "Pi.local")
	if err != nil {
		t.Fatal(err)
	}
	if got != piAddr {
		t.Fatalf("lookup = %v, want %v", got, piAddr)
	}
	q := (<-r.got).q
	if q.Name.String() != "Pi.local." || q.Type != dnsmessage.TypeA || q.Class != dnsmessage.ClassINET {
		t.Fatalf("query = %+v, want an IN A question for Pi.local.", q)
	}
}

func TestFR_CONN_018_IgnoresOtherPackets(t *testing.T) {
	r := startResponder(t, func(_ int, q query) [][]byte {
		name := q.q.Name.String()
		notResponse := aAnswer(t, q.id, name, netip.MustParseAddr("10.0.0.1"))
		notResponse[2] &^= 0x80 // clear the QR bit: a query, not an answer
		aaaa := answer(t, dnsmessage.Header{ID: q.id, Response: true}, name, func(b *dnsmessage.Builder, h dnsmessage.ResourceHeader) error {
			return b.AAAAResource(h, dnsmessage.AAAAResource{AAAA: netip.MustParseAddr("fe80::1").As16()})
		})
		return [][]byte{
			[]byte("not dns"),
			notResponse,
			aAnswer(t, q.id+1, name, netip.MustParseAddr("10.0.0.3")), // another query's ID
			aAnswer(t, q.id, "other.local.", netip.MustParseAddr("10.0.0.2")),
			aaaa,
			aAnswer(t, q.id, name, netip.MustParseAddr("0.0.0.0")),
			aAnswer(t, q.id, name, netip.MustParseAddr("127.0.0.1")),
			aAnswer(t, q.id, name, netip.MustParseAddr("224.0.0.251")),
			aAnswer(t, q.id, strings.ToUpper(name), piAddr),
		}
	})

	got, err := r.resolver(time.Second).lookup(context.Background(), "pi.local")
	if err != nil {
		t.Fatal(err)
	}
	if got != piAddr {
		t.Fatalf("lookup = %v, want %v", got, piAddr)
	}
}

func TestFR_CONN_018_ResendsOnce(t *testing.T) {
	r := startResponder(t, func(n int, q query) [][]byte {
		if n == 1 {
			return nil // the first query is "lost"
		}
		return [][]byte{aAnswer(t, q.id, q.q.Name.String(), piAddr)}
	})

	got, err := r.resolver(400*time.Millisecond).lookup(context.Background(), "pi.local")
	if err != nil {
		t.Fatal(err)
	}
	if got != piAddr || r.queries.Load() != 2 {
		t.Fatalf("lookup = %v after %d queries, want %v after 2", got, r.queries.Load(), piAddr)
	}
}

func TestFR_CONN_018_NoAnswerFailsWithinTimeout(t *testing.T) {
	r := startResponder(t, func(int, query) [][]byte { return nil })

	start := time.Now()
	_, err := r.resolver(300*time.Millisecond).lookup(context.Background(), "pi.local")
	elapsed := time.Since(start)
	if !errors.Is(err, errNoMDNSAnswer) {
		t.Fatalf("err = %v, want errNoMDNSAnswer", err)
	}
	if elapsed < 300*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("gave up after %v, want about 300ms", elapsed)
	}
	if n := r.queries.Load(); n != 2 {
		t.Fatalf("queries = %d, want 2 (one resend)", n)
	}
}

func TestFR_CONN_018_ContextDeadlineBoundsLookup(t *testing.T) {
	r := startResponder(t, func(int, query) [][]byte { return nil })
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := r.resolver(10*time.Second).lookup(ctx, "pi.local")
	if err == nil {
		t.Fatal("lookup succeeded with nobody answering")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("gave up after %v, want about the context's 150ms", elapsed)
	}
	if n := r.queries.Load(); n != 1 {
		t.Fatalf("queries = %d, want 1 (the resend is past the deadline)", n)
	}
}

func TestFR_CONN_018_RepliesOnlyFromLink(t *testing.T) {
	link := []netip.Prefix{netip.MustParsePrefix("192.168.1.0/24"), netip.MustParsePrefix("10.42.0.0/16")}
	for from, want := range map[string]bool{
		"192.168.1.204":      true,
		"10.42.2.178":        true,
		"::ffff:192.168.1.7": true,
		"192.168.2.1":        false,
		"203.0.113.9":        false,
	} {
		if got := fromLink(link, netip.MustParseAddr(from)); got != want {
			t.Errorf("fromLink(%s) = %v, want %v", from, got, want)
		}
	}
	if !fromLink(nil, netip.MustParseAddr("203.0.113.9")) {
		t.Error("a nil link refused a reply; it means no check")
	}
	if fromLink([]netip.Prefix{}, netip.MustParseAddr("192.168.1.204")) {
		t.Error("an empty link accepted a reply")
	}
	for _, p := range localPrefixes() {
		if !p.Addr().Is4() || p != p.Masked() {
			t.Errorf("localPrefixes gave %v, want masked IPv4 subnets", p)
		}
	}
}

func TestFR_CONN_018_QueryIDIsRandom(t *testing.T) {
	r := startResponder(t, func(_ int, q query) [][]byte {
		return [][]byte{aAnswer(t, q.id, q.q.Name.String(), piAddr)}
	})
	ids := map[uint16]bool{}
	for range 8 {
		if _, err := r.resolver(time.Second).lookup(context.Background(), "pi.local"); err != nil {
			t.Fatal(err)
		}
		ids[(<-r.got).id] = true
	}
	// Eight random 16-bit IDs are all equal with probability 2^-112.
	if len(ids) < 2 {
		t.Fatalf("8 queries used IDs %v, want them random", ids)
	}
}

func TestFR_CONN_018_CancelStopsLookup(t *testing.T) {
	r := startResponder(t, func(int, query) [][]byte { return nil })
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)

	start := time.Now()
	_, err := r.resolver(10*time.Second).lookup(ctx, "pi.local")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("cancel took %v", elapsed)
	}
}

func TestFR_CONN_018_IsLocalName(t *testing.T) {
	for host, want := range map[string]bool{
		"pi.local":        true,
		"PI.LOCAL":        true,
		"pi.local.":       true,
		"a.b.local":       true,
		"local":           false,
		".local":          false,
		"pi.localhost":    false,
		"pi.local.lan":    false,
		"example.com":     false,
		"192.168.1.204":   false,
		"fe80::1%eth0":    false,
		"notlocal":        false,
		"pi.local.domain": false,
	} {
		if got := isLocalName(host); got != want {
			t.Errorf("isLocalName(%q) = %v, want %v", host, got, want)
		}
	}
}

// dnsFailure is what net.Dialer returns when the name has no address.
func dnsFailure(addr string) error {
	host, _, _ := net.SplitHostPort(addr)
	return &net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "server misbehaving", Name: host, Server: "127.0.0.53:53"}}
}

// dnsTimeout is what net.Dialer returns when no DNS server answers.
func dnsTimeout(addr string) error {
	host, _, _ := net.SplitHostPort(addr)
	return &net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "i/o timeout", Name: host, IsTimeout: true}}
}

var errRefused = &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connect: connection refused")}

// trackedConn records whether it was closed.
type trackedConn struct {
	net.Conn
	closed atomic.Bool
}

func (c *trackedConn) Close() error {
	c.closed.Store(true)
	return c.Conn.Close()
}

func newTrackedConn() *trackedConn {
	c1, c2 := net.Pipe()
	_ = c2.Close()
	return &trackedConn{Conn: c1}
}

func TestFR_CONN_018_RacesLocalNames(t *testing.T) {
	// How a fake dial or lookup behaves: "ok" succeeds at once, "hang"
	// blocks until its context ends (an unreachable DNS server, a silent
	// link); anything else fails at once.
	cases := []struct {
		name       string
		addr       string
		sys        string // the normal dial: ok, hang, nxdomain, refused
		mdns       string // the lookup: ok, hang, none
		mdnsDial   string // the dial of the mDNS address: ok, refused
		wantLookup bool
		wantFrom   string // which dial's connection is returned: sys, mdns
		wantErr    string // dns: a *net.DNSError; refused: errRefused
		wantMDNS   bool   // the error mentions mDNS
	}{
		{name: "other name", addr: "pi.example.com:22", sys: "nxdomain", mdns: "ok", wantErr: "dns"},
		{name: "local name resolved normally", addr: "pi.local:22", sys: "ok", mdns: "hang", wantLookup: true, wantFrom: "sys"},
		{name: "local name not in DNS", addr: "pi.local:2222", sys: "nxdomain", mdns: "ok", mdnsDial: "ok", wantLookup: true, wantFrom: "mdns"},
		{name: "DNS unreachable", addr: "pi.local:22", sys: "hang", mdns: "ok", mdnsDial: "ok", wantLookup: true, wantFrom: "mdns"},
		{name: "both answer", addr: "pi.local:22", sys: "ok", mdns: "ok", mdnsDial: "ok", wantLookup: true},
		{name: "hosts entry refused, no mDNS", addr: "pi.local:22", sys: "refused", mdns: "none", wantLookup: true, wantErr: "refused"},
		{name: "mDNS host refused", addr: "pi.local:22", sys: "nxdomain", mdns: "ok", mdnsDial: "refused", wantLookup: true, wantErr: "refused"},
		{name: "nobody answers", addr: "pi.local:22", sys: "nxdomain", mdns: "none", wantLookup: true, wantErr: "dns", wantMDNS: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := goleak.IgnoreCurrent()
			var mu sync.Mutex
			conns := map[string]*trackedConn{}
			mdnsAddr := net.JoinHostPort(piAddr.String(), portOf(tc.addr))
			dial := func(ctx context.Context, _, addr string) (net.Conn, error) {
				who, how := "sys", tc.sys
				if addr == mdnsAddr {
					who, how = "mdns", tc.mdnsDial
				}
				switch how {
				case "ok":
					c := newTrackedConn()
					mu.Lock()
					conns[who] = c
					mu.Unlock()
					return c, nil
				case "hang":
					<-ctx.Done()
					return nil, dnsTimeout(addr)
				case "nxdomain":
					return nil, dnsFailure(addr)
				default:
					return nil, errRefused
				}
			}
			var looked atomic.Bool
			lookup := func(ctx context.Context, host string) (netip.Addr, error) {
				looked.Store(true)
				if host != "pi.local" {
					t.Errorf("lookup(%q), want pi.local", host)
				}
				switch tc.mdns {
				case "ok":
					return piAddr, nil
				case "hang":
					<-ctx.Done()
					return netip.Addr{}, ctx.Err()
				default:
					return netip.Addr{}, errNoMDNSAnswer
				}
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			start := time.Now()
			conn, err := dialWithMDNS(dial, lookup)(ctx, "tcp", tc.addr)
			if elapsed := time.Since(start); elapsed > 2*time.Second {
				t.Fatalf("dial took %v, want no wait for the losing side", elapsed)
			}
			if looked.Load() != tc.wantLookup {
				t.Fatalf("mDNS lookup = %v, want %v", looked.Load(), tc.wantLookup)
			}
			if tc.wantErr != "" {
				if conn != nil || !isErr(err, tc.wantErr) {
					t.Fatalf("conn = %v, err = %v; want %s", conn, err, tc.wantErr)
				}
				if got := strings.Contains(err.Error(), "mDNS: no answer"); got != tc.wantMDNS {
					t.Fatalf("err = %q, mentions mDNS = %v, want %v", err, got, tc.wantMDNS)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if tc.wantFrom != "" && conn != net.Conn(conns[tc.wantFrom]) {
					t.Fatalf("got a connection from the wrong dial, want %s", tc.wantFrom)
				}
				// A connection that lost the race is closed; the winner
				// is not.
				for who, c := range conns {
					if winner := net.Conn(c) == conn; c.closed.Load() == winner {
						t.Errorf("%s connection closed = %v, winner = %v", who, c.closed.Load(), winner)
					}
				}
				_ = conn.Close()
			}
			goleak.VerifyNone(t, before)
		})
	}
}

func portOf(addr string) string {
	_, port, _ := net.SplitHostPort(addr)
	return port
}

func TestFR_CONN_018_CancelStopsRace(t *testing.T) {
	before := goleak.IgnoreCurrent()
	hang := func(ctx context.Context, _, addr string) (net.Conn, error) {
		<-ctx.Done()
		return nil, dnsTimeout(addr)
	}
	lookup := func(ctx context.Context, _ string) (netip.Addr, error) {
		<-ctx.Done()
		return netip.Addr{}, ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)

	start := time.Now()
	if _, err := dialWithMDNS(hang, lookup)(ctx, "tcp", "pi.local:22"); err == nil {
		t.Fatal("dial succeeded after cancel")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("cancel took %v", elapsed)
	}
	goleak.VerifyNone(t, before)
}

func TestFR_CONN_018_DarwinUsesSystemResolver(t *testing.T) {
	for goos, want := range map[string]bool{"darwin": false, "linux": true, "freebsd": true} {
		dial := func(_ context.Context, _, addr string) (net.Conn, error) { return nil, dnsFailure(addr) }
		var looked atomic.Bool
		lookup := func(context.Context, string) (netip.Addr, error) {
			looked.Store(true)
			return netip.Addr{}, errNoMDNSAnswer
		}
		_, _ = defaultDial(goos, dial, lookup)(context.Background(), "tcp", "pi.local:22")
		if looked.Load() != want {
			t.Errorf("%s: mDNS lookup = %v, want %v", goos, looked.Load(), want)
		}
	}
}

func TestFR_CONN_018_IsDialable(t *testing.T) {
	for ip, want := range map[string]bool{
		"192.168.1.204":   true,
		"10.0.0.1":        true,
		"169.254.10.20":   true,
		"0.0.0.0":         false,
		"127.0.0.1":       false,
		"127.1.2.3":       false,
		"224.0.0.251":     false,
		"239.255.255.250": false,
		"255.255.255.255": false,
	} {
		if got := isDialable(netip.MustParseAddr(ip)); got != want {
			t.Errorf("isDialable(%s) = %v, want %v", ip, got, want)
		}
	}
}

func TestFR_CONN_018_LookupFailureIsNameResolution(t *testing.T) {
	dial := func(_ context.Context, _, addr string) (net.Conn, error) { return nil, dnsFailure(addr) }
	lookup := func(context.Context, string) (netip.Addr, error) { return netip.Addr{}, errNoMDNSAnswer }

	_, err := dialWithMDNS(dial, lookup)(context.Background(), "tcp", "pi.local:22")
	err = classify(err)
	if !errors.Is(err, ErrNameResolution) {
		t.Fatalf("err = %v, want ErrNameResolution", err)
	}
	if !strings.Contains(err.Error(), "mDNS: no answer") {
		t.Fatalf("err = %q, want it to mention the mDNS failure", err)
	}
}

// lanDial stands in for a LAN without DNS, as a CGO_ENABLED=0 build sees
// it: a ".local" name never resolves (the lookup hangs until ctx ends),
// and piAddr is the test server.
func lanDial(srv string) dialFunc {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, _ := net.SplitHostPort(addr)
		if isLocalName(host) {
			<-ctx.Done()
			return nil, dnsTimeout(addr)
		}
		if host == piAddr.String() {
			addr = net.JoinHostPort(srv, port)
		}
		var d net.Dialer
		return d.DialContext(ctx, network, addr)
	}
}

func TestFR_CONN_018_ConnectByLocalName(t *testing.T) {
	f := newFixture(t, false)
	r := startResponder(t, func(_ int, q query) [][]byte {
		return [][]byte{aAnswer(t, q.id, q.q.Name.String(), piAddr)}
	})
	cfg := f.config()
	cfg.Dial = dialWithMDNS(lanDial(f.srv.Host()), r.resolver(time.Second).lookup)
	target := f.target
	target.Hostname = "pi.local"
	target.ConnectTimeout = 5 * time.Second
	p := &recordingPrompter{trust: true}

	start := time.Now()
	c, err := cfg.Connect(context.Background(), target, f.creds, p)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("connect took %v, want it not to wait for DNS", elapsed)
	}
	// The host key is checked against the name the user typed, not the
	// address mDNS gave.
	want := net.JoinHostPort("pi.local", strconv.Itoa(f.srv.Port()))
	if len(p.prompts) != 1 || p.prompts[0].Address != want {
		t.Fatalf("prompts = %+v, want one for %s", p.prompts, want)
	}
}

func TestFR_CONN_018_ConnectUnknownLocalName(t *testing.T) {
	f := newFixture(t, true)
	r := startResponder(t, func(int, query) [][]byte { return nil })
	cfg := f.config()
	cfg.Dial = dialWithMDNS(func(_ context.Context, _, addr string) (net.Conn, error) {
		return nil, dnsFailure(addr)
	}, r.resolver(200*time.Millisecond).lookup)
	target := f.target
	target.Hostname = "missing.local"

	c, err := cfg.Connect(context.Background(), target, f.creds, nil)
	if err == nil {
		_ = c.Close()
		t.Fatal("connected to a name nobody answered for")
	}
	if !errors.Is(err, ErrNameResolution) {
		t.Fatalf("err = %v, want ErrNameResolution", err)
	}
}

// isErr reports whether err is of kind: "dns" for a *net.DNSError,
// "refused" for errRefused.
func isErr(err error, kind string) bool {
	if kind == "dns" {
		_, ok := errors.AsType[*net.DNSError](err)
		return ok
	}
	return errors.Is(err, errRefused)
}
