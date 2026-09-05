"use client";

import { useEffect, useState } from "react";
import {
  listSSHHosts,
  createSSHHost,
  updateSSHHost,
  deleteSSHHost,
  testSSHHost,
  disconnectSSHHost,
  type SSHAuthType,
  type SSHHost,
} from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Plus, Trash2, Pencil, PlugZap, Unplug } from "lucide-react";

type FormState = {
  name: string;
  host: string;
  port: string;
  username: string;
  authType: SSHAuthType;
  password: string;
  privateKey: string;
  passphrase: string;
  defaultCwd: string;
  idleTimeoutSec: string;
  persistTmux: boolean;
};

const emptyForm: FormState = {
  name: "",
  host: "",
  port: "22",
  username: "",
  authType: "key",
  password: "",
  privateKey: "",
  passphrase: "",
  defaultCwd: "",
  idleTimeoutSec: "7200",
  persistTmux: true,
};

const IDLE_OPTIONS = [
  { value: "900", label: "15 minutes" },
  { value: "3600", label: "1 hour" },
  { value: "7200", label: "2 hours" },
  { value: "28800", label: "8 hours" },
  { value: "-1", label: "Until FastClaw restarts" },
] as const;

const IDLE_LABELS: Record<string, string> = Object.fromEntries(
  IDLE_OPTIONS.map((o) => [o.value, o.label]),
);

export default function SSHHostsPage() {
  const [hosts, setHosts] = useState<SSHHost[]>([]);
  const [error, setError] = useState("");
  const [form, setForm] = useState<FormState>(emptyForm);
  const [editing, setEditing] = useState<SSHHost | null>(null);
  const [open, setOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<SSHHost | null>(null);
  const [testingId, setTestingId] = useState("");
  const [disconnectingId, setDisconnectingId] = useState("");
  const [testNote, setTestNote] = useState("");
  const [saving, setSaving] = useState(false);

  async function refresh() {
    setError("");
    const r = await listSSHHosts();
    if (r.error) setError(r.error);
    setHosts(r.hosts || []);
  }

  useEffect(() => {
    refresh();
  }, []);

  function openCreate() {
    setEditing(null);
    setForm(emptyForm);
    setOpen(true);
  }

  function openEdit(h: SSHHost) {
    setEditing(h);
    setForm({
      name: h.name,
      host: h.host,
      port: String(h.port || 22),
      username: h.username,
      authType: h.authType,
      password: "",
      privateKey: "",
      passphrase: "",
      defaultCwd: h.defaultCwd || "",
      idleTimeoutSec: String(h.idleTimeoutSec ?? 7200),
      persistTmux: h.persistTmux !== false,
    });
    setOpen(true);
  }

  async function handleSave(e: React.FormEvent) {
    e.preventDefault();
    setSaving(true);
    setError("");
    const port = Number(form.port || "22");
    const body = {
      name: form.name.trim(),
      host: form.host.trim(),
      port,
      username: form.username.trim(),
      authType: form.authType,
      defaultCwd: form.defaultCwd.trim(),
      idleTimeoutSec: Number(form.idleTimeoutSec),
      persistTmux: form.persistTmux,
      ...(form.authType === "password" && form.password
        ? { password: form.password }
        : {}),
      ...(form.authType === "key" && form.privateKey
        ? { privateKey: form.privateKey, passphrase: form.passphrase }
        : {}),
    };
    const res = editing
      ? await updateSSHHost(editing.id, body)
      : await createSSHHost(body);
    setSaving(false);
    if (res.error || !res.ok) {
      setError(res.error || "save failed");
      return;
    }
    setOpen(false);
    refresh();
  }

  async function handleTest(h: SSHHost) {
    setTestingId(h.id);
    setTestNote("");
    const res = await testSSHHost(h.id);
    setTestingId("");
    setTestNote(res.ok ? `${h.name}: connected` : `${h.name}: ${res.error || "failed"}`);
    if (res.ok) refresh();
  }

  async function handleDisconnect(h: SSHHost) {
    setDisconnectingId(h.id);
    setTestNote("");
    const res = await disconnectSSHHost(h.id);
    setDisconnectingId("");
    setTestNote(res.ok ? `${h.name}: disconnected` : `${h.name}: ${res.error || "failed"}`);
    refresh();
  }

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h3 className="text-xl font-semibold tracking-tight">SSH Hosts</h3>
          <p className="text-sm text-muted-foreground mt-1">
            Save servers once. The agent logs in by alias and keeps the SSH
            session (default 2 hours idle). Passwords and private keys stay out
            of chat.
          </p>
        </div>
        <Button onClick={openCreate}>
          <Plus className="size-4" />
          Add host
        </Button>
      </div>

      {error && <p className="text-sm text-destructive">{error}</p>}
      {testNote && <p className="text-sm text-muted-foreground">{testNote}</p>}

      {hosts.length === 0 ? (
        <p className="text-sm text-muted-foreground rounded-lg border border-dashed p-6">
          No saved hosts yet. Add one, then tell the agent &quot;on gpu-box, run
          df -h&quot;. FastClaw stays logged in so follow-up commands do not
          handshake again.
        </p>
      ) : (
        <div className="rounded-lg border divide-y">
          {hosts.map((h) => (
            <div key={h.id} className="flex flex-wrap items-center gap-3 px-4 py-3">
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className="font-medium">{h.name}</span>
                  <Badge variant="secondary">
                    {h.authType === "key" ? "public key" : "password"}
                  </Badge>
                  {h.connected && <Badge>Connected</Badge>}
                  {h.persistTmux && (
                    <Badge variant="outline">{h.tmuxSession || "tmux"}</Badge>
                  )}
                  {!h.enabled && <Badge variant="outline">disabled</Badge>}
                </div>
                <p className="text-sm text-muted-foreground truncate">
                  {h.username}@{h.host}:{h.port}
                  {h.defaultCwd ? ` · ${h.defaultCwd}` : ""}
                  {` · idle ${IDLE_LABELS[String(h.idleTimeoutSec ?? 7200)] || `${h.idleTimeoutSec}s`}`}
                </p>
              </div>
              <div className="flex items-center gap-1">
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => handleTest(h)}
                  disabled={testingId === h.id}
                >
                  <PlugZap className="size-4" />
                  {testingId === h.id ? "Testing…" : "Test"}
                </Button>
                {h.connected && (
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => handleDisconnect(h)}
                    disabled={disconnectingId === h.id}
                  >
                    <Unplug className="size-4" />
                    {disconnectingId === h.id ? "Closing…" : "Disconnect"}
                  </Button>
                )}
                <Button variant="ghost" size="icon" onClick={() => openEdit(h)}>
                  <Pencil className="size-4" />
                </Button>
                <Button variant="ghost" size="icon" onClick={() => setDeleteTarget(h)}>
                  <Trash2 className="size-4" />
                </Button>
              </div>
            </div>
          ))}
        </div>
      )}

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-w-lg">
          <form onSubmit={handleSave}>
            <DialogHeader>
              <DialogTitle>{editing ? "Edit SSH host" : "Add SSH host"}</DialogTitle>
              <DialogDescription>
                The agent calls this host by the alias and reuses the SSH
                connection. Leave secret fields blank when editing to keep the
                saved credential.
              </DialogDescription>
            </DialogHeader>
            <div className="grid gap-3 py-4">
              <div className="grid gap-1.5">
                <Label htmlFor="ssh-name">Alias</Label>
                <Input
                  id="ssh-name"
                  placeholder="gpu-box"
                  value={form.name}
                  onChange={(e) => setForm({ ...form, name: e.target.value })}
                  required
                />
              </div>
              <div className="grid grid-cols-3 gap-3">
                <div className="col-span-2 grid gap-1.5">
                  <Label htmlFor="ssh-host">Host</Label>
                  <Input
                    id="ssh-host"
                    placeholder="10.0.4.21"
                    value={form.host}
                    onChange={(e) => setForm({ ...form, host: e.target.value })}
                    required
                  />
                </div>
                <div className="grid gap-1.5">
                  <Label htmlFor="ssh-port">Port</Label>
                  <Input
                    id="ssh-port"
                    type="number"
                    value={form.port}
                    onChange={(e) => setForm({ ...form, port: e.target.value })}
                  />
                </div>
              </div>
              <div className="grid gap-1.5">
                <Label htmlFor="ssh-user">Username</Label>
                <Input
                  id="ssh-user"
                  placeholder="deploy"
                  value={form.username}
                  onChange={(e) => setForm({ ...form, username: e.target.value })}
                  required
                />
              </div>
              <div className="grid gap-1.5">
                <Label>Auth</Label>
                <div className="flex gap-2">
                  {(["key", "password"] as SSHAuthType[]).map((t) => (
                    <Button
                      key={t}
                      type="button"
                      variant={form.authType === t ? "default" : "outline"}
                      size="sm"
                      onClick={() => setForm({ ...form, authType: t })}
                    >
                      {t === "key" ? "Public key" : "Password"}
                    </Button>
                  ))}
                </div>
              </div>
              {form.authType === "password" ? (
                <div className="grid gap-1.5">
                  <Label htmlFor="ssh-pass">Password</Label>
                  <Input
                    id="ssh-pass"
                    type="password"
                    placeholder={editing ? "unchanged if blank" : ""}
                    value={form.password}
                    onChange={(e) => setForm({ ...form, password: e.target.value })}
                    required={!editing}
                    autoComplete="new-password"
                  />
                </div>
              ) : (
                <>
                  <div className="grid gap-1.5">
                    <Label htmlFor="ssh-key">Private key (PEM)</Label>
                    <Textarea
                      id="ssh-key"
                      rows={6}
                      placeholder={
                        editing
                          ? "unchanged if blank"
                          : "-----BEGIN OPENSSH PRIVATE KEY-----"
                      }
                      value={form.privateKey}
                      onChange={(e) => setForm({ ...form, privateKey: e.target.value })}
                      required={!editing}
                      className="font-mono text-xs"
                    />
                  </div>
                  <div className="grid gap-1.5">
                    <Label htmlFor="ssh-key-pass">Key passphrase (optional)</Label>
                    <Input
                      id="ssh-key-pass"
                      type="password"
                      value={form.passphrase}
                      onChange={(e) => setForm({ ...form, passphrase: e.target.value })}
                      autoComplete="new-password"
                    />
                  </div>
                </>
              )}
              <div className="grid gap-1.5">
                <Label htmlFor="ssh-cwd">Default directory (optional)</Label>
                <Input
                  id="ssh-cwd"
                  placeholder="/srv/app"
                  value={form.defaultCwd}
                  onChange={(e) => setForm({ ...form, defaultCwd: e.target.value })}
                />
              </div>
              <div className="grid gap-1.5">
                <Label>Disconnect when idle</Label>
                <Select
                  value={form.idleTimeoutSec}
                  onValueChange={(v) => v && setForm({ ...form, idleTimeoutSec: String(v) })}
                >
                  <SelectTrigger className="w-full">
                    <SelectValue>
                      {(v: unknown) =>
                        IDLE_LABELS[v as string] ?? (v as string) ?? ""
                      }
                    </SelectValue>
                  </SelectTrigger>
                  <SelectContent>
                    {IDLE_OPTIONS.map((opt) => (
                      <SelectItem key={opt.value} value={opt.value}>
                        {opt.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <p className="text-xs text-muted-foreground">
                  FastClaw stays logged in and reuses the session. Next command
                  after disconnect logs in again.
                </p>
              </div>
              <div className="flex items-center justify-between gap-3 rounded-lg border px-3 py-2">
                <div className="min-w-0">
                  <Label htmlFor="ssh-tmux">Keep a tmux session</Label>
                  <p className="text-xs text-muted-foreground mt-0.5">
                    Creates <span className="font-mono">fastclaw-{form.name || "alias"}</span> on
                    the server so you can attach. Ignored if tmux is not installed.
                  </p>
                </div>
                <Switch
                  id="ssh-tmux"
                  checked={form.persistTmux}
                  onCheckedChange={(v) => setForm({ ...form, persistTmux: v })}
                />
              </div>
            </div>
            <DialogFooter>
              <Button type="submit" disabled={saving}>
                {saving ? "Saving…" : "Save"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <AlertDialog open={!!deleteTarget} onOpenChange={() => setDeleteTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete {deleteTarget?.name}?</AlertDialogTitle>
            <AlertDialogDescription>
              The agent will no longer be able to use this alias. The remote
              server is not changed.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={async () => {
                if (!deleteTarget) return;
                const res = await deleteSSHHost(deleteTarget.id);
                if (res.error) setError(res.error);
                setDeleteTarget(null);
                refresh();
              }}
            >
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
