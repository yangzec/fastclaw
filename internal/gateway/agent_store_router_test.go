package gateway

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/config"
	"github.com/fastclaw-ai/fastclaw/internal/scope"
	"github.com/fastclaw-ai/fastclaw/internal/store"
	"github.com/fastclaw-ai/fastclaw/internal/workspace"
)

type recordingStore struct {
	name string
	puts []string
}

func (s *recordingStore) Put(ctx context.Context, agentID, projectID, sessionID, path string, r io.Reader, size int64, contentType string) error {
	s.puts = append(s.puts, agentID+":"+path)
	return nil
}
func (s *recordingStore) Get(ctx context.Context, agentID, projectID, sessionID, path string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}
func (s *recordingStore) Stat(ctx context.Context, agentID, projectID, sessionID, path string) (*workspace.ObjectInfo, error) {
	return &workspace.ObjectInfo{Path: path}, nil
}
func (s *recordingStore) List(ctx context.Context, agentID, projectID, sessionID string) ([]workspace.ObjectInfo, error) {
	return nil, nil
}
func (s *recordingStore) Delete(ctx context.Context, agentID, projectID, sessionID, path string) error {
	return nil
}
func (s *recordingStore) Move(ctx context.Context, agentID, fromProjectID, fromSessionID, toProjectID, toSessionID string) error {
	return nil
}
func (s *recordingStore) SignedURL(ctx context.Context, agentID, projectID, sessionID, path string, ttl time.Duration) (string, error) {
	return "", nil
}
func (s *recordingStore) PublicURL(ctx context.Context, agentID, projectID, sessionID, path string) (string, error) {
	return "", workspace.ErrSignedURLUnsupported
}

func newRouterTestStore(t *testing.T) store.Store {
	t.Helper()
	st, err := store.NewDBStore("sqlite", "file:"+filepath.Join(t.TempDir(), "fc.db")+"?cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return st
}

func saveR2(t *testing.T, st store.Store, agentID, bucket string) {
	t.Helper()
	saveScopedR2(t, st, "", agentID, bucket)
}

func saveUserR2(t *testing.T, st store.Store, userID, bucket string) {
	t.Helper()
	saveScopedR2(t, st, userID, "", bucket)
}

func saveScopedR2(t *testing.T, st store.Store, userID, agentID, bucket string) {
	t.Helper()
	cfg := config.ObjectStoreCfg{Type: "cloudflare-r2", AccountID: "acct"}
	cfg.S3.Bucket = bucket
	cfg.S3.AccessKey = "ak"
	cfg.S3.SecretKey = "sk"
	cfg.S3.UseSSL = true
	if err := scope.SaveSetting(context.Background(), st, userID, agentID, objectStoreNamespace, objectStoreDataForTest(cfg)); err != nil {
		t.Fatal(err)
	}
}

func saveAgentRecord(t *testing.T, st store.Store, agentID, userID string) {
	t.Helper()
	if err := st.SaveAgent(context.Background(), &store.AgentRecord{ID: agentID, UserID: userID, Name: agentID}); err != nil {
		t.Fatal(err)
	}
}

func objectStoreDataForTest(cfg config.ObjectStoreCfg) map[string]interface{} {
	return map[string]interface{}{"type": cfg.Type, "accountId": cfg.AccountID, "s3": map[string]interface{}{"bucket": cfg.S3.Bucket, "accessKey": cfg.S3.AccessKey, "secretKey": cfg.S3.SecretKey, "useSSL": true}}
}

func TestAgentStoreRouterRoutesAndFailsClosed(t *testing.T) {
	ctx := context.Background()
	st := newRouterTestStore(t)
	global := &recordingStore{name: "global"}
	stores := map[string]*recordingStore{}
	r := newAgentStoreRouter(global, st, func(cfg config.ObjectStoreCfg) (workspace.Store, error) {
		if cfg.S3.Bucket == "bad" {
			return nil, fmt.Errorf("bad config")
		}
		rs := &recordingStore{name: cfg.S3.Bucket}
		stores[cfg.S3.Bucket] = rs
		return rs, nil
	})
	if err := r.Put(ctx, "agent-a", "", "sess", "x.png", bytes.NewReader(nil), 0, "image/png"); err != nil {
		t.Fatal(err)
	}
	if len(global.puts) != 1 {
		t.Fatalf("unconfigured agent should use global store")
	}

	saveR2(t, st, "agent-a", "bucket-a")
	saveR2(t, st, "agent-b", "bucket-b")
	if err := r.Put(ctx, "agent-a", "", "sess", "a.png", bytes.NewReader(nil), 0, "image/png"); err != nil {
		t.Fatal(err)
	}
	if err := r.Put(ctx, "agent-b", "", "sess", "b.png", bytes.NewReader(nil), 0, "image/png"); err != nil {
		t.Fatal(err)
	}
	if len(stores["bucket-a"].puts) != 1 || len(stores["bucket-b"].puts) != 1 {
		t.Fatalf("agents did not route to independent stores: %#v", stores)
	}

	saveR2(t, st, "agent-c", "bad")
	if err := r.Put(ctx, "agent-c", "", "sess", "c.png", bytes.NewReader(nil), 0, "image/png"); err == nil {
		t.Fatalf("invalid config must not fallback to global")
	}
	if len(global.puts) != 1 {
		t.Fatalf("invalid config fell back to global")
	}
}

func TestAgentStoreRouterCacheInvalidation(t *testing.T) {
	ctx := context.Background()
	st := newRouterTestStore(t)
	global := &recordingStore{}
	var builds int
	r := newAgentStoreRouter(global, st, func(cfg config.ObjectStoreCfg) (workspace.Store, error) {
		builds++
		return &recordingStore{name: cfg.S3.Bucket}, nil
	})
	rr := r.(*agentStoreRouter)
	saveR2(t, st, "agent-a", "one")
	_ = r.Put(ctx, "agent-a", "", "", "x", bytes.NewReader(nil), 0, "")
	_ = r.Put(ctx, "agent-a", "", "", "y", bytes.NewReader(nil), 0, "")
	if builds != 1 {
		t.Fatalf("want cached store build once, got %d", builds)
	}
	rr.ClearAgentStoreCache("agent-a")
	_ = r.Put(ctx, "agent-a", "", "", "z", bytes.NewReader(nil), 0, "")
	if builds != 2 {
		t.Fatalf("cache invalidation did not rebuild, got %d", builds)
	}
}

func TestAgentStoreRouterAgentUserGlobalPriorityAndIsolation(t *testing.T) {
	ctx := context.Background()
	st := newRouterTestStore(t)
	global := &recordingStore{name: "global"}
	stores := map[string]*recordingStore{}
	r := newAgentStoreRouter(global, st, func(cfg config.ObjectStoreCfg) (workspace.Store, error) {
		rs := &recordingStore{name: cfg.S3.Bucket}
		stores[cfg.S3.Bucket] = rs
		return rs, nil
	})
	saveAgentRecord(t, st, "agent-a", "user-1")
	saveAgentRecord(t, st, "agent-b", "user-1")
	saveAgentRecord(t, st, "agent-c", "user-2")
	saveUserR2(t, st, "user-1", "user-one")
	saveUserR2(t, st, "user-2", "user-two")
	saveR2(t, st, "agent-a", "agent-a")

	_ = r.Put(ctx, "agent-a", "", "", "a", bytes.NewReader(nil), 0, "")
	_ = r.Put(ctx, "agent-b", "", "", "b", bytes.NewReader(nil), 0, "")
	_ = r.Put(ctx, "agent-c", "", "", "c", bytes.NewReader(nil), 0, "")
	_ = r.Put(ctx, "missing-agent", "", "", "g", bytes.NewReader(nil), 0, "")

	if got := stores["agent-a"].puts; len(got) != 1 || got[0] != "agent-a:a" {
		t.Fatalf("agent override not used with original agent id: %#v", got)
	}
	if got := stores["user-one"].puts; len(got) != 1 || got[0] != "agent-b:b" {
		t.Fatalf("user one inherited store not used: %#v", got)
	}
	if got := stores["user-two"].puts; len(got) != 1 || got[0] != "agent-c:c" {
		t.Fatalf("user two inherited store not isolated: %#v", got)
	}
	if len(global.puts) != 1 {
		t.Fatalf("missing agent should fall back to global, got %#v", global.puts)
	}
}

func TestAgentStoreRouterUserCacheInvalidationSkipsAgentOverrides(t *testing.T) {
	ctx := context.Background()
	st := newRouterTestStore(t)
	var builds int
	r := newAgentStoreRouter(&recordingStore{}, st, func(cfg config.ObjectStoreCfg) (workspace.Store, error) {
		builds++
		return &recordingStore{name: cfg.S3.Bucket}, nil
	}).(*agentStoreRouter)
	saveAgentRecord(t, st, "agent-a", "user-1")
	saveAgentRecord(t, st, "agent-b", "user-1")
	saveUserR2(t, st, "user-1", "user-one")
	saveR2(t, st, "agent-a", "agent-a")

	_ = r.Put(ctx, "agent-a", "", "", "a", bytes.NewReader(nil), 0, "")
	_ = r.Put(ctx, "agent-b", "", "", "b", bytes.NewReader(nil), 0, "")
	if builds != 2 {
		t.Fatalf("initial builds = %d, want 2", builds)
	}
	r.ClearUserInheritedStoreCache("user-1")
	_ = r.Put(ctx, "agent-a", "", "", "a2", bytes.NewReader(nil), 0, "")
	_ = r.Put(ctx, "agent-b", "", "", "b2", bytes.NewReader(nil), 0, "")
	if builds != 3 {
		t.Fatalf("user invalidation should rebuild inherited only, got builds=%d", builds)
	}
}

func TestAgentStoreRouterInvalidUserConfigFailsClosed(t *testing.T) {
	ctx := context.Background()
	st := newRouterTestStore(t)
	global := &recordingStore{}
	r := newAgentStoreRouter(global, st, func(cfg config.ObjectStoreCfg) (workspace.Store, error) {
		return nil, fmt.Errorf("bad user config")
	})
	saveAgentRecord(t, st, "agent-a", "user-1")
	saveUserR2(t, st, "user-1", "bad")
	if err := r.Put(ctx, "agent-a", "", "", "x", bytes.NewReader(nil), 0, ""); err == nil {
		t.Fatal("invalid user config must error")
	}
	if len(global.puts) != 0 {
		t.Fatal("invalid user config fell back to global")
	}
}
