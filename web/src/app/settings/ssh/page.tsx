"use client";

import { useEffect, useState } from "react";
import {
  listSSHHosts,
  createSSHHost,
  updateSSHHost,
  deleteSSHHost,
  testSSHHost,
  type SSHAuthType,
  type SSHHost,
} from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Badge } from "@/components/ui/badge";
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
import { Plus, Trash2, Pencil, PlugZap } from "lucide-react";

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
};

function statusBadge(h: SSHHost) {
  if (h.lastTestStatus === "ok") {
    return <Badge>Connected</Badge>;
  }
  if (h.lastTestStatus === "fail") {
    return (
      <Badge variant="destructive" title={h.lastTestError || "connection failed"}>
        Failed
      </Badge>
    );
  }
  return <Badge variant="outline">Not tested</Badge>;
}

function formatTestedAt(value?: string | null) {
  if (!value) return "";
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return "";
  return d.toLocaleString();
}

export default function SSHHostsPage() {
  const [hosts, setHosts] = useState<SSHHost[]>([]);
  const [error, setError] = useState("");
  const [form, setForm] = useState<FormState>(emptyForm);
  const [formError, setFormError] = useState("");
  const [editing, setEditing] = useState<SSHHost | null>(null);
  const [open, setOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<SSHHost | null>(null);
  const [testingId, setTestingId] = useState("");
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
    setFormError("");
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
    });
    setFormError("");
    setOpen(true);
  }

  async function handleSave(e: React.FormEvent) {
    e.preventDefault();
    setSaving(true);
    setFormError("");
    setError("");
    const port = Number(form.port || "22");
    const body = {
      name: form.name.trim(),
      host: form.host.trim(),
      port,
      username: form.username.trim(),
      authType: form.authType,
      defaultCwd: form.defaultCwd.trim(),
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
      setFormError(res.error || "connection failed");
      return;
    }
    setOpen(false);
    refresh();
  }

  async function handleTest(h: SSHHost) {
    setTestingId(h.id);
    const res = await testSSHHost(h.id);
    setTestingId("");
    if (res.error && !res.ok) {
      setError(`${h.name}: ${res.error}`);
    } else {
      setError("");
    }
    refresh();
  }

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h3 className="text-xl font-semibold tracking-tight">SSH Hosts</h3>
          <p className="text-sm text-muted-foreground mt-1">
            FastClaw tests the connection before saving. The agent then
            connects with the alias — passwords and private keys stay out of
            chat.
          </p>
        </div>
        <Button onClick={openCreate}>
          <Plus className="size-4" />
          Add host
        </Button>
      </div>

      {error && <p className="text-sm text-destructive">{error}</p>}

      {hosts.length === 0 ? (
        <p className="text-sm text-muted-foreground rounded-lg border border-dashed p-6">
          No saved hosts yet. Add one, then tell the agent &quot;on gpu-box, run
          df -h&quot;.
        </p>
      ) : (
        <div className="rounded-lg border divide-y">
          {hosts.map((h) => (
            <div key={h.id} className="flex flex-wrap items-center gap-3 px-4 py-3">
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2 flex-wrap">
                  <span className="font-medium">{h.name}</span>
                  <Badge variant="secondary">
                    {h.authType === "key" ? "public key" : "password"}
                  </Badge>
                  {statusBadge(h)}
                  {!h.enabled && <Badge variant="outline">disabled</Badge>}
                </div>
                <p className="text-sm text-muted-foreground truncate">
                  {h.username}@{h.host}:{h.port}
                  {h.defaultCwd ? ` · ${h.defaultCwd}` : ""}
                </p>
                {h.lastTestStatus === "fail" && h.lastTestError ? (
                  <p className="text-xs text-destructive mt-1 truncate" title={h.lastTestError}>
                    {h.lastTestError}
                  </p>
                ) : null}
                {formatTestedAt(h.lastTestedAt) ? (
                  <p className="text-xs text-muted-foreground mt-0.5">
                    Last tested {formatTestedAt(h.lastTestedAt)}
                  </p>
                ) : null}
              </div>
              <div className="flex items-center gap-1">
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => handleTest(h)}
                  disabled={testingId === h.id}
                >
                  <PlugZap className="size-4" />
                  {testingId === h.id ? "Testing…" : "Retest"}
                </Button>
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
                FastClaw will try the connection before saving. If it fails,
                nothing is stored. Leave secret fields blank when editing to
                keep the saved credential.
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
              {formError && (
                <p className="text-sm text-destructive">{formError}</p>
              )}
            </div>
            <DialogFooter>
              <Button type="submit" disabled={saving}>
                {saving ? "Testing connection…" : "Test and save"}
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
