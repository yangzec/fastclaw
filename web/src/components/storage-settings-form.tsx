"use client";

import { useEffect, useMemo, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  ObjectStoreConfig,
  ObjectStoreInput,
  getAgentObjectStore,
  getUserObjectStore,
  removeAgentObjectStore,
  removeUserObjectStore,
  saveAgentObjectStore,
  saveUserObjectStore,
  testAgentObjectStore,
  testUserObjectStore,
} from "@/lib/agent-storage-api";

type StorageScope = "agent" | "user";
const SAVED_SECRET_MASK = "••••••••";

function displaySecret(saved: boolean) {
  return saved ? SAVED_SECRET_MASK : "";
}

function payloadForSubmit(form: ObjectStoreInput): ObjectStoreInput {
  return {
    ...form,
    accessKey: form.accessKey === SAVED_SECRET_MASK ? "" : form.accessKey,
    secretKey: form.secretKey === SAVED_SECRET_MASK ? "" : form.secretKey,
  };
}

export function StorageSettingsForm({ scope, agentId }: { scope: StorageScope; agentId?: string }) {
  const [form, setForm] = useState<ObjectStoreInput>({ accountId: "", bucket: "", prefix: "", endpoint: "", publicBaseURL: "", accessKey: "", secretKey: "" });
  const [hasKeys, setHasKeys] = useState({ access: false, secret: false });
  const [source, setSource] = useState<ObjectStoreConfig["source"]>(scope === "agent" ? "global" : "global");
  const [status, setStatus] = useState<string>("");
  const [testing, setTesting] = useState(false);
  const [saving, setSaving] = useState(false);
  const [tested, setTested] = useState(false);

  useEffect(() => {
    let cancelled = false;
    const load = scope === "agent" ? getAgentObjectStore(agentId || "") : getUserObjectStore();
    load
      .then((cfg) => {
        if (cancelled) return;
        const savedKeys = { access: !!cfg.hasAccessKey, secret: !!cfg.hasSecretKey };
        setForm({ accountId: cfg.accountId || "", bucket: cfg.bucket || "", prefix: cfg.prefix || "", endpoint: cfg.endpoint || "", publicBaseURL: cfg.publicBaseURL || "", accessKey: displaySecret(savedKeys.access), secretKey: displaySecret(savedKeys.secret) });
        setHasKeys(savedKeys);
        setSource(cfg.source || "global");
      })
      .catch((err) => setStatus(err.message || "Failed to load storage settings"));
    return () => { cancelled = true; };
  }, [scope, agentId]);

  const canSave = useMemo(() => tested && !saving && !testing, [tested, saving, testing]);
  const update = (key: keyof ObjectStoreInput) => (e: React.ChangeEvent<HTMLInputElement>) => {
    setTested(false);
    setForm((f) => ({ ...f, [key]: e.target.value }));
  };
  const clearMaskOnFocus = (key: "accessKey" | "secretKey") => () => {
    setForm((f) => f[key] === SAVED_SECRET_MASK ? { ...f, [key]: "" } : f);
  };

  async function onTest() {
    setTesting(true); setStatus("");
    try {
      const payload = payloadForSubmit(form);
      const res = scope === "agent" ? await testAgentObjectStore(agentId || "", payload) : await testUserObjectStore(payload);
      setTested(true);
      setStatus(`Connection OK (${res.latencyMs} ms). You can save now.`);
    } catch (err) {
      setTested(false);
      setStatus(err instanceof Error ? err.message : "Connection test failed");
    } finally { setTesting(false); }
  }

  async function onSave() {
    setSaving(true); setStatus("");
    try {
      const payload = payloadForSubmit(form);
      const res = scope === "agent" ? await saveAgentObjectStore(agentId || "", payload) : await saveUserObjectStore(payload);
      const savedKeys = { access: res.objectstore.hasAccessKey, secret: res.objectstore.hasSecretKey };
      setHasKeys(savedKeys);
      setSource(res.objectstore.source || scope);
      setForm((f) => ({ ...f, accessKey: displaySecret(savedKeys.access), secretKey: displaySecret(savedKeys.secret) }));
      setTested(false);
      setStatus(`Saved. Verified in ${res.latencyMs} ms.`);
    } catch (err) { setStatus(err instanceof Error ? err.message : "Save failed"); }
    finally { setSaving(false); }
  }

  async function onRemove() {
    const label = scope === "agent" ? "this agent's R2 override" : "your user R2 storage settings";
    if (!confirm(`Remove ${label}? Existing files are not migrated or deleted.`)) return;
    setSaving(true); setStatus("");
    try {
      const res = scope === "agent" ? await removeAgentObjectStore(agentId || "") : await removeUserObjectStore();
      setHasKeys({ access: !!res.objectstore.hasAccessKey, secret: !!res.objectstore.hasSecretKey });
      setSource(res.objectstore.source || "global");
      setStatus(scope === "agent" ? `Override removed. Current source: ${sourceLabel(res.objectstore.source)}.` : "User storage removed. Agents without overrides will use global storage.");
    }
    catch (err) { setStatus(err instanceof Error ? err.message : "Remove failed"); }
    finally { setSaving(false); }
  }

  const isAgentOverride = scope === "agent" && source === "agent";
  const keyHint = scope === "agent" && source !== "agent" ? " (required for a new agent override)" : " (saved; leave blank to keep)";

  return (
    <div className="p-6 max-w-3xl space-y-6">
      <div>
        <h2 className="text-xl font-semibold">Storage</h2>
        <p className="text-sm text-muted-foreground mt-1">{scope === "agent" ? "Configure a Cloudflare R2 override for this agent. Existing files are not migrated." : "Configure Cloudflare R2 storage for all of your agents that do not have their own override. Existing and future agents inherit this setting. Existing files are not migrated."}</p>
        <p className="text-sm mt-2"><span className="text-muted-foreground">Current source:</span> {sourceLabel(source)}</p>
      </div>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Account ID" value={form.accountId} onChange={update("accountId")} />
        <Field label="Bucket" value={form.bucket} onChange={update("bucket")} />
        <Field label="Prefix (optional)" value={form.prefix || ""} onChange={update("prefix")} />
        <Field label="Endpoint (optional)" value={form.endpoint || ""} onChange={update("endpoint")} placeholder="https://<account>.r2.cloudflarestorage.com" />
        <Field label="Public Base URL (optional)" value={form.publicBaseURL || ""} onChange={update("publicBaseURL")} placeholder="https://cdn.example.com/fastclaw" />
        <Field label={`Access Key ID${hasKeys.access ? keyHint : ""}`} value={form.accessKey || ""} onChange={update("accessKey")} onFocus={clearMaskOnFocus("accessKey")} />
        <Field label={`Secret Access Key${hasKeys.secret ? keyHint : ""}`} type="password" value={form.secretKey || ""} onChange={update("secretKey")} onFocus={clearMaskOnFocus("secretKey")} />
      </div>
      <div className="flex gap-2">
        <Button variant="outline" onClick={onTest} disabled={testing || saving}>{testing ? "Testing…" : "Test connection"}</Button>
        <Button onClick={onSave} disabled={!canSave}>{saving ? "Saving…" : "Save"}</Button>
        {(scope === "user" || isAgentOverride) && <Button variant="destructive" onClick={onRemove} disabled={saving}>{scope === "agent" ? "Remove override" : "Remove user storage"}</Button>}
      </div>
      {status && <p className="text-sm text-muted-foreground">{status}</p>}
    </div>
  );
}

function sourceLabel(source?: ObjectStoreConfig["source"]) {
  if (source === "agent") return "Agent R2";
  if (source === "user") return "Inherited from User";
  return "Global storage";
}

function Field(props: { label: string; value: string; onChange: (e: React.ChangeEvent<HTMLInputElement>) => void; onFocus?: () => void; type?: string; placeholder?: string }) {
  return <div className="space-y-2"><Label>{props.label}</Label><Input type={props.type || "text"} value={props.value} onChange={props.onChange} onFocus={props.onFocus} placeholder={props.placeholder} /></div>;
}
