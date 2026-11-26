import { Provider } from '@angular/core';
import { NuGetApi, NuGetPublished, NuGetPublishOptions, SymbolsRefused } from '../nuget-api';
import { ApiError } from '../problem';

/**
 * The screens' push of a NuGet package (R-NUGET-006): `published` is what the next push answers, a stored version or a refusal;
 * `symbolsRefusal` refuses the symbol package after the package was stored. `calls` records each push's repository and files.
 */
export class FakeNuGetApi extends NuGetApi {
  published: NuGetPublished | ApiError = { id: 'Contoso.Logging', version: '2.4.0', symbols: 'none' };
  symbolsRefusal: ApiError | null = null;
  readonly calls: { readonly ref: string; readonly pkg: number; readonly symbols: number | null }[] = [];

  override async publish(ref: string, pkg: Blob, symbols: Blob | null, options: NuGetPublishOptions = {}): Promise<NuGetPublished> {
    this.calls.push({ ref, pkg: pkg.size, symbols: symbols?.size ?? null });
    options.onProgress?.(pkg.size, pkg.size);
    const answer = this.published;
    if (answer instanceof ApiError) throw answer;
    if (!symbols) return { ...answer, symbols: 'none' };
    options.onSymbols?.();
    if (this.symbolsRefusal) throw new SymbolsRefused({ ...answer, symbols: 'none' }, this.symbolsRefusal);
    return { ...answer, symbols: 'stored' };
  }
}

export function provideNuGetFakes(): Provider[] {
  return [{ provide: NuGetApi, useClass: FakeNuGetApi }];
}
