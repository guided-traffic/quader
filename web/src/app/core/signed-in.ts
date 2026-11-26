import { InjectionToken, Signal } from '@angular/core';

/**
 * Whether the reader has a session, for a part of the screens that offers a signed-in reader more: a download's refusal by the limits
 * links to the reader's usage (R-UI-186). The main installation binds it to its session (`provideViewReads` of api/providers.ts); an
 * application without sessions binds none, and the part offers nothing more (R-UI-223).
 */
export const SIGNED_IN = new InjectionToken<Signal<boolean>>('SIGNED_IN');
