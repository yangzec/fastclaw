"use client";

import { useState, useCallback, useEffect } from "react";
import { useRouter } from "next/navigation";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Check,
  ChevronDown,
  Loader2,
  MessageSquare,
} from "lucide-react";
import { getStatus, onboard, testProvider, type StatusResponse } from "@/lib/api";
import { firstAgentChatPath } from "@/lib/first-chat";
import { resolveFirstChatPath } from "@/lib/first-chat-nav";
import { MIN_PASSWORD_LENGTH } from "@/lib/password";
import { PROVIDER_PRESETS, PROVIDER_LABELS } from "@/lib/provider-presets";
import { nextContextWindowOnIdChange, nextMaxTokensOnIdChange, presetContextWindow, presetMaxTokens } from "@/lib/model-defaults";
import { ModelLimitsFields } from "@/components/model-limits-fields";

const API_TYPE_LABELS: Record<string, string> = {
  "openai-chat": "OpenAI Chat Completions",
  "anthropic-messages": "Anthropic Messages",
};

const AUTH_TYPE_LABELS: Record<string, string> = {
  "bearer-token": "Bearer Token",
  "api-key": "API Key Header",
};

export default function OnboardPage() {
  const router = useRouter();
  const [envProvider, setEnvProvider] = useState<StatusResponse["envProvider"]>();
  const [username, setUsername] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [passwordConfirm, setPasswordConfirm] = useState("");

  const [providerKey, setProviderKey] = useState("openai");
  const [providerName, setProviderName] = useState("openai");
  const [apiBase, setApiBase] = useState(PROVIDER_PRESETS.openai.apiBase);
  const [apiKey, setApiKey] = useState("");
  const [apiType, setApiType] = useState(PROVIDER_PRESETS.openai.apiType);
  const [authType, setAuthType] = useState("bearer-token");
  const [model, setModel] = useState(PROVIDER_PRESETS.openai.models[0]);
  const [contextWindow, setContextWindow] = useState(
    presetContextWindow(PROVIDER_PRESETS.openai.models[0]),
  );
  const [maxTokens, setMaxTokens] = useState(
    presetMaxTokens(PROVIDER_PRESETS.openai.models[0]),
  );
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [testStatus, setTestStatus] = useState<"" | "ok" | "fail" | "running">("");
  const [testError, setTestError] = useState("");

  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState("");

  const handleProviderChange = useCallback((next: string) => {
    setProviderKey(next);
    const preset = PROVIDER_PRESETS[next];
    if (preset) {
      setApiBase(preset.apiBase);
      setApiType(preset.apiType);
      setAuthType(preset.authType);
      if (preset.models[0]) {
        setModel(preset.models[0]);
        setContextWindow(presetContextWindow(preset.models[0]));
        setMaxTokens(presetMaxTokens(preset.models[0]));
      }
    }
    setProviderName(next === "custom" ? "" : next);
    setTestStatus("");
    setTestError("");
  }, []);

  useEffect(() => {
    let cancelled = false;
    getStatus()
      .then(async (s) => {
        if (cancelled) return;
        if (s?.configured) {
          router.replace(await resolveFirstChatPath());
          return;
        }
        if (s?.envProvider) {
          setEnvProvider(s.envProvider);
          handleProviderChange(s.envProvider.name);
        }
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [router, handleProviderChange]);

  const usingEnvKey =
    !!envProvider && envProvider.name === providerKey && apiKey.trim() === "";

  async function handleTest() {
    const key = apiKey || (usingEnvKey ? "env" : "");
    if (!apiKey && !usingEnvKey) {
      setTestStatus("fail");
      setTestError("API key required");
      return;
    }
    if (!apiKey && usingEnvKey) {
      // The browser cannot read the process env; skip client test and
      // let submit persist the server-side key.
      setTestStatus("ok");
      return;
    }
    setTestStatus("running");
    setTestError("");
    const res = await testProvider({ apiBase, apiKey: key, model, apiType, authType });
    if (res.ok) {
      setTestStatus("ok");
    } else {
      setTestStatus("fail");
      setTestError(res.error || "test failed");
    }
  }

  async function handleSubmit(skipProvider: boolean) {
    setSubmitError("");
    setSubmitting(true);
    const usingProvider = !skipProvider;
    const finalProviderName =
      providerName.trim().toLowerCase().replace(/\s+/g, "-") || providerKey;
    const res = await onboard({
      username,
      email,
      password,
      provider: usingProvider ? finalProviderName : "",
      apiBase: usingProvider ? apiBase : "",
      apiKey: usingProvider ? apiKey : "",
      apiType: usingProvider ? apiType : "",
      authType: usingProvider ? authType : "",
      model: usingProvider ? model : "",
      contextWindow: usingProvider ? contextWindow : undefined,
      maxTokens: usingProvider ? maxTokens : undefined,
      agentName: "Assistant",
    });
    setSubmitting(false);
    if (!res.ok) {
      setSubmitError(res.error || "setup failed");
      return;
    }
    router.replace(firstAgentChatPath([{ id: res.agentId }]) ?? "/overview/");
  }

  const accountValid =
    username.trim() !== "" &&
    email.trim() !== "" &&
    password.length >= MIN_PASSWORD_LENGTH &&
    password === passwordConfirm;
  const modelValid =
    usingEnvKey ||
    (apiKey.trim() !== "" && model.trim() !== "" && apiBase.trim() !== "");
  const canStart = accountValid && modelValid;

  const passwordTooShort =
    password.length > 0 && password.length < MIN_PASSWORD_LENGTH;
  const mismatch = passwordConfirm.length > 0 && password !== passwordConfirm;
  const preset = PROVIDER_PRESETS[providerKey];
  const isCustom = providerKey === "custom";

  return (
    <div className="flex min-h-screen items-center justify-center bg-muted/30 p-4">
      <div className="w-full max-w-xl">
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <MessageSquare className="size-5 text-primary" />
              Start talking
            </CardTitle>
            <CardDescription>
              {usingEnvKey
                ? `Found ${envProvider?.env} on this machine. Create an account and go.`
                : "Account, then an API key. Everything else can wait."}
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-5">
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label htmlFor="ob-username">Username</Label>
                <Input
                  id="ob-username"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  autoComplete="username"
                  placeholder="alice"
                  autoFocus
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="ob-email">Email</Label>
                <Input
                  id="ob-email"
                  type="email"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  autoComplete="email"
                  placeholder="alice@example.com"
                />
              </div>
            </div>
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label htmlFor="ob-password">Password</Label>
                <Input
                  id="ob-password"
                  type="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  autoComplete="new-password"
                  placeholder={`${MIN_PASSWORD_LENGTH}+ characters`}
                />
                {passwordTooShort && (
                  <p className="text-xs text-destructive">at least {MIN_PASSWORD_LENGTH} characters</p>
                )}
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="ob-password2">Confirm</Label>
                <Input
                  id="ob-password2"
                  type="password"
                  value={passwordConfirm}
                  onChange={(e) => setPasswordConfirm(e.target.value)}
                  autoComplete="new-password"
                />
                {mismatch && (
                  <p className="text-xs text-destructive">passwords don&apos;t match</p>
                )}
              </div>
            </div>

            <div className="space-y-1.5">
              <Label>Provider</Label>
              <Select
                value={providerKey}
                onValueChange={(v) => v && handleProviderChange(v)}
              >
                <SelectTrigger className="w-full">
                  <SelectValue>
                    {(v: unknown) => PROVIDER_LABELS[v as string] ?? (v as string) ?? ""}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {Object.keys(PROVIDER_PRESETS).map((p) => (
                    <SelectItem key={p} value={p}>
                      {PROVIDER_LABELS[p] ?? p}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            {isCustom && (
              <div className="grid gap-3 sm:grid-cols-2">
                <div className="space-y-1.5">
                  <Label>Name</Label>
                  <Input
                    value={providerName}
                    onChange={(e) => setProviderName(e.target.value)}
                    placeholder="my-llm"
                    className="font-mono text-sm"
                  />
                </div>
                <div className="space-y-1.5">
                  <Label>API Base URL</Label>
                  <Input
                    value={apiBase}
                    onChange={(e) => setApiBase(e.target.value)}
                    className="font-mono text-sm"
                  />
                </div>
              </div>
            )}

            <div className="space-y-1.5">
              <Label>API Key</Label>
              <Input
                type="password"
                value={apiKey}
                onChange={(e) => setApiKey(e.target.value)}
                placeholder={usingEnvKey ? `using ${envProvider?.env}` : "sk-…"}
                className="font-mono text-sm"
              />
              {usingEnvKey && (
                <p className="text-xs text-muted-foreground">
                  Leave blank to use the key already exported on this machine.
                </p>
              )}
            </div>

            <div className="space-y-1.5">
              <Label>Model</Label>
              <Input
                value={model}
                onChange={(e) => {
                  setContextWindow((prev) => nextContextWindowOnIdChange(e.target.value, model, prev));
                  setMaxTokens((prev) => nextMaxTokensOnIdChange(e.target.value, model, prev));
                  setModel(e.target.value);
                }}
                placeholder={preset?.models[0] || "model-id"}
                className="font-mono text-sm"
              />
            </div>

            <div className="flex items-center gap-3">
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={handleTest}
                disabled={testStatus === "running" || (!apiKey && !usingEnvKey)}
              >
                {testStatus === "running" ? (
                  <>
                    <Loader2 className="mr-1 size-4 animate-spin" /> Testing
                  </>
                ) : (
                  "Test connection"
                )}
              </Button>
              {testStatus === "ok" && (
                <Badge className="bg-emerald-500/15 text-emerald-700 hover:bg-emerald-500/15">
                  <Check className="mr-1 size-3" /> connected
                </Badge>
              )}
              {testStatus === "fail" && (
                <span className="text-xs text-destructive">{testError}</span>
              )}
            </div>

            <button
              type="button"
              className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
              onClick={() => setShowAdvanced(!showAdvanced)}
            >
              <ChevronDown
                className={
                  "size-3.5 transition-transform " + (showAdvanced ? "rotate-180" : "")
                }
              />
              Advanced
            </button>
            {showAdvanced && (
              <div className="space-y-4 rounded-lg border border-border p-3">
                {!isCustom && (
                  <div className="space-y-1.5">
                    <Label>API Base URL</Label>
                    <Input
                      value={apiBase}
                      onChange={(e) => setApiBase(e.target.value)}
                      className="font-mono text-sm"
                    />
                  </div>
                )}
                <div className="grid gap-3 sm:grid-cols-2">
                  <div className="space-y-1.5">
                    <Label>API Type</Label>
                    <Select value={apiType} onValueChange={(v) => v && setApiType(v)}>
                      <SelectTrigger className="w-full">
                        <SelectValue>
                          {(v: unknown) => API_TYPE_LABELS[v as string] ?? (v as string) ?? ""}
                        </SelectValue>
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="openai-chat">OpenAI Chat Completions</SelectItem>
                        <SelectItem value="anthropic-messages">Anthropic Messages</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                  <div className="space-y-1.5">
                    <Label>Auth Type</Label>
                    <Select value={authType} onValueChange={(v) => v && setAuthType(v)}>
                      <SelectTrigger className="w-full">
                        <SelectValue>
                          {(v: unknown) => AUTH_TYPE_LABELS[v as string] ?? (v as string) ?? ""}
                        </SelectValue>
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="bearer-token">Bearer Token</SelectItem>
                        <SelectItem value="api-key">API Key Header</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                </div>
                <ModelLimitsFields
                  modelId={model}
                  contextWindow={contextWindow}
                  maxTokens={maxTokens}
                  onContextWindowChange={setContextWindow}
                  onMaxTokensChange={setMaxTokens}
                />
              </div>
            )}

            {submitError && (
              <p className="text-sm text-destructive">{submitError}</p>
            )}

            <div className="flex items-center justify-between gap-3 pt-1">
              <Button
                variant="ghost"
                onClick={() => handleSubmit(true)}
                disabled={!accountValid || submitting}
              >
                Skip key
              </Button>
              <Button
                onClick={() => handleSubmit(false)}
                disabled={!canStart || submitting}
              >
                {submitting ? (
                  <>
                    <Loader2 className="mr-1 size-4 animate-spin" /> Setting up
                  </>
                ) : (
                  "Start chatting"
                )}
              </Button>
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
