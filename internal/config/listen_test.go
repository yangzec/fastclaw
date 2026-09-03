package config

import (
	"net"
	"os"
	"strings"
	"testing"
)

func TestNormalizeBindDefaultsToAll(t *testing.T) {
	if got := NormalizeBind(""); got != BindAll {
		t.Fatalf("empty = %q", got)
	}
	if got := NormalizeBind("loopback"); got != BindLoopback {
		t.Fatalf("loopback = %q", got)
	}
	if got := NormalizeBind("all"); got != BindAll {
		t.Fatalf("all = %q", got)
	}
	if got := NormalizeBind("weird"); got != BindAll {
		t.Fatalf("unknown should stay LAN-reachable, got %q", got)
	}
}

func TestListenAddr(t *testing.T) {
	if got := ListenAddr(BindAll, 18953); got != "0.0.0.0:18953" {
		t.Fatalf("all = %q", got)
	}
	if got := ListenAddr(BindLoopback, 18953); got != "127.0.0.1:18953" {
		t.Fatalf("loopback = %q", got)
	}
	if got := ListenAddr("", 18953); got != "0.0.0.0:18953" {
		t.Fatalf("default = %q", got)
	}
}

func TestLoadEnvBindDefaultAndOverride(t *testing.T) {
	t.Setenv("FASTCLAW_BIND", "")
	_ = os.Unsetenv("FASTCLAW_BIND")
	if got := LoadEnv().Gateway.Bind; got != BindAll {
		t.Fatalf("unset FASTCLAW_BIND = %q, want all", got)
	}

	t.Setenv("FASTCLAW_BIND", "loopback")
	if got := LoadEnv().Gateway.Bind; got != BindLoopback {
		t.Fatalf("FASTCLAW_BIND=loopback = %q", got)
	}
}

func TestLANHTTPURLsArePrivateIPv4(t *testing.T) {
	for _, u := range LANHTTPURLs(18953) {
		if !strings.HasPrefix(u, "http://") || !strings.HasSuffix(u, ":18953") {
			t.Fatalf("url = %q", u)
		}
		host := strings.TrimPrefix(u, "http://")
		host = strings.TrimSuffix(host, ":18953")
		ip := net.ParseIP(host)
		if ip == nil || ip.To4() == nil || !ip.IsPrivate() || ip.IsLoopback() {
			t.Fatalf("not a LAN IPv4: %q", u)
		}
	}
}
