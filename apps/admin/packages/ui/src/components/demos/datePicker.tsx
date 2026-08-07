import { useState } from "react"
import { useTranslations } from "use-intl"

import { DatePicker } from "@workspace/ui/components/pickers/date-picker"
import { Label } from "../label"
import { TimePicker } from "@workspace/ui/components/pickers/time-picker"
import { DemoSection } from "./_section"

export function DatePickerDemo() {
  const t = useTranslations("showcase.demos.datePicker")
  const tt = useTranslations("showcase.demos.timePicker")
  const [date, setDate] = useState<Date | null>(null)
  const [eventDate, setEventDate] = useState<Date | null>(null)
  const [eventTime, setEventTime] = useState("09:00")

  return (
    <div className="space-y-8">
      <DemoSection titleKey="date">
        <div className="grid w-full max-w-sm gap-1.5">
          <Label>{t("birth")}</Label>
          <DatePicker
            value={date}
            onChange={setDate}
            placeholder={t("selectDate")}
          />
        </div>
      </DemoSection>

      <DemoSection titleKey="dateAndTime">
        <div className="grid w-full max-w-sm gap-1.5">
          <Label>{t("event")}</Label>
          <div className="flex flex-wrap items-center gap-2">
            <DatePicker
              value={eventDate}
              onChange={setEventDate}
              placeholder={t("selectDate")}
            />
            <TimePicker
              value={eventTime}
              onChange={setEventTime}
              hourLabel={tt("hour")}
              minuteLabel={tt("minute")}
              incrementLabel={(label) => `${label} +`}
              decrementLabel={(label) => `${label} -`}
            />
          </div>
        </div>
      </DemoSection>
    </div>
  )
}
