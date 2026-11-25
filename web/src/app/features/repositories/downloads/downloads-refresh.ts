/**
 * How often the tab Downloads reads the queue and its syncs again while a row waits or runs or a sync's run is in progress (R-CUR-018,
 * R-CUR-027; WCAG 2.2.2: with a pause control); while nothing does, or while an upstream's rate limit holds the queue and nothing
 * runs, it reads every `LIVE_SLOW_MS` (api-and-ui.md R-UI-213, Q-UI-1102).
 */
export const DOWNLOADS_REFRESH_MS = 5_000;
