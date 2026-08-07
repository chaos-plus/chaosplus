import { useTranslations } from "use-intl"

import {
  Accordion,
  AccordionItem,
  AccordionTrigger,
  AccordionContent,
} from "../accordion"
import { DemoSection } from "./_section"

export function AccordionDemo() {
  const t = useTranslations("showcase.demos.accordion")
  return (
    <div className="space-y-8">
      <DemoSection titleKey="single">
        <Accordion className="w-full">
          <AccordionItem value="item-1">
            <AccordionTrigger>{t("q1")}</AccordionTrigger>
            <AccordionContent>{t("a1")}</AccordionContent>
          </AccordionItem>
          <AccordionItem value="item-2">
            <AccordionTrigger>{t("q2")}</AccordionTrigger>
            <AccordionContent>{t("a2")}</AccordionContent>
          </AccordionItem>
          <AccordionItem value="item-3">
            <AccordionTrigger>{t("q3")}</AccordionTrigger>
            <AccordionContent>{t("a3")}</AccordionContent>
          </AccordionItem>
        </Accordion>
      </DemoSection>

      <DemoSection titleKey="group">
        <Accordion className="w-full" multiple defaultValue={["panel-1"]}>
          <AccordionItem value="panel-1">
            <AccordionTrigger>{t("section1")}</AccordionTrigger>
            <AccordionContent>{t("content1")}</AccordionContent>
          </AccordionItem>
          <AccordionItem value="panel-2">
            <AccordionTrigger>{t("section2")}</AccordionTrigger>
            <AccordionContent>{t("content2")}</AccordionContent>
          </AccordionItem>
          <AccordionItem value="panel-3">
            <AccordionTrigger>{t("section3")}</AccordionTrigger>
            <AccordionContent>{t("content3")}</AccordionContent>
          </AccordionItem>
        </Accordion>
      </DemoSection>

      <DemoSection titleKey="disabled">
        <Accordion className="w-full">
          <AccordionItem value="active-1">
            <AccordionTrigger>{t("activeItem")}</AccordionTrigger>
            <AccordionContent>{t("activeContent")}</AccordionContent>
          </AccordionItem>
          <AccordionItem value="disabled-1" disabled>
            <AccordionTrigger className="disabled:cursor-not-allowed disabled:opacity-50">
              {t("disabledItem")}
            </AccordionTrigger>
            <AccordionContent>{t("disabledContent")}</AccordionContent>
          </AccordionItem>
        </Accordion>
      </DemoSection>
    </div>
  )
}
