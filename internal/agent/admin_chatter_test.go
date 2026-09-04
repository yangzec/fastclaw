package agent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/bus"
	"github.com/fastclaw-ai/fastclaw/internal/store"
	"github.com/fastclaw-ai/fastclaw/internal/users"
)

// Shared-identity channels resolve the chatter to the channel owner's
// web user id, so an owner-bound personal channel is admin in DMs with
// zero extra configuration. Groups on such channels must NOT inherit
// that: routing rewrites every group speaker to the owner id, so owner
// equality proves nothing there.
func TestAdminChatterSharedIdentity(t *testing.T) {
	a := &Agent{ownerUserID: "u_owner"}

	dm := bus.InboundMessage{
		Channel:        "telegram",
		UserID:         "u_owner",
		PeerKind:       "dm",
		SharedIdentity: true,
	}
	if !a.isAdminChatter(dm) {
		t.Fatal("shared-identity DM resolved to owner should be admin")
	}

	group := dm
	group.PeerKind = "group"
	if a.isAdminChatter(group) {
		t.Fatal("group speaker on shared-identity channel must not be admin")
	}
}

// Regular (non-shared) IM chatters are minted as app_users — never equal
// to the owner id — so they stay guests unless the admins allowlist
// names their platform id.
func TestAdminChatterRegularIMChannel(t *testing.T) {
	a := &Agent{
		ownerUserID: "u_owner",
		admins:      map[string][]string{"telegram": {"u_app_listed"}},
	}

	stranger := bus.InboundMessage{Channel: "telegram", UserID: "u_app_stranger", PeerKind: "dm"}
	if a.isAdminChatter(stranger) {
		t.Fatal("minted app_user chatter must not be admin")
	}

	listed := bus.InboundMessage{Channel: "telegram", UserID: "u_app_listed", PeerKind: "dm"}
	if !a.isAdminChatter(listed) {
		t.Fatal("allowlisted chatter should be admin")
	}
}

func seedRoleUsers(t *testing.T) (store.Store, *Agent) {
	t.Helper()
	st, err := store.NewDBStore("sqlite", "file:"+filepath.Join(t.TempDir(), "roles.db")+"?cache=shared")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now().UTC()
	for _, u := range []store.UserRecord{
		{ID: "u_ops", Username: "ops", Email: "ops@x", PasswordHash: "x", Role: users.RoleSuperAdmin, Status: users.StatusActive, AgentQuota: -1, CreatedAt: now, UpdatedAt: now},
		{ID: "u_owner", Username: "owner", Email: "owner@x", PasswordHash: "x", Role: users.RoleUser, Status: users.StatusActive, AgentQuota: -1, CreatedAt: now, UpdatedAt: now},
		{ID: "u_chat", Username: "chat", Email: "chat@x", PasswordHash: "x", Role: users.RoleChannelUser, Status: users.StatusActive, AgentQuota: -1, CreatedAt: now, UpdatedAt: now},
	} {
		rec := u
		if err := st.CreateUser(context.Background(), &rec); err != nil {
			t.Fatalf("create %s: %v", rec.ID, err)
		}
	}
	return st, &Agent{ownerUserID: "u_owner", dataStore: st}
}

func TestChatterCanHost_WebRoles(t *testing.T) {
	t.Setenv("FASTCLAW_DEPLOY", "")
	_, a := seedRoleUsers(t)

	webOps := bus.InboundMessage{Channel: "web", UserID: "u_ops", PeerKind: "dm"}
	if !a.chatterCanHost(webOps) {
		t.Fatal("web super_admin should have host access")
	}
	if a.isAdminChatter(webOps) {
		t.Fatal("super_admin chatting on someone else's agent is not the agent admin")
	}

	webOwner := bus.InboundMessage{Channel: "web", UserID: "u_owner", PeerKind: "dm"}
	if a.chatterCanHost(webOwner) {
		t.Fatal("web agent owner (role=user) must not have host access")
	}
	if !a.isAdminChatter(webOwner) {
		t.Fatal("web owner should still be agent admin")
	}
}

func TestChatterCanHost_FeishuAndGroups(t *testing.T) {
	t.Setenv("FASTCLAW_DEPLOY", "")
	_, a := seedRoleUsers(t)
	a.ownerUserID = "u_ops"

	feishuGuest := bus.InboundMessage{Channel: "feishu", UserID: "u_chat", PeerKind: "dm"}
	if a.chatterCanHost(feishuGuest) {
		t.Fatal("unbound feishu channel_user must not have host access")
	}

	sharedDM := bus.InboundMessage{
		Channel: "feishu", UserID: "u_ops", PeerKind: "dm", SharedIdentity: true,
	}
	if !a.chatterCanHost(sharedDM) {
		t.Fatal("shared-identity DM rewritten to super_admin should have host access")
	}

	sharedGroup := sharedDM
	sharedGroup.PeerKind = "group"
	if a.chatterCanHost(sharedGroup) {
		t.Fatal("shared-identity group must never have host access")
	}
}

func TestChatterCanHost_HeartbeatAndCron(t *testing.T) {
	t.Setenv("FASTCLAW_DEPLOY", "")
	_, a := seedRoleUsers(t)

	hbOwner := bus.InboundMessage{Source: bus.SourceHeartbeat, UserID: "system"}
	if a.chatterCanHost(hbOwner) {
		t.Fatal("heartbeat of a role=user agent must not have host access")
	}
	if !a.isTurnAgentAdmin(hbOwner) {
		t.Fatal("heartbeat should still be agent-admin so persona files stay readable")
	}

	a.ownerUserID = "u_ops"
	if !a.chatterCanHost(hbOwner) {
		t.Fatal("heartbeat of a super_admin-owned agent should have host access")
	}

	cron := bus.InboundMessage{Channel: "web", UserID: "u_ops", Source: bus.SourceCron}
	if a.chatterCanHost(cron) {
		t.Fatal("cron replay must not inherit host access")
	}
	sub := bus.InboundMessage{Channel: "subagent", UserID: "u_ops", Source: bus.SourceSubAgent}
	if a.chatterCanHost(sub) {
		t.Fatal("subagent spawn must not inherit host access")
	}
}

func TestChatterCanHost_HostedDeploy(t *testing.T) {
	t.Setenv("FASTCLAW_DEPLOY", "hosted")
	_, a := seedRoleUsers(t)
	msg := bus.InboundMessage{Channel: "web", UserID: "u_ops", PeerKind: "dm"}
	if a.chatterCanHost(msg) {
		t.Fatal("hosted deploy must never grant host access")
	}
}

func TestWhoamiSplitsAdminAndHost(t *testing.T) {
	t.Setenv("FASTCLAW_DEPLOY", "")
	_, a := seedRoleUsers(t)

	owner := bus.InboundMessage{Channel: "web", UserID: "u_owner", PeerKind: "dm", Text: "/whoami"}
	got := a.handleSlashCommand(owner)
	if !got.handled {
		t.Fatal("whoami should be handled")
	}
	if want := "Agent admin: yes"; !strings.Contains(got.reply, want) {
		t.Fatalf("reply %q, want %q", got.reply, want)
	}
	if want := "Host access: no"; !strings.Contains(got.reply, want) {
		t.Fatalf("reply %q, want %q", got.reply, want)
	}

	a.ownerUserID = "u_ops"
	ops := bus.InboundMessage{Channel: "web", UserID: "u_ops", PeerKind: "dm", Text: "/whoami"}
	got = a.handleSlashCommand(ops)
	if want := "Agent admin: yes"; !strings.Contains(got.reply, want) {
		t.Fatalf("ops reply %q, want %q", got.reply, want)
	}
	if want := "Host access: yes"; !strings.Contains(got.reply, want) {
		t.Fatalf("ops reply %q, want %q", got.reply, want)
	}
}
