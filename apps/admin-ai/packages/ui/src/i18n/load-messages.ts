// Client-side on-demand loading of a locale's messages (the bundler splits messages/*.json into async chunks)
export async function loadMessages(
  locale: string
): Promise<Record<string, unknown>> {
  const mod = await import(`./messages/${locale}.json`)
  return (mod as { default: Record<string, unknown> }).default
}
