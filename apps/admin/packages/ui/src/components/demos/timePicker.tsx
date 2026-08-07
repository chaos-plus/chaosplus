import { useState } from "react"
import { useTranslations } from "use-intl"

import { DatePicker } from "@workspace/ui/components/pickers/date-picker"
import { Label } from "../label"
import { TimePicker } from "@workspace/ui/components/pickers/time-picker"
import { DemoSection } from "./_section"

function LocalSection({
  title,
  children,
}: {
  title: string
  children: React.ReactNode
}) {
  return (
    <div className="space-y-3">
      <h4 className="text-sm font-semibold text-muted-foreground">{title}</h4>
      {children}
    </div>
  )
}

export function TimePickerDemo() {
  const tDate = useTranslations("showcase.demos.datePicker")
  const t = useTranslations("showcase.demos.timePicker")
  const [reminderTime, setReminderTime] = useState("08:30")
  const [hourOnly, setHourOnly] = useState("08")
  const [preciseTime, setPreciseTime] = useState("08:30:15")
  const [startTime, setStartTime] = useState("09:00")
  const [endTime, setEndTime] = useState("17:00")
  const [withDate, setWithDate] = useState<Date | null>(null)
  const [withDateTime, setWithDateTime] = useState("09:00")

  const timeRangeError =
    startTime && endTime && endTime < startTime ? t("timeRangeError") : null

  return (
    <div className="space-y-8">
      <DemoSection titleKey="time">
        <div className="grid w-full max-w-sm gap-3">
          <div className="grid gap-1.5">
            <Label>{t("reminder")}</Label>
            <TimePicker
              value={reminderTime}
              onChange={setReminderTime}
              hourLabel={t("hour")}
              minuteLabel={t("minute")}
            />
          </div>
          <div className="grid gap-1.5">
            <Label>{t("hour")}</Label>
            <TimePicker
              value={hourOnly}
              onChange={setHourOnly}
              granularity="hour"
              hourLabel={t("hour")}
              formatValue={(raw) => `${raw}:00`}
            />
          </div>
          <div className="grid gap-1.5">
            <Label>{t("second")}</Label>
            <TimePicker
              value={preciseTime}
              onChange={setPreciseTime}
              granularity="second"
              hourLabel={t("hour")}
              minuteLabel={t("minute")}
              secondLabel={t("second")}
            />
          </div>
        </div>
      </DemoSection>

      <LocalSection title={t("range")}>
        <div className="grid w-full max-w-sm gap-3">
          <div className="grid gap-1.5">
            <Label>{t("startTime")}</Label>
            <TimePicker
              value={startTime}
              onChange={setStartTime}
              hourLabel={t("hour")}
              minuteLabel={t("minute")}
              incrementLabel={(label) => `${label} +`}
              decrementLabel={(label) => `${label} -`}
            />
          </div>
          <div className="grid gap-1.5">
            <Label>{t("endTime")}</Label>
            <TimePicker
              value={endTime}
              onChange={setEndTime}
              hourLabel={t("hour")}
              minuteLabel={t("minute")}
              incrementLabel={(label) => `${label} +`}
              decrementLabel={(label) => `${label} -`}
            />
          </div>
          {timeRangeError && (
            <p className="text-xs text-destructive">{timeRangeError}</p>
          )}
        </div>
      </LocalSection>

      <LocalSection title={t("withDate")}>
        <div className="grid w-full max-w-sm gap-1.5">
          <Label>{t("dateAndTime")}</Label>
          <div className="flex flex-wrap items-center gap-2">
            <DatePicker
              value={withDate}
              onChange={setWithDate}
              placeholder={tDate("selectDate")}
            />
            <TimePicker
              value={withDateTime}
              onChange={setWithDateTime}
              hourLabel={t("hour")}
              minuteLabel={t("minute")}
              incrementLabel={(label) => `${label} +`}
              decrementLabel={(label) => `${label} -`}
            />
          </div>
        </div>
      </LocalSection>
    </div>
  )
}
