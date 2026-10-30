package session

import (
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/RafaelXokito/sshelf/internal/testutil"
	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
)

func TestMain(m *testing.M) { testutil.VerifyTestMain(m) }

// dialServer connects to srv as user u with password pw.
func dialServer(t *testing.T, srv *sshtest.Server) *ssh.Client {
	t.Helper()
	c, err := ssh.Dial("tcp", srv.Addr(), &ssh.ClientConfig{
		User:            "u",
		Auth:            []ssh.AuthMethod{ssh.Password("pw")},
		HostKeyCallback: ssh.FixedHostKey(srv.HostKeys()[0]),
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}
