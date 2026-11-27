import { Injectable, forwardRef, inject } from '@angular/core';
import { ApiError } from './problem';
import { ScreenPublish, TransferOptions, UploadTransfer } from './upload-transfer';

/* The screens' upload of a NuGet package (formats/nuget.md R-NUGET-006, R-NUGET-020): the client's own push, with a token of the session. */

/** What a push from the screen stored: the ID as the package declares it, the full normalized version, and its symbol package's fate. */
export interface NuGetPublished {
  readonly id: string;
  readonly version: string;
  /** `stored`: the symbol package was pushed after the package; `none`: none was chosen. */
  readonly symbols: 'stored' | 'none';
}

/** How a push from the screen is followed: the bytes of the package, then those of its symbol package. */
export interface NuGetPublishOptions extends TransferOptions {
  /** The package is stored and its symbol package's bytes begin. */
  readonly onSymbols?: () => void;
}

@Injectable({ providedIn: 'root', useClass: forwardRef(() => HttpNuGetApi) })
export abstract class NuGetApi {
  /**
   * The screens' push of a `.nupkg`, and of its `.snupkg` after it (R-NUGET-006, Q-NUGET-162): `POST …/upload-tokens` issues the
   * caller's token of five minutes for this repository; the package goes with it as the raw body to the package publish resource
   * (`publishPath`, served by the role packages with every check of `dotnet nuget push`), whose `201` names the version it stored in
   * `Quader-Package` and `Quader-Version`; the symbol package follows to `symbolsPath`. The token is revoked once the pushes answered, and
   * is never shown, logged, stored or put into an address. A refusal rejects with an `ApiError` whose code is the one NuGet's plain-text
   * answer names. A refused symbol package after a stored package rejects with `SymbolsRefused`, which carries what was stored.
   */
  abstract publish(ref: string, pkg: Blob, symbols: Blob | null, options?: NuGetPublishOptions): Promise<NuGetPublished>;
}

/** The symbol package was refused after its package was stored: what was stored, and why the symbols were not. */
export class SymbolsRefused extends Error {
  constructor(
    readonly stored: NuGetPublished,
    readonly failure: unknown,
  ) {
    super('The symbol package was refused.');
  }
}

@Injectable()
export class HttpNuGetApi extends NuGetApi {
  private readonly transfer = inject(UploadTransfer);
  private readonly tokens = inject(ScreenPublish);

  override async publish(ref: string, pkg: Blob, symbols: Blob | null, options: NuGetPublishOptions = {}): Promise<NuGetPublished> {
    const issued = await this.tokens.issue(ref);
    const headers = { 'Content-Type': 'application/octet-stream', Authorization: `Bearer ${issued.token}` };
    try {
      let answer;
      try {
        answer = await this.transfer.sendForHeaders('PUT', issued.publishPath, pkg, headers, options);
      } catch (e) {
        throw nugetFailure(e);
      }
      const stored: NuGetPublished = { id: answer.get('Quader-Package') ?? '', version: answer.get('Quader-Version') ?? '', symbols: 'none' };
      if (!symbols || !issued.symbolsPath) return stored;
      options.onSymbols?.();
      try {
        await this.transfer.sendForHeaders('PUT', issued.symbolsPath, symbols, headers, options);
      } catch (e) {
        if (e instanceof DOMException && e.name === 'AbortError') return stored;
        throw new SymbolsRefused(stored, nugetFailure(e));
      }
      return { ...stored, symbols: 'stored' };
    } finally {
      // The token served one upload: it is revoked once its pushes answered, failed or were cancelled (Q-ANS-140, Q-NUGET-162).
      this.tokens.revoke(issued);
    }
  }
}

/**
 * The refusal of NuGet's push as the screens read one (R-UI-216): the route answers in plain text, `<code>: <sentence>` (R-NUGET-002),
 * which is no problem document; it becomes the problem of the `ApiError`, and every other fact of the answer stays. Anything else passes
 * unchanged.
 */
export function nugetFailure(e: unknown): unknown {
  if (!(e instanceof ApiError) || e.problem || !e.answer.body) return e;
  const m = /^([a-z][a-z0-9_]*): (.*)$/s.exec(e.answer.body.trim());
  if (!m) return e;
  const problem = { code: m[1]!, status: e.status, detail: m[2]! };
  const out = new ApiError(e.status, problem, e.requestId, e.retryAfterSeconds, { ...e.answer, body: null, bodyLength: null });
  out.afterRetry = e.afterRetry;
  return out;
}
