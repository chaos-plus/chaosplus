import { useTranslations } from "use-intl"

import { DemoSection } from "./_section"

export function ImageDemo() {
  const t = useTranslations("showcase.demos.image")
  return (
    <div className="space-y-8">
      <DemoSection titleKey="single">
        <img
          src="https://images.unsplash.com/photo-1618005182384-a83a8bd57fbe?w=600&h=400&fit=crop"
          alt={t("abstract")}
          className="aspect-video max-w-md rounded-lg object-cover"
        />
      </DemoSection>

      <DemoSection titleKey="gallery">
        <div className="grid max-w-md grid-cols-2 gap-3">
          <img
            src="https://images.unsplash.com/photo-1558591710-4b4a1ae0f04d?w=300&h=300&fit=crop"
            alt={t("geometric")}
            className="aspect-square rounded-lg object-cover"
          />
          <img
            src="https://images.unsplash.com/photo-1579546929518-9e396f3cc809?w=300&h=300&fit=crop"
            alt={t("soft")}
            className="aspect-square rounded-lg object-cover"
          />
          <img
            src="https://images.unsplash.com/photo-1557682250-33bd709cbe85?w=300&h=300&fit=crop"
            alt={t("waves")}
            className="aspect-square rounded-lg object-cover"
          />
          <img
            src="https://images.unsplash.com/photo-1550684376-efcbd6e3f031?w=300&h=300&fit=crop"
            alt={t("neon")}
            className="aspect-square rounded-lg object-cover"
          />
        </div>
      </DemoSection>
    </div>
  )
}
