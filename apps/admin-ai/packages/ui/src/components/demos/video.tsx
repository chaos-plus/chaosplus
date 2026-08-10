import { useTranslations } from "use-intl"

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

export function VideoDemo() {
  const t = useTranslations("showcase.demos.video")
  return (
    <div className="space-y-8">
      <DemoSection titleKey="html5">
        <video
          controls
          preload="none"
          poster="https://images.unsplash.com/photo-1618005182384-a83a8bd57fbe?w=600&h=400&fit=crop"
          className="aspect-video max-w-md rounded-lg bg-black"
        >
          <source
            src="https://commondatastorage.googleapis.com/gtv-videos-bucket/sample/ForBiggerBlazes.mp4"
            type="video/mp4"
          />
          {t("unsupported")}
        </video>
      </DemoSection>

      <LocalSection title={t("autoplay")}>
        <div className="space-y-2">
          <p className="text-xs text-muted-foreground">
            {t("muted")} · {t("looped")}
          </p>
          <video
            muted
            autoPlay
            loop
            playsInline
            poster="https://images.unsplash.com/photo-1500462918059-b1a0cb512f1d?w=600&h=340&fit=crop"
            className="aspect-video max-w-md rounded-lg bg-black"
          >
            <source
              src="https://commondatastorage.googleapis.com/gtv-videos-bucket/sample/SubaruOutbackOnStreetAndDirt.mp4"
              type="video/mp4"
            />
            {t("unsupported")}
          </video>
        </div>
      </LocalSection>

      <LocalSection title={t("responsive")}>
        <div className="space-y-2">
          <p className="text-xs text-muted-foreground">{t("youtube")}</p>
          <div className="aspect-video w-full max-w-md overflow-hidden rounded-lg">
            <iframe
              src="https://www.youtube.com/embed/dQw4w9WgXcQ"
              title={t("youtube")}
              allow="accelerometer; autoplay; clipboard-write; encrypted-media; gyroscope; picture-in-picture"
              allowFullScreen
              className="size-full"
            />
          </div>
        </div>
      </LocalSection>
    </div>
  )
}
