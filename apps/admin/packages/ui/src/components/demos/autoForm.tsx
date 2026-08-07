import { useState } from "react"
import { toast } from "sonner"
import { useTranslations } from "use-intl"

import { Button } from "../button"
import { Badge } from "../badge"
import { AutoForm } from "../auto-form/auto-form"
import type {
  FormConfig,
  FormMode,
  FieldRendererProps,
} from "../auto-form/types"
import { MapPicker } from "@workspace/ui/components/pickers/map-picker"
import type { MapLocation } from "@workspace/ui/components/pickers/google-map-picker"
import { cn } from "@workspace/ui/lib/utils"
import { DemoSection } from "./_section"

const MODES: FormMode[] = ["add", "edit", "detail", "copy"]

/** Adapts MapPicker into a custom AutoForm field renderer (defaults to OSM, works zero-config) */
function MapField({ value, onChange, disabled }: FieldRendererProps) {
  const tm = useTranslations("showcase.demos.mapPicker")
  return (
    <MapPicker
      provider="osm"
      value={(value as MapLocation | null) ?? null}
      onChange={(loc) => onChange(loc)}
      readonly={disabled}
      height={260}
      searchPlaceholder={tm("searchPlaceholder")}
      missingKeyHint={tm("missingKey")}
    />
  )
}

export function AutoFormDemo() {
  const t = useTranslations("showcase.demos.autoForm")
  const [mode, setMode] = useState<FormMode>("add")

  // Includes a copy:false unique field (code) to show that copy mode clears it
  const crudFields: FormConfig = {
    code: {
      name: t("code"),
      type: "text",
      copy: false,
      config: { placeholder: "USR-XXXX" },
    },
    username: {
      name: t("username"),
      type: "text",
      required: true,
    },
    role: {
      name: t("role"),
      type: "select",
      value: "viewer",
      config: {
        options: [
          { value: "admin", label: t("admin") },
          { value: "editor", label: t("editor") },
          { value: "viewer", label: t("viewer") },
        ],
      },
    },
    active: {
      name: t("active"),
      type: "switch",
      value: true,
    },
    note: {
      name: t("note"),
      type: "textarea",
      config: { placeholder: t("notePlaceholder") },
    },
    location: {
      name: t("location"),
      type: "custom",
      component: MapField,
    },
  }

  const sampleRecord = {
    code: "USR-1001",
    username: "alice_chen",
    role: "admin",
    active: true,
    note: "Senior administrator account.",
    location: {
      latitude: 39.9042,
      longitude: 116.4074,
      address: "Beijing, China",
      name: "Head Office",
    } as MapLocation,
  }

  const modeDesc: Record<FormMode, string> = {
    add: t("modeDescAdd"),
    edit: t("modeDescEdit"),
    detail: t("modeDescDetail"),
    copy: t("modeDescCopy"),
  }

  const fields: FormConfig = {
    ruleType: {
      name: t("ruleType"),
      type: "tabs",
      value: "NONE",
      required: true,
      config: {
        tabs: { NONE: t("none"), FIXED: t("fixed"), RATIO: t("ratio") },
      },
    },
    fixedValue: {
      name: t("fixedValue"),
      type: "text",
      required: true,
      show: (values) => values?.ruleType === "FIXED",
      config: { prefix: t("give"), suffix: t("coin") },
    },
    ratioValue: {
      name: t("ratioValue"),
      type: "text",
      required: true,
      show: (values) => values?.ruleType === "RATIO",
      config: { prefix: t("give"), suffix: t("coinPerCent") },
    },
  }

  const scheduleFields: FormConfig = {
    visitDate: {
      name: "Date",
      type: "date",
      value: "2026-06-18",
      config: { placeholder: "Select date" },
    },
    reminderTime: {
      name: "Time",
      type: "time",
      value: "09:30",
    },
    eventAt: {
      name: "Date time",
      type: "datetime",
      value: "2026-06-18T09:30",
      config: { placeholder: "Select date" },
    },
    dateRange: {
      name: "Date range",
      type: "dateRange",
      value: { start: "2026-06-18", end: "2026-06-25" },
      config: {
        startPlaceholder: "Start date",
        endPlaceholder: "End date",
      },
    },
    timeRange: {
      name: "Time range",
      type: "timeRange",
      value: { start: "09:00", end: "18:00" },
    },
    dateTimeRange: {
      name: "Date time range",
      type: "datetimeRange",
      value: {
        start: "2026-06-18T09:00",
        end: "2026-06-18T18:00",
      },
      config: {
        startPlaceholder: "Start date",
        endPlaceholder: "End date",
      },
    },
  }

  const validationFields: FormConfig = {
    username: {
      name: t("username"),
      type: "text",
      required: true,
      config: {
        placeholder: t("validationHint"),
        rules: [
          (value) =>
            !value || String(value).trim().length < 3
              ? t("validationHint")
              : undefined,
        ],
      },
    },
    email: {
      name: t("emailLabel"),
      type: "email",
      required: true,
      config: {
        placeholder: "user@example.com",
        rules: [
          (value) =>
            !value || !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(String(value))
              ? t("emailError")
              : undefined,
        ],
      },
    },
  }

  return (
    <div className="space-y-8">
      {/* ── The four business modes ───────────────────────────── */}
      <div className="space-y-3">
        <h4 className="text-sm font-semibold text-muted-foreground">
          {t("modes")}
        </h4>

        <div className="inline-flex rounded-lg border bg-muted/40 p-1">
          {MODES.map((m) => (
            <button
              key={m}
              type="button"
              onClick={() => setMode(m)}
              aria-pressed={mode === m ? "true" : "false"}
              className={cn(
                "rounded-md px-3 py-1.5 text-sm font-medium transition-colors",
                mode === m
                  ? "bg-background text-foreground shadow-sm"
                  : "text-muted-foreground hover:text-foreground"
              )}
            >
              {t(m)}
            </button>
          ))}
        </div>

        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <Badge variant="secondary">{t(mode)}</Badge>
          <span>{modeDesc[mode]}</span>
        </div>

        {/* key=mode forces the form to re-initialize when the mode changes */}
        <AutoForm
          key={mode}
          mode={mode}
          fields={crudFields}
          initialData={sampleRecord}
          className="rounded-lg border p-4"
          onSubmit={(data) => {
            toast.success(JSON.stringify(data))
          }}
        >
          {mode !== "detail" && <Button type="submit">{t("submit")}</Button>}
        </AutoForm>
      </div>

      {/* ── Conditional fields (config-driven) ──────────────────── */}
      <DemoSection titleKey="configDriven">
        <AutoForm
          fields={fields}
          className="rounded-lg border p-4"
          onSubmit={(data) => {
            toast.success(JSON.stringify(data))
          }}
        >
          <Button type="submit">{t("submit")}</Button>
        </AutoForm>
      </DemoSection>

      <DemoSection titleKey="dateAndTime">
        <AutoForm
          fields={scheduleFields}
          className="rounded-lg border p-4"
          onSubmit={(data) => {
            toast.success(JSON.stringify(data))
          }}
        >
          <Button type="submit">{t("submit")}</Button>
        </AutoForm>
      </DemoSection>

      {/* ── Validation ──────────────────────────────────── */}
      <div className="space-y-3">
        <h4 className="text-sm font-semibold text-muted-foreground">
          {t("validation")}
        </h4>
        <AutoForm
          fields={validationFields}
          className="rounded-lg border p-4"
          onSubmit={(data) => {
            toast.success(JSON.stringify(data))
          }}
        >
          <Button type="submit">{t("submit")}</Button>
        </AutoForm>
      </div>
    </div>
  )
}
