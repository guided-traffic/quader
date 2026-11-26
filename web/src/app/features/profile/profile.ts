import { PageHeader } from '../../shared/ui/page-header';
import { Component, computed, inject, signal } from '@angular/core';
import { Checkbox } from '../../shared/ui/checkbox';
import { IdentityApi, User } from '../../api/identity-api';
import { LoadedPreferences, PreferencesApi, PreferencesPatch } from '../../api/preferences-api';
import { ApiError } from '../../api/problem';
import { Notifier } from '../../core/notifier';
import { PreferencesStore } from '../../core/preferences-store';
import { SessionStore } from '../../core/session-store';
import { SelectField, SelectOption, TextField } from '../../shared/access/fields';
import { Disclosure } from '../../shared/ui/disclosure';
import { asApiError } from '../../shared/paged-resource';
import { ConflictFields, ConflictResolver } from '../../shared/conflict-dialog';
import { ErrorNotice } from '../../shared/error-notice';
import { AvatarStore } from '../../core/avatar-store';
import { ProfileAvatar } from './profile-avatar';
import { ProfileNav } from './profile-nav';
import { Busy } from '../../shared/ui/busy';
import { Loading } from '../../shared/ui/loading';
import { TimePipe } from '../../shared/time-pipe';
import { timeFormatOptions } from '../../shared/time-format-options';
import { formatTime } from '../../shared/time-format';
import { TimeFormatStore } from '../../core/time-format-store';

/**
 * Profile (R-UI-029, R-AUTH-017): identity as the identity provider delivered it (read-only fields), then the quiet sections
 * of the page (Q-UI-333): the preferences of R-API-021 (open), the groups with their source and the system roles of the caller.
 * There is no password, no unlock and no editable email or display name: users have no password
 * in Quader and both attributes are replaced at every login. A missing group claim is a notice,
 * not a dialog (R-AUTH-008). Preferences are saved with `If-Match`; a 412 opens the conflict
 * dialog with their version against yours.
 */
@Component({
  selector: 'quader-profile',
  imports: [Loading, Busy, TimePipe, Checkbox, Disclosure, ErrorNotice, PageHeader, ProfileAvatar, ProfileNav, SelectField, TextField],
  templateUrl: './profile.html',
  styleUrl: '../../shared/access/access.scss',
  styles: `
    .settings {
      max-inline-size: var(--quader-settings-measure);
    }
    .row {
      display: grid;
      grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
      gap: var(--quader-space-6);
    }
    @media (max-width: 40rem) {
      .row {
        grid-template-columns: minmax(0, 1fr);
      }
    }
  `,
})
export class Profile {
  protected readonly heading = $localize`:@@profile.heading:Profile`;
  private readonly identity = inject(IdentityApi);
  private readonly preferencesApi = inject(PreferencesApi);
  private readonly preferencesStore = inject(PreferencesStore);
  private readonly conflicts = inject(ConflictResolver);
  private readonly notifier = inject(Notifier);
  private readonly avatars = inject(AvatarStore);
  private readonly times = inject(TimeFormatStore);
  protected readonly session = inject(SessionStore);

  protected readonly user = signal<User | null>(null);
  protected readonly loadError = signal<ApiError | null>(null);
  protected readonly prefError = signal<ApiError | null>(null);
  protected readonly prefsLoaded = signal(false);
  /** A read of the page runs. */
  protected readonly loading = signal(true);
  /** Nothing of the page is read yet: the whole page loads (R-UI-252). */
  protected readonly pageLoading = computed(() => this.loading() && this.user() === null && !this.prefsLoaded());
  protected readonly saving = signal(false);

  protected readonly locale = signal('');
  protected readonly theme = signal('');
  protected readonly timeFormat = signal('');
  protected readonly shortcuts = signal(false);
  private loaded: LoadedPreferences | null = null;

  protected readonly roles = computed(() => this.session.me()?.roles ?? []);
  protected readonly localeOptions: readonly SelectOption[] = [
    { value: '', label: $localize`:@@profile.pref.locale.browser:Follow the browser` },
    { value: 'en', label: 'English' },
    { value: 'de', label: 'Deutsch' },
  ];
  /**
   * The forms of a time, each with today's time as its example (R-UI-250, Q-UI-1561); the first option follows the installation's
   * default and names it.
   */
  protected readonly timeFormatOptions = computed<readonly SelectOption[]>(() => {
    const now = Date.now();
    return [
      {
        value: '',
        label: $localize`:@@profile.pref.time-format.installation:${formatTime(now, { format: this.times.installation() })}:example: — Installation default`,
      },
      ...timeFormatOptions(now),
    ];
  });
  protected readonly themeOptions: readonly SelectOption[] = [
    { value: '', label: $localize`:@@profile.pref.theme.system:Follow the system` },
    { value: 'light', label: $localize`:@@profile.pref.theme.light:Light` },
    { value: 'dark', label: $localize`:@@profile.pref.theme.dark:Dark` },
  ];

  constructor() {
    void this.load();
  }

  protected async load(): Promise<void> {
    this.loading.set(true);
    try {
      const me = await this.identity.me();
      this.loadError.set(null);
      this.user.set(me);
      this.avatars.set(me.avatarUrl);
    } catch (e) {
      this.loadError.set(asApiError(e));
    }
    await this.loadPreferences();
    this.loading.set(false);
  }

  private async loadPreferences(): Promise<void> {
    try {
      this.adopt(await this.preferencesApi.load());
      this.prefError.set(null);
    } catch (e) {
      // Service accounts and CI tokens have no preferences (R-API-021); the section says so.
      this.prefError.set(asApiError(e));
    }
  }

  private adopt(loaded: LoadedPreferences): void {
    this.loaded = loaded;
    this.locale.set(loaded.preferences.locale ?? '');
    this.theme.set(loaded.preferences.theme ?? '');
    this.timeFormat.set(loaded.preferences.timeFormat ?? '');
    this.shortcuts.set(loaded.preferences.keyboardShortcuts);
    this.prefsLoaded.set(true);
  }

  private patch(): PreferencesPatch {
    return {
      locale: this.locale() || null,
      theme: this.theme() || null,
      timeFormat: this.timeFormat() || null,
      keyboardShortcuts: this.shortcuts(),
    };
  }

  protected async savePreferences(event: Event): Promise<void> {
    event.preventDefault();
    if (!this.loaded) return;
    this.saving.set(true);
    this.prefError.set(null);
    try {
      await this.store(this.loaded.etag);
    } catch (e) {
      const error = asApiError(e);
      if (error.status === 412) await this.resolveConflict();
      else this.prefError.set(error);
    } finally {
      this.saving.set(false);
    }
  }

  private async store(etag: string): Promise<void> {
    const saved = await this.preferencesApi.patch(this.patch(), etag);
    this.adopt(saved);
    this.preferencesStore.adopt(saved);
    this.notifier.status($localize`:@@profile.pref.saved:Preferences saved.`);
  }

  /** The preferences' labels and the options their selects show, for the conflict dialog (R-UI-042, Q-UI-1210). */
  private conflictFields(): ConflictFields {
    const option = (options: readonly SelectOption[]) => (v: unknown) => options.find((o) => o.value === v)?.label ?? String(v);
    return {
      '/locale': {
        label: $localize`:@@profile.pref.locale:Language`,
        show: option(this.localeOptions),
        empty: this.localeOptions[0]!.label,
      },
      '/theme': { label: $localize`:@@profile.pref.theme:Theme`, show: option(this.themeOptions), empty: this.themeOptions[0]!.label },
      '/timeFormat': {
        label: $localize`:@@profile.pref.time-format:Time format`,
        show: option(this.timeFormatOptions()),
        empty: this.timeFormatOptions()[0]!.label,
      },
      '/keyboardShortcuts': $localize`:@@profile.pref.shortcuts:Keyboard shortcuts with a single key, such as / for the search`,
    };
  }

  /** 412: show their version against mine; "Overwrite" sends mine with the ETag read now. */
  private async resolveConflict(): Promise<void> {
    const current = await this.preferencesApi.load();
    const theirs = {
      locale: current.preferences.locale,
      theme: current.preferences.theme,
      timeFormat: current.preferences.timeFormat,
      keyboardShortcuts: current.preferences.keyboardShortcuts,
    };
    const decision = await this.conflicts.resolve({ mine: this.patch(), theirs, fields: this.conflictFields() });
    if (decision === 'overwrite') {
      try {
        await this.store(current.etag);
      } catch (e) {
        this.prefError.set(asApiError(e));
      }
    } else {
      this.adopt(current);
    }
  }
}
