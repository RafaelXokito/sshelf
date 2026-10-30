package sshtest

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The M2 scripted commands (ARCH-TEST-003). Each runs inside the scripted
// shell, or as an exec command after which the session exits 0:
//
//	flood <size>    writes exactly <size> bytes (FloodData) as fast as the
//	                client takes them; size is bytes, or a number with K or
//	                M (KiB, MiB)
//	stream <rate>   writes text lines at <rate> bytes per second until the
//	                client sends a byte (which is consumed)
//	replay <path>   writes the file at path
//	modes <list>    enables the DECSET modes, e.g. "modes 1000,1006"
//	query           sends ESC[c, ESC[6n and an OSC 11 query, then waits for
//	                the three replies (recorded, see Server.Received)
//	hostile         writes HostileCorpus
//	echo-hex        echoes every byte received as two hex digits and a
//	                space, until the channel closes

// HostileCorpus is the set of sequences a hostile server sends
// (ARCH-SESS-011, SEC-SESSION-005): clipboard writes and reads, titles, a
// hyperlink, colour changes, DCS, DECRQSS, XTGETTCAP, APC (kitty
// graphics), sixel, PM, SOS, mode toggles meant for the user's terminal
// (alternate screen, bracketed paste, focus events, the kitty keyboard
// protocol, a stray paste end marker), terminal
// queries (DA1, DA2, DSR, CPR, DECRQM, XTVERSION, window reports), a
// reset, 8-bit C1 forms, and very long, cut and broken sequences. The
// only text it prints is the hyperlink's "link"; none of it prints "evil".
var HostileCorpus = []string{
	"\x1b]52;c;c2VjcmV0\x07",
	"\x1b]52;c;?\x07",
	"\x1b]0;evil title\x07",
	"\x1b]1;evil icon\x07",
	"\x1b]2;evil title\x1b\\",
	"\x1b]8;;https://evil.example/\x1b\\link\x1b]8;;\x1b\\",
	"\x1b]4;1;rgb:ff/00/00\x07",
	"\x1b]10;#ff0000\x07\x1b]11;#00ff00\x07",
	"\x1bP+q544e\x1b\\",
	"\x1bP$qm\x1b\\",
	"\x1b_Gf=100;AAAA\x1b\\",
	"\x1b_Ga=T,f=100,t=d;iVBORw0KGgoevil\x1b\\",
	"\x1bPq#0;2;0;0;0#0~~@@vv@@~~$\x1b\\",
	"\x1b^privacy\x1b\\",
	"\x1bXsos\x1b\\",
	"\x1b[?1049h\x1b[?1049l",
	"\x1b[?2004h\x1b[?2004l",
	"\x1b[?1004h\x1b[?1004l",
	"\x1b[>1u\x1b[?u\x1b[<u",
	"\x1b[201~",
	"\x1b[c\x1b[>c\x1b[5n\x1b[6n\x1b[?6n",
	"\x1b[?2004$p\x1b[>0q",
	"\x1b[21t\x1b[14t\x1b[18t",
	"\x1bc",
	"\x9b31m\x9d0;c1 title\x9c",
	"\xc2\x9b31m",
	"\x1b]2;" + strings.Repeat("evil ", 16<<10) + "\x07",
	"\x1b[" + strings.Repeat("1;", 4000) + "m\x1b[m",
	"\x1b]0;evil cut by CAN\x18",
	"\x1bP$qevil cut by ESC\x1b[m",
	"\x1b[?1;2\x1b]2;evil after a broken CSI\x07",
	"\x1b]52;c;" + strings.Repeat("ZXZpbA", 8<<10) + "\x1b\\",
}

// QuerySequences is what the query command sends.
const QuerySequences = "\x1b[c\x1b[6n\x1b]11;?\x1b\\"

// FloodEnd is the tail of every FloodData: CAN, which ends any escape
// sequence cut by the size, then a line the client can look for.
const FloodEnd = "\x18\r\nflood done\r\n"

// FloodData returns n bytes of 100-column text lines, every other one in
// an SGR colour, ending with FloodEnd.
func FloodData(n int) []byte {
	const filler = " the quick brown fox jumps over the lazy dog; the quick brown fox jumps over the lazy dog; 0123456789"
	var b bytes.Buffer
	b.Grow(n + 128)
	for i := 0; b.Len() < n; i++ {
		line := (fmt.Sprintf("%08d", i) + filler)[:100]
		if i%2 == 1 {
			fmt.Fprintf(&b, "\x1b[3%dm%s\x1b[0m\r\n", 1+i%6, line)
		} else {
			b.WriteString(line + "\r\n")
		}
	}
	data := b.Bytes()[:n]
	if n >= len(FloodEnd) {
		copy(data[n-len(FloodEnd):], FloodEnd)
	}
	return data
}

// parseSize parses "123", "100K" or "10M" (K = 1024 bytes).
func parseSize(s string) (int, error) {
	mult := 1
	switch {
	case strings.HasSuffix(s, "K"):
		mult, s = 1<<10, strings.TrimSuffix(s, "K")
	case strings.HasSuffix(s, "M"):
		mult, s = 1<<20, strings.TrimSuffix(s, "M")
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("bad size %q", s)
	}
	return n * mult, nil
}

// runScript runs an M2 command and reports whether line was one.
func runScript(ss *Session, in *bufio.Reader, line string) bool {
	f := strings.Fields(line)
	if len(f) == 0 {
		return false
	}
	arg := ""
	if len(f) > 1 {
		arg = f[1]
	}
	switch f[0] {
	case "flood":
		n, err := parseSize(arg)
		if err != nil {
			_, _ = ss.Write([]byte("sshtest: flood: " + err.Error() + "\r\n"))
			return true
		}
		data := FloodData(n)
		for len(data) > 0 {
			k := min(len(data), 32<<10)
			if _, err := ss.Write(data[:k]); err != nil {
				return true
			}
			data = data[k:]
		}
	case "stream":
		rate, err := parseSize(arg)
		if err != nil || rate == 0 {
			_, _ = ss.Write([]byte("sshtest: stream: bad rate\r\n"))
			return true
		}
		stream(ss, in, rate)
	case "replay":
		data, err := os.ReadFile(arg) //nolint:gosec // a fixture path chosen by the test
		if err != nil {
			_, _ = ss.Write([]byte("sshtest: replay: " + err.Error() + "\r\n"))
			return true
		}
		_, _ = ss.Write(data)
	case "modes":
		var b strings.Builder
		for m := range strings.SplitSeq(arg, ",") {
			if _, err := strconv.Atoi(m); err == nil {
				b.WriteString("\x1b[?" + m + "h")
			}
		}
		_, _ = ss.Write([]byte(b.String()))
	case "query":
		_, _ = ss.Write([]byte(QuerySequences))
		var got []byte
		answered := func() bool {
			return bytes.Contains(got, []byte("c")) && bytes.Contains(got, []byte("R")) &&
				(bytes.Contains(got, []byte("\x07")) || bytes.Contains(got, []byte("\x1b\\")))
		}
		for !answered() {
			b, err := in.ReadByte()
			if err != nil {
				return true
			}
			got = append(got, b)
		}
	case "hostile":
		_, _ = ss.Write([]byte(strings.Join(HostileCorpus, "")))
	case "echo-hex":
		for {
			b, err := in.ReadByte()
			if err != nil {
				return true
			}
			if _, err := fmt.Fprintf(ss, "%02x ", b); err != nil {
				return true
			}
		}
	default:
		return false
	}
	return true
}

// stream writes lines at rate bytes per second, in 10 ms steps, until a
// byte arrives from the client or the channel closes.
func stream(ss *Session, in *bufio.Reader, rate int) {
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = in.ReadByte()
		close(stop)
	}()
	defer wg.Wait()
	line := []byte("streaming the quick brown fox jumps over the lazy dog 0123456789\r\n")
	var sent int
	start := time.Now()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
		}
		due := int(time.Since(start).Seconds() * float64(rate))
		if due <= sent {
			continue
		}
		out := make([]byte, due-sent)
		for i := range out {
			out[i] = line[(sent+i)%len(line)]
		}
		if _, err := ss.Write(out); err != nil {
			<-stop
			return
		}
		sent += len(out)
	}
}
