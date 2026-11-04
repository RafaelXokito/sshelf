package sshclient

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"golang.org/x/net/ipv4"

	"github.com/RafaelXokito/sshelf/internal/term"
)

// mDNS for ".local" names (FR-CONN-018, D-031).
//
// Releases are built with CGO_ENABLED=0. On Linux that leaves Go's own
// resolver doing every lookup: it reads /etc/hosts and asks DNS, but unlike
// the system resolver it never asks mDNS (nss-mdns), so a name such as
// "raspberrypi.local" fails in sshelf although `ssh` reaches it. On darwin
// Go hands lookups to libSystem even without cgo, so mDNSResponder already
// answers ".local" names and sshelf adds nothing there.
//
// Elsewhere a ".local" name is link-local and belongs to mDNS (RFC 6762
// §3), so sshclient asks the link itself, one IPv4 mDNS query for the
// name's A record, at the same time as the normal lookup. Racing the two
// keeps /etc/hosts working and never waits on a DNS server that is not
// there (a Pi on a direct cable, an offline LAN).

const (
	// mdnsGroup is the IPv4 mDNS multicast group and port.
	mdnsGroup = "224.0.0.251:5353"
	// mdnsTimeout bounds one lookup, so an unknown name fails quickly
	// instead of using the whole connect timeout.
	mdnsTimeout = 2 * time.Second
	// mdnsMaxPacket is the largest mDNS message (RFC 6762 §17).
	mdnsMaxPacket = 9000
	// mdnsTTL is the IP TTL mDNS packets carry (RFC 6762 §11).
	mdnsTTL = 255
	// mdnsCacheFlush is the top bit of a record's class in mDNS answers.
	mdnsCacheFlush dnsmessage.Class = 1 << 15
)

// errNoMDNSAnswer means no responder answered within the timeout.
var errNoMDNSAnswer = errors.New("no answer")

// dialFunc opens a connection, as net.Dialer.DialContext does.
type dialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// lookupFunc returns an IPv4 address for host.
type lookupFunc func(ctx context.Context, host string) (netip.Addr, error)

// defaultDial returns the dial sshclient uses on goos when Config.Dial is
// nil: dial itself on darwin, whose resolver already asks mDNS, and dial
// with mDNS for ".local" names everywhere else.
func defaultDial(goos string, dial dialFunc, lookup lookupFunc) dialFunc {
	if goos == "darwin" {
		return dial
	}
	return dialWithMDNS(dial, lookup)
}

// dialWithMDNS wraps dial. Names not under ".local" go to dial unchanged.
// For a ".local" name it runs dial and an mDNS lookup at once, the lookup
// followed by a dial of the answered address with the same port. The first
// connection made wins; the other attempt is cancelled and waited for, and
// a connection it made anyway is closed, so nothing outlives the call. ctx
// bounds and cancels both.
//
// If both fail and mDNS answered, the failed dial of that address is
// returned: the host was found. Otherwise the normal dial's error is
// returned, with the mDNS failure appended when it is a *net.DNSError, so
// it is still classified as ErrNameResolution.
func dialWithMDNS(dial dialFunc, lookup lookupFunc) dialFunc {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil || !isLocalName(host) {
			return dial(ctx, network, addr)
		}
		rctx, cancel := context.WithCancel(ctx)
		defer cancel()

		results := make(chan raceResult, 2)
		race := func(mdns bool, fn func() raceResult) {
			term.SafeGo(func() { results <- fn() }, func(r any) {
				results <- raceResult{err: fmt.Errorf("panic: %v", r), mdns: mdns}
			})
		}
		race(false, func() raceResult {
			c, err := dial(rctx, network, addr)
			return raceResult{conn: c, err: err}
		})
		race(true, func() raceResult {
			ip, err := lookup(rctx, host)
			if err != nil {
				return raceResult{err: err, mdns: true}
			}
			slog.Debug("connect: resolved over mDNS", "host", host, "ip", ip)
			c, err := dial(rctx, network, net.JoinHostPort(ip.String(), port))
			return raceResult{conn: c, err: err, mdns: true, answered: true}
		})

		var won net.Conn
		var sys, md raceResult
		for range 2 {
			r := <-results
			switch {
			case r.err == nil && won == nil:
				won = r.conn
				cancel()
			case r.err == nil:
				_ = r.conn.Close()
			case r.mdns:
				md = r
			default:
				sys = r
			}
		}
		if won != nil {
			return won, nil
		}
		slog.Debug("connect: mDNS did not help", "host", host, "err", md.err)
		if md.answered {
			return nil, md.err
		}
		if _, ok := errors.AsType[*net.DNSError](sys.err); ok {
			return nil, fmt.Errorf("%w (mDNS: %v)", sys.err, md.err)
		}
		return nil, sys.err
	}
}

// raceResult is how one side of dialWithMDNS's race ended.
type raceResult struct {
	conn net.Conn
	err  error
	// mdns is set for the mDNS side, and answered when its lookup gave an
	// address.
	mdns, answered bool
}

// isLocalName reports whether host is a name under ".local", the mDNS
// domain, ignoring case and a trailing dot.
func isLocalName(host string) bool {
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	return len(h) > len(".local") && strings.HasSuffix(h, ".local")
}

// mdnsResolver sends mDNS queries for A records.
type mdnsResolver struct {
	// group is where queries go: mdnsGroup, or a test responder.
	group string
	// timeout bounds a lookup. The query is sent again after half of it,
	// because multicast is easily lost on Wi-Fi.
	timeout time.Duration
}

var defaultMDNS = mdnsResolver{group: mdnsGroup, timeout: mdnsTimeout}

// lookup asks for host's A record and returns the first address answered.
// The query comes from an ephemeral port, which makes it a "legacy
// unicast" query: responders answer to that port directly and echo the
// query's random ID (RFC 6762 §6.7), so sshelf never binds port 5353 or
// joins the group. A reply counts only if it carries that ID and, for a
// multicast query, comes from an address on one of this machine's IPv4
// subnets (§11), so a host off the link cannot plant an answer.
func (r mdnsResolver) lookup(ctx context.Context, host string) (netip.Addr, error) {
	name, err := dnsmessage.NewName(strings.TrimSuffix(host, ".") + ".")
	if err != nil {
		return netip.Addr{}, err
	}
	id, err := randomID()
	if err != nil {
		return netip.Addr{}, err
	}
	query, err := mdnsQuery(id, name)
	if err != nil {
		return netip.Addr{}, err
	}
	dst, err := net.ResolveUDPAddr("udp4", r.group)
	if err != nil {
		return netip.Addr{}, err
	}
	var link []netip.Prefix // nil: a unicast destination (tests), no check
	if dst.IP.IsMulticast() {
		link = localPrefixes()
	}
	c, err := net.ListenUDP("udp4", nil)
	if err != nil {
		return netip.Addr{}, err
	}
	defer func() { _ = c.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = c.Close() })
	defer stop()

	deadline := time.Now().Add(r.timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	resendAt := time.Now().Add(r.timeout / 2)
	if err := sendMDNS(c, query, dst); err != nil {
		return netip.Addr{}, err
	}
	buf := make([]byte, mdnsMaxPacket)
	resent := false
	for {
		wait := deadline
		if !resent && resendAt.Before(wait) {
			wait = resendAt
		}
		_ = c.SetReadDeadline(wait)
		n, from, err := c.ReadFromUDPAddrPort(buf)
		if err == nil {
			if ip, ok := mdnsAnswer(buf[:n], id, name); ok && fromLink(link, from.Addr()) {
				return ip, nil
			}
			continue
		}
		if ctx.Err() != nil {
			return netip.Addr{}, ctx.Err()
		}
		if !errors.Is(err, os.ErrDeadlineExceeded) {
			return netip.Addr{}, err
		}
		if resent || !time.Now().Before(deadline) {
			return netip.Addr{}, errNoMDNSAnswer
		}
		resent = true
		if err := sendMDNS(c, query, dst); err != nil {
			return netip.Addr{}, err
		}
	}
}

// randomID returns a random DNS message ID.
func randomID() (uint16, error) {
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(b[:]), nil
}

// mdnsQuery builds a query with the given ID for name's A record.
func mdnsQuery(id uint16, name dnsmessage.Name) ([]byte, error) {
	b := dnsmessage.NewBuilder(make([]byte, 0, 512), dnsmessage.Header{ID: id})
	if err := b.StartQuestions(); err != nil {
		return nil, err
	}
	if err := b.Question(dnsmessage.Question{Name: name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}); err != nil {
		return nil, err
	}
	return b.Finish()
}

// sendMDNS sends query to dst. A multicast dst gets one copy per
// multicast-capable interface, as the system's mDNS daemon does, so a VPN
// that owns the default route does not swallow the query. It fails only if
// no copy could be sent.
func sendMDNS(c *net.UDPConn, query []byte, dst *net.UDPAddr) error {
	ifs := multicastInterfaces()
	if !dst.IP.IsMulticast() || len(ifs) == 0 {
		_, err := c.WriteToUDP(query, dst)
		return err
	}
	pc := ipv4.NewPacketConn(c)
	_ = pc.SetMulticastTTL(mdnsTTL)
	var lastErr error
	sent := false
	for i := range ifs {
		if err := pc.SetMulticastInterface(&ifs[i]); err != nil {
			lastErr = err
			continue
		}
		if _, err := c.WriteToUDP(query, dst); err != nil {
			lastErr = err
			continue
		}
		sent = true
	}
	if !sent {
		return lastErr
	}
	return nil
}

// multicastInterfaces returns the interfaces that are up, support
// multicast and are not loopback.
func multicastInterfaces() []net.Interface {
	all, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []net.Interface
	for _, ifi := range all {
		if ifi.Flags&net.FlagUp != 0 && ifi.Flags&net.FlagMulticast != 0 && ifi.Flags&net.FlagLoopback == 0 {
			out = append(out, ifi)
		}
	}
	return out
}

// localPrefixes returns the IPv4 subnets of this machine's interfaces. It
// is empty, not nil, when they cannot be read, so every reply is refused.
func localPrefixes() []netip.Prefix {
	out := []netip.Prefix{}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return out
	}
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok {
			if p, err := netip.ParsePrefix(ipn.String()); err == nil && p.Addr().Is4() {
				out = append(out, p.Masked())
			}
		}
	}
	return out
}

// fromLink reports whether from lies in one of link's subnets. A nil link
// means no check.
func fromLink(link []netip.Prefix, from netip.Addr) bool {
	if link == nil {
		return true
	}
	from = from.Unmap()
	for _, p := range link {
		if p.Contains(from) {
			return true
		}
	}
	return false
}

// mdnsAnswer returns the first usable IPv4 address msg, a response with
// the given ID, gives for name. Anything else (a query, another ID, a
// malformed packet, records for other names or types, an unspecified,
// loopback, broadcast or multicast address) yields false. Names compare
// case-insensitively, and the cache-flush bit of the class is ignored.
func mdnsAnswer(msg []byte, id uint16, name dnsmessage.Name) (netip.Addr, bool) {
	var p dnsmessage.Parser
	h, err := p.Start(msg)
	if err != nil || !h.Response || h.ID != id {
		return netip.Addr{}, false
	}
	if err := p.SkipAllQuestions(); err != nil {
		return netip.Addr{}, false
	}
	for {
		ah, err := p.AnswerHeader()
		if err != nil {
			return netip.Addr{}, false
		}
		if !isAnswerFor(ah, name) {
			if err := p.SkipAnswer(); err != nil {
				return netip.Addr{}, false
			}
			continue
		}
		a, err := p.AResource()
		if err != nil {
			return netip.Addr{}, false
		}
		if ip := netip.AddrFrom4(a.A); isDialable(ip) {
			return ip, true
		}
	}
}

// isDialable reports whether ip can be another host's address on the
// link: not unspecified, loopback, broadcast or multicast.
func isDialable(ip netip.Addr) bool {
	return !ip.IsUnspecified() && !ip.IsLoopback() && !ip.IsMulticast() && ip != netip.AddrFrom4([4]byte{255, 255, 255, 255})
}

// isAnswerFor reports whether h is an IN A record for name.
func isAnswerFor(h dnsmessage.ResourceHeader, name dnsmessage.Name) bool {
	return h.Type == dnsmessage.TypeA &&
		h.Class&^mdnsCacheFlush == dnsmessage.ClassINET &&
		strings.EqualFold(h.Name.String(), name.String())
}
