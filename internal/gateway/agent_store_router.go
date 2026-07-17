package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/config"
	"github.com/fastclaw-ai/fastclaw/internal/store"
	"github.com/fastclaw-ai/fastclaw/internal/workspace"
)

const objectStoreNamespace = "objectstore"

type agentStoreFactory func(config.ObjectStoreCfg) (workspace.Store, error)

// agentStoreRouter routes workspace operations to an agent-scoped object store
// when one is configured. Misconfigured agent overrides fail closed: once an
// agent has an objectstore row, operations never fall back to the global store.
type agentStoreRouter struct {
	global  workspace.Store
	st      store.Store
	factory agentStoreFactory

	mu    sync.Mutex
	cache map[string]agentStoreCacheEntry
}

type agentStoreCacheEntry struct {
	store       workspace.Store
	source      string
	ownerUserID string
}

func newAgentStoreRouter(global workspace.Store, st store.Store, factory agentStoreFactory) workspace.Store {
	if factory == nil {
		factory = defaultAgentStoreFactory
	}
	return &agentStoreRouter{global: global, st: st, factory: factory, cache: map[string]agentStoreCacheEntry{}}
}

func defaultAgentStoreFactory(cfg config.ObjectStoreCfg) (workspace.Store, error) {
	return workspace.Factory{
		Type:         cfg.Type,
		LocalDir:     cfg.Local.Root,
		AccountID:    cfg.AccountID,
		AliyunIntern: cfg.AliyunIntern,
		S3: workspace.S3Config{
			Endpoint:  cfg.S3.Endpoint,
			Region:    cfg.S3.Region,
			Bucket:    cfg.S3.Bucket,
			Prefix:    cfg.S3.Prefix,
			AccessKey: cfg.S3.AccessKey,
			SecretKey: cfg.S3.SecretKey,
			UseSSL:    true,
		},
	}.New("")
}

func (r *agentStoreRouter) ClearAgentStoreCache(agentID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.cache, agentID)
}

func (r *agentStoreRouter) ClearUserInheritedStoreCache(userID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for agentID, entry := range r.cache {
		if entry.source == "user" && entry.ownerUserID == userID {
			delete(r.cache, agentID)
		}
	}
}

func (r *agentStoreRouter) storeFor(ctx context.Context, agentID string) (workspace.Store, error) {
	if agentID == "" || r.st == nil {
		return r.global, nil
	}
	r.mu.Lock()
	cached, ok := r.cache[agentID]
	r.mu.Unlock()
	if ok {
		return cached.store, nil
	}

	if ws, ok, err := r.storeFromConfig(ctx, "agent", "", agentID); err != nil {
		return nil, err
	} else if ok {
		owner := ""
		if rec, recErr := r.st.GetAgent(ctx, agentID); recErr == nil && rec != nil {
			owner = rec.UserID
		}
		r.cacheStore(agentID, agentStoreCacheEntry{store: ws, source: "agent", ownerUserID: owner})
		return ws, nil
	}

	agentRec, err := r.st.GetAgent(ctx, agentID)
	if errors.Is(err, store.ErrNotFound) {
		return r.global, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load agent record: %w", err)
	}
	if agentRec == nil || agentRec.UserID == "" {
		return r.global, nil
	}
	if ws, ok, err := r.storeFromConfig(ctx, "user", agentRec.UserID, ""); err != nil {
		return nil, err
	} else if ok {
		r.cacheStore(agentID, agentStoreCacheEntry{store: ws, source: "user", ownerUserID: agentRec.UserID})
		return ws, nil
	}
	return r.global, nil
}

func (r *agentStoreRouter) cacheStore(agentID string, entry agentStoreCacheEntry) {
	r.mu.Lock()
	r.cache[agentID] = entry
	r.mu.Unlock()
}

func (r *agentStoreRouter) storeFromConfig(ctx context.Context, source, userID, agentID string) (workspace.Store, bool, error) {
	rec, err := r.st.GetConfigByName(ctx, store.KindSetting, userID, agentID, objectStoreNamespace)
	if errors.Is(err, store.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("load %s objectstore config: %w", source, err)
	}
	if rec == nil || !rec.Enabled || len(rec.Data) == 0 {
		return nil, false, fmt.Errorf("%s objectstore: invalid empty config", source)
	}
	var cfg config.ObjectStoreCfg
	b, err := json.Marshal(rec.Data)
	if err != nil {
		return nil, false, fmt.Errorf("marshal %s objectstore config: %w", source, err)
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil, false, fmt.Errorf("decode %s objectstore config: %w", source, err)
	}
	if cfg.Type != "cloudflare-r2" {
		return nil, false, fmt.Errorf("%s objectstore: unsupported type %q", source, cfg.Type)
	}
	cfg.S3.UseSSL = true
	ws, err := r.factory(cfg)
	if err != nil {
		return nil, false, fmt.Errorf("init %s objectstore: %w", source, err)
	}
	return ws, true, nil
}

func (r *agentStoreRouter) Put(ctx context.Context, agentID, projectID, sessionID, path string, rd io.Reader, size int64, contentType string) error {
	ws, err := r.storeFor(ctx, agentID)
	if err != nil {
		return err
	}
	return ws.Put(ctx, agentID, projectID, sessionID, path, rd, size, contentType)
}
func (r *agentStoreRouter) Get(ctx context.Context, agentID, projectID, sessionID, path string) (io.ReadCloser, error) {
	ws, err := r.storeFor(ctx, agentID)
	if err != nil {
		return nil, err
	}
	return ws.Get(ctx, agentID, projectID, sessionID, path)
}
func (r *agentStoreRouter) Stat(ctx context.Context, agentID, projectID, sessionID, path string) (*workspace.ObjectInfo, error) {
	ws, err := r.storeFor(ctx, agentID)
	if err != nil {
		return nil, err
	}
	return ws.Stat(ctx, agentID, projectID, sessionID, path)
}
func (r *agentStoreRouter) List(ctx context.Context, agentID, projectID, sessionID string) ([]workspace.ObjectInfo, error) {
	ws, err := r.storeFor(ctx, agentID)
	if err != nil {
		return nil, err
	}
	return ws.List(ctx, agentID, projectID, sessionID)
}
func (r *agentStoreRouter) Delete(ctx context.Context, agentID, projectID, sessionID, path string) error {
	ws, err := r.storeFor(ctx, agentID)
	if err != nil {
		return err
	}
	return ws.Delete(ctx, agentID, projectID, sessionID, path)
}
func (r *agentStoreRouter) Move(ctx context.Context, agentID, fromProjectID, fromSessionID, toProjectID, toSessionID string) error {
	ws, err := r.storeFor(ctx, agentID)
	if err != nil {
		return err
	}
	return ws.Move(ctx, agentID, fromProjectID, fromSessionID, toProjectID, toSessionID)
}
func (r *agentStoreRouter) SignedURL(ctx context.Context, agentID, projectID, sessionID, path string, ttl time.Duration) (string, error) {
	ws, err := r.storeFor(ctx, agentID)
	if err != nil {
		return "", err
	}
	return ws.SignedURL(ctx, agentID, projectID, sessionID, path, ttl)
}
