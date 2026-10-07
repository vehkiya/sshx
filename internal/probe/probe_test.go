package probe

import (
	"testing"

	"github.com/vehkiya/sshx/internal/sshconfig"
)

func TestProbeAddress(t *testing.T) {
	hosts := []sshconfig.HostItem{
		{Alias: "bastion", AllAliases: []string{"bastion"}, HostName: "bastion.example", Port: 2200},
		{Alias: "inner", AllAliases: []string{"inner"}, HostName: "10.0.0.5", Port: 22, ProxyJump: "bastion,other"},
		{Alias: "direct", AllAliases: []string{"direct"}, Port: 22},
		{Alias: "v6", AllAliases: []string{"v6"}, HostName: "[2001:db8::1]", Port: 2222},
		{Alias: "adhoc", AllAliases: []string{"adhoc"}, HostName: "x", ProxyJump: "ops@jump.example:2022"},
	}
	tests := []struct {
		alias, addr, via string
	}{
		{"inner", "bastion.example:2200", "bastion"},
		{"direct", "direct:22", ""},
		{"v6", "[2001:db8::1]:2222", ""},
		{"adhoc", "jump.example:2022", "ops@jump.example:2022"},
	}
	for _, tc := range tests {
		h, _ := sshconfig.FindHost(hosts, tc.alias)
		addr, via := Address(h, hosts)
		if addr != tc.addr || via != tc.via {
			t.Errorf("Address(%s) = (%q, %q); expected (%q, %q)", tc.alias, addr, via, tc.addr, tc.via)
		}
	}
}
