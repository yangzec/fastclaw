"use client";

import { useState, useCallback, useEffect } from "react";
import { useRouter } from "next/navigation";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
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
  ArrowLeft,
  ArrowRight,
  Check,
  ChevronDown,
  KeyRound,
  Loader2,
  MessageSquare,
  Sparkles,
  UserPlus,
} from "lucide-react";
import { getStatus, onboard, testProvider } from "@/lib/api";
import { firstAgentChatPath } from "@/lib/first-chat";
import { resolveFirstChatPath } from "@/lib/first-chat-nav";
import { MIN_PASSWORD_LENGTH } from "@/lib/password";
import { PROVIDER_PRESETS, PROVIDER_LABELS } from "@/lib/provider-presets";
import { nextContextWindowOnIdChange, nextMaxTokensOnIdChange, presetContextWindow, presetMaxTokens } from "@/lib/model-defaults";
import { ModelLimitsFields } from "@/components/model-limits-fields";

const STEPS = [
  { id: "account", label: "Account", icon: UserPlus },
  { id: "model", label: "Model", icon: KeyRound },
  { id: "ready", label: "Ready", icon: Sparkles },
] as const;

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
  const [step, setStep] = useState(0);
  const [agentId, setAgentId] = useState("");

  useEffect(() => {
    let cancelled = false;
    getStatus()
      .then(async (s) => {
        if (cancelled || !s?.configured) return;
        router.replace(await resolveFirstChatPath());
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [router]);

  const [username, setUsername] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [passwordConfirm, setPasswordConfirm] = useState("");

  const [providerEnabled, setProviderEnabled] = useState(true);
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

  async function handleTest() {
    if (!apiKey) {
      setTestStatus("fail");
      setTestError("API key required");
      return;
    }
    setTestStatus("running");
    setTestError("");
    const res = await testProvider({ apiBase, apiKey, model, apiType, authType });
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
    const usingProvider = providerEnabled && !skipProvider;
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
      setStep(0);
      return;
    }
    setAgentId(res.agentId || "");
    setProviderEnabled(usingProvider);
    setStep(STEPS.length - 1);
  }

  const accountValid =
    username.trim() !== "" &&
    email.trim() !== "" &&
    password.length >= MIN_PASSWORD_LENGTH &&
    password === passwordConfirm;
  const modelValid =
    !providerEnabled ||
    (apiKey.trim() !== "" && model.trim() !== "" && apiBase.trim() !== "");

  return (
    <div className="flex min-h-screen items-center justify-center bg-muted/30 p-4">
      <div className="w-full max-w-xl space-y-6">
        <Stepper current={step} />

        {step === 0 && (
          <AccountStep
            username={username}
            setUsername={setUsername}
            email={email}
            setEmail={setEmail}
            password={password}
            setPassword={setPassword}
            passwordConfirm={passwordConfirm}
            setPasswordConfirm={setPasswordConfirm}
          />
        )}

        {step === 1 && (
          <ModelStep
            providerKey={providerKey}
            onProviderChange={handleProviderChange}
            providerName={providerName}
            setProviderName={setProviderName}
            apiBase={apiBase}
            setApiBase={setApiBase}
            apiKey={apiKey}
            setApiKey={setApiKey}
            apiType={apiType}
            setApiType={setApiType}
            authType={authType}
            setAuthType={setAuthType}
            model={model}
            setModel={(next) => {
              setContextWindow((prev) => nextContextWindowOnIdChange(next, model, prev));
              setMaxTokens((prev) => nextMaxTokensOnIdChange(next, model, prev));
              setModel(next);
            }}
            contextWindow={contextWindow}
            setContextWindow={setContextWindow}
            maxTokens={maxTokens}
            setMaxTokens={setMaxTokens}
            showAdvanced={showAdvanced}
            setShowAdvanced={setShowAdvanced}
            onTest={handleTest}
            testStatus={testStatus}
            testError={testError}
          />
        )}

        {step === 2 && (
          <DoneStep
            providerConfigured={providerEnabled}
            onContinue={() =>
              router.replace(firstAgentChatPath([{ id: agentId }]) ?? "/overview/")
            }
          />
        )}

        {submitError && (
          <Card className="border-destructive/40 bg-destructive/5">
            <CardContent className="pt-6">
              <p className="text-sm text-destructive">{submitError}</p>
            </CardContent>
          </Card>
        )}

        {step !== STEPS.length - 1 && (
          <div className="flex items-center justify-between gap-3">
            <Button
              variant="ghost"
              onClick={() => setStep((s) => Math.max(0, s - 1))}
              disabled={step === 0}
            >
              <ArrowLeft className="mr-1 size-4" /> Back
            </Button>
            {step === 0 ? (
              <Button onClick={() => setStep(1)} disabled={!accountValid}>
                Next <ArrowRight className="ml-1 size-4" />
              </Button>
            ) : (
              <div className="flex items-center gap-2">
                <Button
                  variant="ghost"
                  onClick={() => handleSubmit(true)}
                  disabled={submitting}
                >
                  Skip
                </Button>
                <Button
                  onClick={() => handleSubmit(false)}
                  disabled={!modelValid || submitting}
                >
                  {submitting ? (
                    <>
                      <Loader2 className="mr-1 size-4 animate-spin" /> Setting up
                    </>
                  ) : (
                    <>
                      Create &amp; chat <Sparkles className="ml-1 size-4" />
                    </>
                  )}
                </Button>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

function Stepper({ current }: { current: number }) {
  return (
    <ol className="flex items-center gap-2">
      {STEPS.map((s, i) => {
        const Icon = s.icon;
        const done = i < current;
        const active = i === current;
        return (
          <li key={s.id} className="flex flex-1 items-center gap-2">
            <div
              className={
                "flex size-8 shrink-0 items-center justify-center rounded-full border transition " +
                (done
                  ? "border-primary bg-primary text-primary-foreground"
                  : active
                    ? "border-primary text-primary"
                    : "border-border text-muted-foreground")
              }
            >
              {done ? <Check className="size-4" /> : <Icon className="size-4" />}
            </div>
            <span
              className={
                "hidden text-sm sm:inline " +
                (active
                  ? "font-medium"
                  : done
                    ? "text-muted-foreground"
                    : "text-muted-foreground/60")
              }
            >
              {s.label}
            </span>
            {i < STEPS.length - 1 && (
              <div
                className={
                  "h-px flex-1 " + (i < current ? "bg-primary" : "bg-border")
                }
              />
            )}
          </li>
        );
      })}
    </ol>
  );
}

function AccountStep(props: {
  username: string;
  setUsername: (v: string) => void;
  email: string;
  setEmail: (v: string) => void;
  password: string;
  setPassword: (v: string) => void;
  passwordConfirm: string;
  setPasswordConfirm: (v: string) => void;
}) {
  const passwordTooShort =
    props.password.length > 0 && props.password.length < MIN_PASSWORD_LENGTH;
  const mismatch =
    props.passwordConfirm.length > 0 && props.password !== props.passwordConfirm;
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <UserPlus className="size-5 text-primary" />
          Create your account
        </CardTitle>
        <CardDescription>
          Then paste an API key and start talking. Sandbox, skills, and extra
          models can wait.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="grid gap-3 sm:grid-cols-2">
          <div className="space-y-1.5">
            <Label htmlFor="ob-username">Username</Label>
            <Input
              id="ob-username"
              value={props.username}
              onChange={(e) => props.setUsername(e.target.value)}
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
              value={props.email}
              onChange={(e) => props.setEmail(e.target.value)}
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
              value={props.password}
              onChange={(e) => props.setPassword(e.target.value)}
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
              value={props.passwordConfirm}
              onChange={(e) => props.setPasswordConfirm(e.target.value)}
              autoComplete="new-password"
            />
            {mismatch && (
              <p className="text-xs text-destructive">passwords don&apos;t match</p>
            )}
          </div>
        </div>
      </CardContent>
    </Card>
  );
}

function ModelStep(props: {
  providerKey: string;
  onProviderChange: (v: string) => void;
  providerName: string;
  setProviderName: (v: string) => void;
  apiBase: string;
  setApiBase: (v: string) => void;
  apiKey: string;
  setApiKey: (v: string) => void;
  apiType: string;
  setApiType: (v: string) => void;
  authType: string;
  setAuthType: (v: string) => void;
  model: string;
  setModel: (v: string) => void;
  contextWindow: number;
  setContextWindow: (v: number) => void;
  maxTokens: number;
  setMaxTokens: (v: number) => void;
  showAdvanced: boolean;
  setShowAdvanced: (v: boolean) => void;
  onTest: () => void;
  testStatus: "" | "ok" | "fail" | "running";
  testError: string;
}) {
  const preset = PROVIDER_PRESETS[props.providerKey];
  const isCustom = props.providerKey === "custom";
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <KeyRound className="size-5 text-primary" />
          Paste an API key
        </CardTitle>
        <CardDescription>
          Pick a provider. The model is already chosen. Skip if you want to add
          this later.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="space-y-1.5">
          <Label>Provider</Label>
          <Select
            value={props.providerKey}
            onValueChange={(v) => v && props.onProviderChange(v)}
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
                value={props.providerName}
                onChange={(e) => props.setProviderName(e.target.value)}
                placeholder="my-llm"
                className="font-mono text-sm"
              />
            </div>
            <div className="space-y-1.5">
              <Label>API Base URL</Label>
              <Input
                value={props.apiBase}
                onChange={(e) => props.setApiBase(e.target.value)}
                className="font-mono text-sm"
              />
            </div>
          </div>
        )}

        <div className="space-y-1.5">
          <Label>API Key</Label>
          <Input
            type="password"
            value={props.apiKey}
            onChange={(e) => props.setApiKey(e.target.value)}
            placeholder="sk-…"
            className="font-mono text-sm"
            autoFocus
          />
        </div>

        <div className="space-y-1.5">
          <Label>Model</Label>
          <Input
            value={props.model}
            onChange={(e) => props.setModel(e.target.value)}
            placeholder={preset?.models[0] || "model-id"}
            className="font-mono text-sm"
          />
        </div>

        <div className="flex items-center gap-3">
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={props.onTest}
            disabled={props.testStatus === "running" || !props.apiKey}
          >
            {props.testStatus === "running" ? (
              <>
                <Loader2 className="mr-1 size-4 animate-spin" /> Testing
              </>
            ) : (
              "Test connection"
            )}
          </Button>
          {props.testStatus === "ok" && (
            <Badge className="bg-emerald-500/15 text-emerald-700 hover:bg-emerald-500/15">
              <Check className="mr-1 size-3" /> connected
            </Badge>
          )}
          {props.testStatus === "fail" && (
            <span className="text-xs text-destructive">{props.testError}</span>
          )}
        </div>

        <button
          type="button"
          className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
          onClick={() => props.setShowAdvanced(!props.showAdvanced)}
        >
          <ChevronDown
            className={
              "size-3.5 transition-transform " + (props.showAdvanced ? "rotate-180" : "")
            }
          />
          Advanced
        </button>
        {props.showAdvanced && (
          <div className="space-y-4 rounded-lg border border-border p-3">
            {!isCustom && (
              <div className="space-y-1.5">
                <Label>API Base URL</Label>
                <Input
                  value={props.apiBase}
                  onChange={(e) => props.setApiBase(e.target.value)}
                  className="font-mono text-sm"
                />
              </div>
            )}
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label>API Type</Label>
                <Select value={props.apiType} onValueChange={(v) => v && props.setApiType(v)}>
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
                <Select value={props.authType} onValueChange={(v) => v && props.setAuthType(v)}>
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
              modelId={props.model}
              contextWindow={props.contextWindow}
              maxTokens={props.maxTokens}
              onContextWindowChange={props.setContextWindow}
              onMaxTokensChange={props.setMaxTokens}
            />
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function DoneStep({
  onContinue,
  providerConfigured,
}: {
  onContinue: () => void;
  providerConfigured: boolean;
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <MessageSquare className="size-5 text-emerald-500" />
          Your agent is ready
        </CardTitle>
        <CardDescription>
          {providerConfigured
            ? "Say anything. You can add skills, channels, and a sandbox later."
            : "Add a model from Models when you want to chat. Your agent is already waiting."}
        </CardDescription>
      </CardHeader>
      <CardFooter>
        <Button onClick={onContinue} className="w-full">
          Start chatting <ArrowRight className="ml-1 size-4" />
        </Button>
      </CardFooter>
    </Card>
  );
}
