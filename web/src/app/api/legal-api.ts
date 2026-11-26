import { Schema } from './types';

/** `GET /api/v1/legal` (R-UI-225): the imprint and the privacy notice as one locale shows them; a page without any text is null. */
export type LegalPages = Schema<'LegalPages'>;
export type LegalPage = Schema<'LegalPage'>;
/** The two legal pages, by the segment of their address. */
export type LegalPageName = 'imprint' | 'privacy';

/**
 * The read of the legal pages. The main installation binds `HttpLegalApi` (legal-api-http.ts); the Bastion's application reads its
 * own at its data path (R-BAS-118, R-UI-228). A module without a client, so that the Bastion's bundle holds none.
 */
export abstract class LegalApi {
  /** Always open, also without a session (R-AUTH-037). */
  abstract pages(locale: 'en' | 'de'): Promise<LegalPages>;
}
