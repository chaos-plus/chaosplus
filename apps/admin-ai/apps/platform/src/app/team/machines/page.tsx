import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router";
import {
  CircleCheck,
  Clock3,
  Copy,
  LoaderCircle,
  Plus,
  RefreshCw,
} from "lucide-react";
import { useTranslations } from "use-intl";
import { Badge } from "@workspace/ui/components/badge";
import { Button } from "@workspace/ui/components/button";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@workspace/ui/components/card";
import { toast } from "@workspace/ui/components/sonner";
import {
  controlApi,
  machineConnectCommand,
  type Machine,
} from "../../../lib/control-api";

export default function MachinesPage() {
  const t = useTranslations("platform");
  const [machines, setMachines] = useState<Machine[]>([]);
  const [wizard, setWizard] = useState<{
    token: string;
    machineId: string;
    expiresAt: number;
  } | null>(null);
  const [onboardingState, setOnboardingState] = useState<
    "waiting" | "connected" | "confirmed" | "expired" | "invalid"
  >("waiting");
  const [busy, setBusy] = useState(false);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState(false);
  const [now, setNow] = useState(() => Date.now());

  const load = useCallback(async (quiet = false) => {
    if (!quiet) setLoading(true);
    try {
      setMachines(await controlApi.machines());
      setLoadError(false);
    } catch {
      setLoadError(true);
    } finally {
      if (!quiet) setLoading(false);
    }
  }, []);

  useEffect(() => {
    const initial = window.setTimeout(() => void load(), 0);
    const timer = setInterval(() => void load(true), 5000);
    return () => {
      window.clearTimeout(initial);
      clearInterval(timer);
    };
  }, [load]);

  useEffect(() => {
    if (!wizard) return;
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [wizard]);

  const wizardMachineId = wizard?.machineId;
  const wizardToken = wizard?.token;

  useEffect(() => {
    if (!wizardMachineId || !wizardToken) return;
    let active = true;
    const check = async () => {
      try {
        const status = await controlApi.onboardingStatus(
          wizardMachineId,
          wizardToken,
        );
        if (!active) return;
        setOnboardingState(status.state);
        if (status.expiresAt)
          setWizard((current) =>
            current ? { ...current, expiresAt: status.expiresAt! } : current,
          );
      } catch {
        if (active) setOnboardingState("invalid");
      }
    };
    void check();
    const timer = setInterval(() => void check(), 2000);
    return () => {
      active = false;
      clearInterval(timer);
    };
  }, [wizardMachineId, wizardToken]);

  const remainingSeconds = wizard
    ? Math.max(0, Math.ceil((wizard.expiresAt - now) / 1000))
    : 0;
  const visibleState = remainingSeconds === 0 ? "expired" : onboardingState;

  const startWizard = async () => {
    let stage: "cancel" | "issue" = wizard ? "cancel" : "issue";
    setBusy(true);
    try {
      if (wizard) {
        await controlApi.cancelMachine(wizard.machineId);
        stage = "issue";
      }
      setWizard(await controlApi.issueToken());
      setOnboardingState("waiting");
      setNow(Date.now());
    } catch (cause) {
      toast.error(
        `${t(stage === "cancel" ? "machines.cancelFailed" : "machines.issueFailed")}: ${cause instanceof Error ? cause.message : String(cause)}`,
      );
    } finally {
      setBusy(false);
    }
  };

  const confirm = async () => {
    if (!wizard) return;
    setBusy(true);
    try {
      await controlApi.confirmMachine(wizard.machineId, wizard.token);
      toast.success(t("machines.confirmed"));
      setWizard(null);
      await load(true);
    } catch (cause) {
      toast.error(
        `${t("machines.confirmFailed")}: ${cause instanceof Error ? cause.message : String(cause)}`,
      );
    } finally {
      setBusy(false);
    }
  };

  const cancel = async () => {
    if (!wizard) return;
    setBusy(true);
    try {
      await controlApi.cancelMachine(wizard.machineId);
      setWizard(null);
    } catch (cause) {
      toast.error(
        `${t("machines.cancelFailed")}: ${cause instanceof Error ? cause.message : String(cause)}`,
      );
    } finally {
      setBusy(false);
    }
  };

  const copyCommand = () => {
    if (!wizard) return;
    void navigator.clipboard
      .writeText(machineConnectCommand(wizard.token))
      .then(
        () => toast.success(t("machines.copied")),
        () => toast.error(t("machines.copyFailed")),
      );
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold">{t("nav.machines")}</h1>
        <Button
          onClick={startWizard}
          disabled={busy}
          className="min-h-11 gap-2"
        >
          {busy ? (
            <LoaderCircle className="size-4 animate-spin" />
          ) : (
            <Plus className="size-4" />
          )}
          {t("machines.add")}
        </Button>
      </div>

      {wizard && (
        <Card className="border-primary/50">
          <CardHeader>
            <CardTitle className="text-sm">
              {t("machines.wizardTitle")}
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <pre className="overflow-x-auto rounded-md bg-muted p-3 text-xs">
              {machineConnectCommand(wizard.token)}
            </pre>
            <div
              className="flex items-center gap-2 text-sm"
              role="status"
              aria-live="polite"
            >
              {visibleState === "connected" ? (
                <CircleCheck className="size-4 text-emerald-600 dark:text-emerald-400" />
              ) : visibleState === "waiting" ? (
                <LoaderCircle className="size-4 animate-spin text-muted-foreground" />
              ) : (
                <Clock3 className="size-4 text-destructive" />
              )}
              <span>{t(`machines.onboarding.${visibleState}`)}</span>
              {visibleState !== "expired" && visibleState !== "invalid" && (
                <span className="tabular-nums text-muted-foreground">
                  {t("machines.expiresIn", { seconds: remainingSeconds })}
                </span>
              )}
            </div>
            <div className="flex flex-wrap gap-2">
              <Button
                variant="outline"
                className="min-h-11 cursor-pointer gap-1.5"
                onClick={copyCommand}
                disabled={
                  visibleState === "expired" || visibleState === "invalid"
                }
              >
                <Copy className="size-4" />
                {t("machines.copyCommand")}
              </Button>
              <Button
                onClick={confirm}
                disabled={busy || visibleState !== "connected"}
                className="min-h-11 gap-2"
              >
                {busy && <LoaderCircle className="size-4 animate-spin" />}
                {t("machines.confirm")}
              </Button>
              <Button
                variant="outline"
                onClick={cancel}
                disabled={busy}
                className="min-h-11"
              >
                {t("common.cancel")}
              </Button>
              {(visibleState === "expired" || visibleState === "invalid") && (
                <Button
                  onClick={startWizard}
                  disabled={busy}
                  className="min-h-11 gap-2"
                >
                  <RefreshCw className="size-4" />
                  {t("machines.refreshCommand")}
                </Button>
              )}
            </div>
          </CardContent>
        </Card>
      )}

      {loading && (
        <div
          className="flex min-h-32 items-center justify-center text-sm text-muted-foreground"
          role="status"
        >
          <LoaderCircle className="mr-2 size-4 animate-spin" />
          {t("common.loading")}
        </div>
      )}
      {!loading && loadError && (
        <div
          className="flex min-h-32 flex-col items-center justify-center gap-3 rounded-md border border-dashed text-sm text-muted-foreground"
          role="alert"
        >
          <span>{t("machines.loadFailed")}</span>
          <Button
            variant="outline"
            className="min-h-11 gap-2"
            onClick={() => void load()}
          >
            <RefreshCw className="size-4" />
            {t("common.retry")}
          </Button>
        </div>
      )}
      {!loading && !loadError && machines.length === 0 && (
        <div className="flex min-h-32 items-center justify-center rounded-md border border-dashed text-sm text-muted-foreground">
          {t("machines.empty")}
        </div>
      )}
      {!loading && !loadError && (
        <div className="grid gap-3 sm:grid-cols-2">
          {machines.map((m) => (
            <Link
              key={m.id}
              to={`/team/machines/${m.id}`}
              className="rounded-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
            >
              <Card className="h-full cursor-pointer transition-colors hover:border-primary/40 hover:bg-accent/30">
                <CardHeader>
                  <CardTitle className="flex items-center justify-between text-base">
                    <span>{m.name}</span>
                    <Badge variant={m.online ? "default" : "secondary"}>
                      {m.online ? t("dashboard.online") : t("machines.offline")}
                    </Badge>
                  </CardTitle>
                </CardHeader>
                <CardContent className="space-y-1 text-sm text-muted-foreground">
                  <div>id: {m.id}</div>
                  <div>
                    {t("machines.address")}: {m.address}
                  </div>
                  <div>
                    {t("machines.agentCount")}: {m.agentCount}
                  </div>
                  <div>
                    {t("dashboard.lastHeartbeat")}:{" "}
                    {m.lastHeartbeatAt
                      ? new Date(m.lastHeartbeatAt).toLocaleTimeString()
                      : "—"}
                  </div>
                  <div className="pt-1 text-xs text-primary">
                    {t("machines.viewDetail")} →
                  </div>
                </CardContent>
              </Card>
            </Link>
          ))}
        </div>
      )}
    </div>
  );
}
