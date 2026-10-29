package sshclient

import (
	"context"
	"errors"
	"net"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
)

// connectErr connects to target with cfg and returns the error, closing
// any client.
func connectErr(ctx context.Context, cfg Config, target domain.Target, creds CredentialSource, p Prompter) error {
	c, err := cfg.Connect(ctx, target, creds, p)
	if c != nil {
		_ = c.Close()
	}
	return err
}

func TestFR_CONN_006_NameResolutionFailed(t *testing.T) {
	// A resolver that cannot reach any DNS server: no network in tests.
	r := &net.Resolver{PreferGo: true, Dial: func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("no DNS in tests")
	}}
	d := net.Dialer{Resolver: r}
	cfg := Config{Dial: d.DialContext}
	err := connectErr(context.Background(), cfg, domain.Target{Hostname: "sshelf-test.invalid", Port: 22}, nil, nil)
	if !errors.Is(err, ErrNameResolution) {
		t.Fatalf("err = %v, want ErrNameResolution", err)
	}
}

func TestFR_CONN_006_ConnectionRefused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	err = connectErr(context.Background(), Config{}, domain.Target{Hostname: "127.0.0.1", Port: port}, nil, nil)
	if !errors.Is(err, ErrConnectionRefused) {
		t.Fatalf("err = %v, want ErrConnectionRefused", err)
	}
}

// blackHole is a loopback listener that accepts connections and never
// answers.
func blackHole(t *testing.T) *net.TCPAddr {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var (
		mu    sync.Mutex
		conns []net.Conn
		wg    sync.WaitGroup
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		wg.Wait()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			_ = c.Close()
		}
	})
	return ln.Addr().(*net.TCPAddr)
}

func TestFR_CONN_006_TimedOut(t *testing.T) {
	addr := blackHole(t)
	target := domain.Target{Hostname: "127.0.0.1", Port: addr.Port, ConnectTimeout: 150 * time.Millisecond}
	start := time.Now()
	err := connectErr(context.Background(), Config{}, target, nil, nil)
	if !errors.Is(err, ErrTimedOut) {
		t.Fatalf("err = %v, want ErrTimedOut", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("timing out took %v", d)
	}
}

func TestFR_CONN_006_DialTimedOut(t *testing.T) {
	stall := func(ctx context.Context, _, _ string) (net.Conn, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	target := domain.Target{Hostname: "127.0.0.1", Port: 22, ConnectTimeout: 50 * time.Millisecond}
	err := connectErr(context.Background(), Config{Dial: stall}, target, nil, nil)
	if !errors.Is(err, ErrTimedOut) {
		t.Fatalf("err = %v, want ErrTimedOut", err)
	}
}

func TestFR_CONN_006_DefaultTimeoutIs10s(t *testing.T) {
	if DefaultConnectTimeout != 10*time.Second || domain.DefaultConnectTimeout != 10*time.Second {
		t.Fatalf("default connect timeout = %v / %v", DefaultConnectTimeout, domain.DefaultConnectTimeout)
	}
}

func TestFR_CONN_006_PromptTimeNotCounted(t *testing.T) {
	// The user takes longer than the timeout to answer: that is not a
	// network timeout.
	p := &scriptedPrompter{answer: func(q AuthPrompt) ([]string, error) {
		time.Sleep(1300 * time.Millisecond)
		return []string{kbdPassword}, nil
	}}
	srv := sshtest.New(t, sshtest.WithPassword("alice", kbdPassword))
	target := domain.Target{Hostname: srv.Host(), Port: srv.Port(), User: "alice", ConnectTimeout: time.Second}
	if err := connectErr(context.Background(), trusting(t, srv), target, pwCreds{}, p); err != nil {
		t.Fatal(err)
	}
}

func TestFR_CONN_006_HostKeyRejected(t *testing.T) {
	f := newFixture(t, false)
	err := connectErr(context.Background(), f.config(), f.target, f.creds, nil)
	if !errors.Is(err, ErrHostKeyRejected) {
		t.Fatalf("err = %v, want ErrHostKeyRejected", err)
	}
}

func TestFR_CONN_006_AuthFailed(t *testing.T) {
	f := newFixture(t, true)
	other, err := sshtest.GenerateSigner(sshtest.Ed25519)
	if err != nil {
		t.Fatal(err)
	}
	err = connectErr(context.Background(), f.config(), f.target, &fakeCreds{signer: other}, nil)
	if !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("err = %v, want ErrAuthFailed", err)
	}
}

func TestFR_CONN_006_Stages(t *testing.T) {
	f := newFixture(t, true)
	var mu sync.Mutex
	var stages []Stage
	cfg := f.config()
	cfg.Progress = func(s Stage) {
		mu.Lock()
		stages = append(stages, s)
		mu.Unlock()
	}
	if err := connectErr(context.Background(), cfg, f.target, f.creds, nil); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(stages, []Stage{StageDialling, StageVerifying, StageAuthenticating}) {
		t.Fatalf("stages = %v", stages)
	}
}

// cancelWithin cancels ctx once ready is closed and checks that connect
// returns context.Canceled within 200 ms (ARCH-CONN-005).
func cancelWithin(t *testing.T, ready <-chan struct{}, connect func(ctx context.Context) error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errc := make(chan error, 1)
	go func() { errc <- connect(ctx) }()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("the attempt never reached the point to cancel at")
	}
	start := time.Now()
	cancel()
	select {
	case err := <-errc:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		if d := time.Since(start); d > 200*time.Millisecond {
			t.Fatalf("cancel took %v", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Connect did not return after cancel")
	}
}

func TestARCH_CONN_005_CancelDuringDial(t *testing.T) {
	dialling := make(chan struct{})
	stall := func(ctx context.Context, _, _ string) (net.Conn, error) {
		close(dialling)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	cancelWithin(t, dialling, func(ctx context.Context) error {
		return connectErr(ctx, Config{Dial: stall}, domain.Target{Hostname: "127.0.0.1", Port: 22}, nil, nil)
	})
}

func TestARCH_CONN_005_CancelDuringAuth(t *testing.T) {
	srv := sshtest.New(t, sshtest.WithKeyboardInteractive("alice", passwordRound))
	p := blockingPrompter{asked: make(chan struct{})}
	target := domain.Target{Hostname: srv.Host(), Port: srv.Port(), User: "alice"}
	cfg := trusting(t, srv)
	cancelWithin(t, p.asked, func(ctx context.Context) error {
		return connectErr(ctx, cfg, target, pwCreds{}, p)
	})
}
