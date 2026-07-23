package sandbox

import (
	"strings"
	"testing"
)

func TestAppendBindMountAllowsColonInHostPath(t *testing.T) {
	host := "/home/ubuntu/.fastclaw/workspaces/agt/sessions/www-agents:chat:scope"
	args := appendBindMount(nil, host, "/workspace", false)
	if len(args) != 2 || args[0] != "--mount" {
		t.Fatalf("args = %#v, want --mount form", args)
	}
	if !strings.Contains(args[1], "source="+host) {
		t.Fatalf("mount spec = %q, missing raw host path", args[1])
	}
	if strings.Contains(args[1], host+":/workspace") {
		t.Fatalf("mount spec uses -v-style colon separator: %q", args[1])
	}
}
