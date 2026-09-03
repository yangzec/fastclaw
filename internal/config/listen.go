package config

import (
	"fmt"
	"net"
)

const (
	BindLoopback = "loopback"
	BindAll      = "all"
)

// DefaultBind is the listen mode when FASTCLAW_BIND is unset.
// "all" binds 0.0.0.0 so other devices on the LAN can reach the UI.
func DefaultBind() string { return BindAll }

// NormalizeBind maps empty / unknown values onto the default.
// Unknown values keep LAN reachability rather than silently falling
// back to loopback.
func NormalizeBind(bind string) string {
	switch bind {
	case BindLoopback, BindAll:
		return bind
	case "":
		return DefaultBind()
	default:
		return DefaultBind()
	}
}

// ListenHost is the TCP host for a bind mode.
func ListenHost(bind string) string {
	if NormalizeBind(bind) == BindLoopback {
		return "127.0.0.1"
	}
	return "0.0.0.0"
}

// ListenAddr is host:port for the web UI.
func ListenAddr(bind string, port int) string {
	return fmt.Sprintf("%s:%d", ListenHost(bind), port)
}

// LANHTTPURLs returns http://<private-ipv4>:<port> for each LAN
// interface, so startup logs can tell the operator how to open the
// dashboard from another device.
func LANHTTPURLs(port int) []string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var out []string
	seen := map[string]struct{}{}
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok || ipn.IP.IsLoopback() {
			continue
		}
		ip := ipn.IP.To4()
		if ip == nil || !ip.IsPrivate() {
			continue
		}
		s := ip.String()
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, fmt.Sprintf("http://%s:%d", s, port))
	}
	return out
}
