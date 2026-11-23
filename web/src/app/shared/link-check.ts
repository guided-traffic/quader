import { formatNumber } from '@angular/common';
import { DownloadLinkReason } from '../api/download-links-api';

/** A count of bytes in the reader's language: "2,048 bytes", "2.048 Bytes". */
function bytesText(count: number): string {
  const n = formatNumber(count, buildLocale());
  return count === 1 ? $localize`:@@links.check.byte:${n}:count: byte` : $localize`:@@links.check.bytes:${n}:count: bytes`;
}

/** The locale of the build, as `$localize` was compiled for it. */
function buildLocale(): string {
  return (typeof $localize !== 'undefined' && $localize.locale) || 'en-US';
}

/**
 * What a failed link check saw, as the clause of a reason's sentence in the reader's language (Q-UI-996): built from the answer's
 * fields, the status and the bytes received against those expected, or the header whose override the store ignored; the server's
 * English `detail` only where the answer carries no check.
 */
export function linkCheckClause(reason: DownloadLinkReason): string {
  const check = reason.check;
  switch (check?.outcome) {
    case 'not_run':
      return $localize`:@@links.check.not-run:the link check has not run on this replica yet`;
    case 'no_answer':
      return $localize`:@@links.check.no-answer:the test link got no answer`;
    case 'unexpected_answer': {
      const status = String(check.status ?? '');
      const expectedStatus = String(check.expectedStatus ?? 200);
      const bytes = bytesText(check.bytes ?? 0);
      const expectedBytes = bytesText(check.expectedBytes ?? 0);
      return $localize`:@@links.check.unexpected:the test link answered with status ${status}:status: and ${bytes}:bytes: instead of status ${expectedStatus}:expectedStatus: and ${expectedBytes}:expectedBytes:`;
    }
    case 'override_ignored': {
      const header = check.header ?? '';
      const expected = check.expectedValue ?? '';
      const value = check.value ?? '';
      return value
        ? $localize`:@@links.check.override:its answer carries ${header}:header: “${value}:value:” instead of “${expected}:expected:”`
        : $localize`:@@links.check.override-none:its answer carries no ${header}:header: instead of “${expected}:expected:”`;
    }
    default:
      return reason.detail ?? '';
  }
}
