# chaos.plus — Design System (Master)

Generated with ui-ux-pro-max (Modern Dark Cinema) on 2026-08-09. This is the
source of truth for the platform web UI's visual direction. Pages may carry
overrides in `pages/`; when none exist, use this Master exclusively.

## Positioning

Real-Time / Operations product — a "跨越生产鸿沟" governance runtime for agent
workflows. The UI must read as a developer control-plane: dense, technical,
trustworthy, with run/approval status as the signal.

## Style — Modern Dark (Cinema Mobile)

- **Dark mode primary.** Light is an exception, never the default.
- Ambient/glassmorphism surfaces, deep slate backgrounds, a single green accent
  for "run/ok" and red only for destructive/terminal states.
- Layered depth (frosted headers, subtle glows on active elements), but never at
  the cost of information density — this is a console, not a marketing page.

## Colors

| Token | Value | Use |
|---|---|---|
| `--color-background` | `#0F172A` (slate-900) | app background |
| `--color-foreground` | `#F8FAFC` | primary text |
| `--color-primary` | `#1E293B` | buttons / focus |
| `--color-primary-foreground` | `#FFFFFF` | on-primary text |
| `--color-secondary` | `#334155` | secondary surfaces |
| `--color-muted` | `#272F42` | muted surfaces |
| `--color-accent` | `#22C55E` (green) | run / success / CTA |
| `--color-border` | `#475569` | borders / dividers |
| `--color-destructive` | `#EF4444` | terminal failure |
| `--color-ring` | `#1E293B` | focus ring |

Notes: code dark + run green. Avoid pure `#000000` (OLED smear); use slate
stacks.

## Typography — Inter

Inter 300–700 (https://fonts.googleapis.com/css2?family=Inter:wght@300;400;500;600;700&display=swap).
Mono: `ui-monospace, 'Cascadia Code', monospace` for run ids, node ids, code.

## Motion

- Easing `cubic-bezier(0.16,1,0.3,1)` (expo.out); 150–300ms transitions.
- Spring modals (damping 20, stiffness 90); press scale 0.97→1.0.
- Motion must clarify flow (node status, approval arrival), never decorate.

## Anti-Patterns (Avoid)

- Light mode default.
- Emojis as icons — use Lucide (SVG).
- Slow/blurry-heavy performance on large lists; blur is for headers/nav only.

## Pre-Delivery Checklist

- [x] No emojis as icons (Lucide SVG in use).
- [x] `cursor-pointer` on all clickable elements.
- [x] Hover states with smooth 150–300ms transitions.
- [x] Dark mode: text contrast ≥ 4.5:1.
- [x] Focus states visible for keyboard nav.
- [ ] `prefers-reduced-motion` respected.
- [ ] Responsive: 375 / 768 / 1024 / 1440.

## Stack Notes

Platform app: React 19 + Vite + Tailwind v4 + shadcn/ui (`@workspace/ui`).
Theme runtime: `next-themes` + `@workspace/ui/themes/color-theme-provider`.
Design tokens live in `@workspace/ui/src/styles/globals.css` (shadcn oklch
variables) — a scoped `.dark` override in the platform `index.css` may restyle
the platform without touching the shared `sys` app.
