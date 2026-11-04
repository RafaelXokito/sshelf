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
)

// mDNS fallback for ".local" names (FR-CONN-018, D-031).
//
// Releases are built with CGO_ENABLED=0, so Go's own resolver does every
// lookup. It reads /etc/hosts and asks DNS, but unlike the system resolver
// it never asks mDNS (nss-mdns on Linux, mDNSResponder on macOS), so a name
// such as "raspberrypi.local" fails in sshelf although `ssh` reaches it.
// When that happens sshclient asks the link itself: one IPv4 mDNS query
// (RFC 6762) for the name's A record.

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

// dialWithMDNS wraps dial: when dialling a ".local" name fails because the
// name could not be resolved, it asks lookup for the address and dials
// that instead, keeping the port. Every other result of dial, including a
// lookup that timed out or ran out of ctx, is returned unchanged. If lookup
// fails too, the original *net.DNSError is returned with the mDNS failure
// appended, so it is still classified as ErrNameResolution.
func dialWithMDNS(dial dialFunc, lookup lookupFunc) dialFunc {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := dial(ctx, network, addr)
		if err == nil || ctx.Err() != nil || !isNameNotFound(err) {
			return conn, err
		}
		host, port, splitErr := net.SplitHostPort(addr)
		if splitErr != nil || !isLocalName(host) {
			return nil, err
		}
		ip, lerr := lookup(ctx, host)
		if lerr != nil {
			slog.Debug("connect: mDNS lookup failed", "host", host, "err", lerr)
			return nil, fmt.Errorf("%w (mDNS: %v)", err, lerr)
		}
		slog.Debug("connect: resolved over mDNS", "host", host, "ip", ip)
		return dial(ctx, network, net.JoinHostPort(ip.String(), port))
	}
}

// isNameNotFound reports whether err is a failed name lookup that did not
// time out.
func isNameNotFound(err error) bool {
	de, ok := errors.AsType[*net.DNSError](err)
	return ok && !de.IsTimeout
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
// broadcast or multicast address) yields false. Names compare
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

// isDialable reports whether ip can be a host's address: not unspecified,
// broadcast or multicast.
func isDialable(ip netip.Addr) bool {
	return !ip.IsUnspecified() && !ip.IsMulticast() && ip != netip.AddrFrom4([4]byte{255, 255, 255, 255})
}

// isAnswerFor reports whether h is an IN A record for name.
func isAnswerFor(h dnsmessage.ResourceHeader, name dnsmessage.Name) bool {
	return h.Type == dnsmessage.TypeA &&
		h.Class&^mdnsCacheFlush == dnsmessage.ClassINET &&
		strings.EqualFold(h.Name.String(), name.String())
}
