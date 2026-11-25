/**
 * The words of an upstream's rate limit that holds a download queue (remote-curation.md R-CUR-011 "An upstream's rate limit", R-CUR-018;
 * api-and-ui.md Q-UI-1100 to Q-UI-1102): the tab **Downloads** says it above its list, on a row and on a bulk's line, and the top bar's
 * downloads panel says it per queue. Every time is a time of day in the viewer's language, with the day where it is not today.
 */

/** What a sentence reads of a limit: the upstream's name, since when, until when, and whether the upstream named that time. */
export interface LimitFacts {
  readonly upstream: string;
  readonly since: string;
  readonly until: string;
  readonly hinted: boolean;
}

function viewerLocale(): string {
  return (typeof $localize !== 'undefined' && $localize.locale) || 'en-US';
}

/** A time of day in the viewer's language, "08:12"; with the day where it is not the viewer's today, "26 Nov, 08:12". */
export function clockText(iso: string, now: number = Date.now(), locale: string = viewerLocale()): string {
  const at = new Date(iso);
  if (Number.isNaN(at.getTime())) return iso;
  const today = new Date(now);
  const sameDay = at.getFullYear() === today.getFullYear() && at.getMonth() === today.getMonth() && at.getDate() === today.getDate();
  const time: Intl.DateTimeFormatOptions = { hour: '2-digit', minute: '2-digit' };
  return new Intl.DateTimeFormat(locale, sameDay ? time : { ...time, day: 'numeric', month: 'short' }).format(at);
}

/**
 * The one sentence above the tab's list while the limit holds the queue (Q-UI-1100): who limits, since when, and when the downloads
 * that wait start, the upstream's own time or the time Quader asks again.
 */
export function limitSentence(l: LimitFacts, now: number = Date.now()): string {
  const upstream = l.upstream;
  const since = clockText(l.since, now);
  const until = clockText(l.until, now);
  return l.hinted
    ? $localize`:@@downloads.limit.sentence-hinted:${upstream}:upstream: limits how often Quader may ask it, since ${since}:since:. The downloads that wait start at ${until}:until:, the time ${upstream}:upstream: named.`
    : $localize`:@@downloads.limit.sentence:${upstream}:upstream: limits how often Quader may ask it, since ${since}:since:, and does not say until when. Quader asks again at ${until}:until:; the downloads that wait start once ${upstream}:upstream: allows it.`;
}

/** A waiting row's line while the limit holds its queue (Q-UI-1100): "Rate limit until 08:12". */
export function limitRowText(until: string, now: number = Date.now()): string {
  const time = clockText(until, now);
  return $localize`:@@downloads.limit.row:Rate limit until ${time}:until:`;
}

/** A bulk's line while the limit holds the rows it waits for (Q-UI-1100): "Waits for Docker Hub's rate limit until 08:12". */
export function limitBulkText(l: Pick<LimitFacts, 'upstream' | 'until'>, now: number = Date.now()): string {
  const upstream = l.upstream;
  const time = clockText(l.until, now);
  return $localize`:@@downloads.limit.bulk:Waits for ${upstream}:upstream:'s rate limit until ${time}:until:`;
}

/**
 * The panel's line of a queue the limit holds (Q-UI-1101): "206 downloads of upstream/docker-hub wait: Docker Hub limits requests
 * until 08:12.", with the time Quader asks again where the upstream named none.
 */
export function limitQueueText(l: LimitFacts & { readonly waiting: number; readonly repository: string }, now: number = Date.now()): string {
  const { upstream, repository } = l;
  const n = l.waiting;
  const time = clockText(l.until, now);
  if (n === 1) {
    return l.hinted
      ? $localize`:@@upstream-downloads.limit.one-hinted:1 download of ${repository}:repository: waits: ${upstream}:upstream: limits requests until ${time}:until:.`
      : $localize`:@@upstream-downloads.limit.one:1 download of ${repository}:repository: waits: ${upstream}:upstream: limits requests; Quader asks again at ${time}:until:.`;
  }
  return l.hinted
    ? $localize`:@@upstream-downloads.limit.many-hinted:${n}:count: downloads of ${repository}:repository: wait: ${upstream}:upstream: limits requests until ${time}:until:.`
    : $localize`:@@upstream-downloads.limit.many:${n}:count: downloads of ${repository}:repository: wait: ${upstream}:upstream: limits requests; Quader asks again at ${time}:until:.`;
}

/**
 * Who reads the offer of an account under a limit's sentence (Q-UI-1160), which decides where the sentence sends them: the writer of the
 * configuration of a well-known repository of the project `upstream` (`upstream-admin`), the writer of a project's own remote
 * (`remote-writer`), or anyone else, of a well-known repository (`upstream-reader`) or of a project's remote (`remote-reader`).
 */
export type AccountOfferReader = 'upstream-admin' | 'remote-writer' | 'upstream-reader' | 'remote-reader';

/**
 * The sentence after a limit's sentence where an account would lift it (`accountRaisesLimit`, Q-UI-1160): the upstream allows more
 * requests with an account, and where this reader adds one. The administrator's variant is followed by the link **Add an account**.
 */
export function accountOfferText(upstream: string, reader: AccountOfferReader): string {
  switch (reader) {
    case 'upstream-admin':
      return $localize`:@@downloads.limit.account-admin:${upstream}:upstream: allows more requests with an account. This registry has none: add one on its page, or in the Helm values (secrets.upstreamAccounts).`;
    case 'remote-writer':
      return $localize`:@@downloads.limit.account-writer:${upstream}:upstream: allows more requests with an account. This repository has none: add one in the repository's settings under Upstream.`;
    case 'upstream-reader':
      return $localize`:@@downloads.limit.account-reader:${upstream}:upstream: allows more requests with an account; an administrator of Quader can add one.`;
    case 'remote-reader':
      return $localize`:@@downloads.limit.account-owner:${upstream}:upstream: allows more requests with an account; an Owner of the project can add one.`;
  }
}
