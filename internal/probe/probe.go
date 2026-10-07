package probe

import (
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/vehkiya/sshx/internal/sshconfig"
)

// Address returns the host:port to TCP-probe for h. A host reached
// through ProxyJump is not directly reachable, so the first jump host is
// probed instead and returned as via. Jump hosts that are themselves
// configured aliases are resolved through hosts.
func Address(h sshconfig.HostItem, hosts []sshconfig.HostItem) (addr, via string) {
	if jump := strings.TrimSpace(h.ProxyJump); jump != "" && !strings.EqualFold(jump, "none") {
		via = strings.TrimSpace(strings.Split(jump, ",")[0])
		_, host, port := sshconfig.ParseTarget(via)
		if j, ok := sshconfig.FindHost(hosts, host); ok {
			if j.HostName != "" {
				host = j.HostName
			}
			if port == 0 {
				port = j.Port
			}
		}
		return JoinHostPort(host, port), via
	}

	host := h.HostName
	if host == "" {
		host = h.Alias
	}
	return JoinHostPort(host, h.Port), ""
}

// JoinHostPort joins a host and port, defaulting to port 22 if port <= 0.
func JoinHostPort(host string, port int) string {
	if port <= 0 {
		port = 22
	}
	return net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(port))
}

// TCP measures how long a TCP connection to addr takes to open.
func TCP(addr string, timeout time.Duration) (time.Duration, error) {
	start := time.Now()
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return 0, err
	}
	_ = conn.Close()
	return time.Since(start), nil
}
