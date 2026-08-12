import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router";
import { LoaderCircle, RefreshCw, ShieldCheck } from "lucide-react";
import { Badge } from "@workspace/ui/components/badge";
import { Button } from "@workspace/ui/components/button";
import { Card } from "@workspace/ui/components/card";
import { controlApi, type Run } from "../../../lib/control-api";
import { useTranslations } from "use-intl";

export default function ApprovalsPage() {
  const t = useTranslations("platform.approvals");
  const [runs, setRuns] = useState<Run[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);

  const load = useCallback(() => {
    void controlApi
      .runs()
      .then((items) => {
        setRuns(items);
        setError(false);
      })
      .catch(() => setError(true))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    load();
    const t = setInterval(load, 3000);
    return () => clearInterval(t);
  }, [load]);

  const pending = runs.filter((r) => r.status === "waiting_approval");

  return (
    <div className="space-y-4">
      <h1 className="text-xl font-semibold">{t("title")}</h1>
      <p className="text-sm text-muted-foreground">{t("description")}</p>
      <div className="space-y-2">
        {loading && (
          <div
            className="flex min-h-28 items-center justify-center gap-2 text-sm text-muted-foreground"
            role="status"
          >
            <LoaderCircle className="size-4 animate-spin" />
            {t("loading")}
          </div>
        )}
        {!loading && error && (
          <div
            className="flex min-h-28 flex-col items-center justify-center gap-3 rounded-md border border-dashed text-sm text-muted-foreground"
            role="alert"
          >
            <span>{t("loadFailed")}</span>
            <Button variant="outline" className="min-h-11 gap-2" onClick={load}>
              <RefreshCw className="size-4" />
              {t("retry")}
            </Button>
          </div>
        )}
        {!loading &&
          !error &&
          pending.map((r) => (
            <Link
              key={r.id}
              to={`/workflow/runs/${r.id}`}
              className="block rounded-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
            >
              <Card className="p-3 text-sm transition-colors hover:bg-accent">
                <div className="flex items-center gap-2">
                  <ShieldCheck
                    className="size-4 shrink-0 text-amber-600 dark:text-amber-400"
                    aria-hidden="true"
                  />
                  <span className="font-medium">{r.id}</span>
                  <Badge>{t("pending")}</Badge>
                  <span className="ml-auto hidden text-muted-foreground sm:inline">
                    {r.createdAt}
                  </span>
                </div>
              </Card>
            </Link>
          ))}
        {!loading && !error && pending.length === 0 && (
          <div className="grid min-h-28 place-items-center rounded-md border border-dashed text-sm text-muted-foreground">
            {t("empty")}
          </div>
        )}
      </div>
    </div>
  );
}
