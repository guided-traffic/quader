import { Injectable, inject, signal } from '@angular/core';
import { Router } from '@angular/router';
import { RepositoriesApi } from '../api/repositories-api';
import { Destination, DestinationReads, RepositoryDestinations, bastionDestinations } from '../views/packages/destinations';
import { ServerAddress } from './server-address';

/** The path of a URL of the application, without its query and fragment: what stays the same while one page is shown. */
function pathOf(url: string): string {
  return url.split(/[?#]/, 1)[0] ?? url;
}

/**
 * The main installation's destinations of a command (R-UI-271, Q-UI-1657): the repository's member `bastions` as every reader reads it
 * (`GET …/repositories/{key}`), each Bastion's listed addresses, and this installation's base URL. A repository is read once per page and
 * the page's choice kept while it stands: both are forgotten at the first read on another path, before any panel there offers a choice,
 * so the next page reads a Bastion's new address or its disable again. The read sends no `If-None-Match` and the API answers it
 * `no-store`, so a Bastion's change that leaves the repository's `ETag` alone is still read. A read that failed is not kept: the next
 * panel asks again.
 */
@Injectable()
export class RepositoryDestinationReads extends DestinationReads {
  private readonly repositories = inject(RepositoriesApi);
  private readonly address = inject(ServerAddress);
  private readonly router = inject(Router, { optional: true });
  private readonly read = new Map<string, Promise<RepositoryDestinations>>();
  /** The path of the page the reads and the choice belong to. */
  private page: string | null = null;
  override readonly chosen = signal<Destination | null>(null);

  override of(repository: string): Promise<RepositoryDestinations> {
    const page = this.router ? pathOf(this.router.url) : null;
    if (page !== this.page) {
      this.page = page;
      this.read.clear();
      this.chosen.set(null);
    }
    let answer = this.read.get(repository);
    if (!answer) {
      answer = this.load(repository);
      this.read.set(repository, answer);
      answer.catch(() => {
        if (this.read.get(repository) === answer) this.read.delete(repository);
      });
    }
    return answer;
  }

  private async load(repository: string): Promise<RepositoryDestinations> {
    const [main, read] = await Promise.all([this.address.baseUrl().catch(() => null), this.repositories.get(repository)]);
    return { main, bastions: bastionDestinations(read.value.bastions) };
  }
}
