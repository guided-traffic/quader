import { ConflictField } from '../../shared/conflict-dialog';
import { levelName, VisibilityLevel } from '../../shared/ui/visibility-level';
import { discoveryLabel, mirrorMemberLabel, remoteModeLabel } from './mirror-settings';
import { SettingNode } from './settings-schema';
import { upstreamAuthLabel } from './upstream-settings';

/** What the select **Hide existence from callers without access** shows for a value of `hideExistence`. */
function hideExistenceLabel(v: unknown): string {
  switch (v) {
    case 'true':
      return $localize`:@@repo.hide.true: Hide: answer as if it did not exist `.trim();
    case 'false':
      return $localize`:@@repo.hide.false: Do not hide: answer 403 `.trim();
    default:
      return $localize`:@@repo.hide.inherit: Use the installation setting `.trim();
  }
}

/** A further host as its row of the form shows it: the pattern, and whether it receives the credential. */
function hostEntries(v: unknown): readonly string[] {
  if (!Array.isArray(v)) return [String(v)];
  return v.map((h: { pattern?: unknown; sendCredentials?: unknown }) =>
    h.sendCredentials === true ? `${String(h.pattern)} · ${$localize`:@@upstream.hosts.send:Receives the credential`}` : String(h.pattern),
  );
}

/** A setting of the format's member (R-UI-088) by its node: a choice as the option's label, a list of options as theirs. */
function nodeField(nodes: readonly SettingNode[], pointer: string): ConflictField | null {
  for (const n of nodes) {
    if (n.kind === 'group') {
      if (pointer.startsWith(`${n.pointer}/`)) return nodeField(n.children, pointer);
      continue;
    }
    if (n.pointer !== pointer || n.kind === 'other') continue;
    if (n.kind === 'choice') {
      const labels = n.labels;
      const options = n.options;
      return {
        label: n.label,
        show: (v) => {
          const i = options.findIndex((o) => JSON.stringify(o) === JSON.stringify(v));
          return i >= 0 ? labels[i]! : typeof v === 'string' ? v : JSON.stringify(v);
        },
      };
    }
    if (n.kind === 'list' && n.options && n.labels) {
      const labels = n.labels;
      const options = n.options;
      return {
        label: n.label,
        show: (v) => (Array.isArray(v) ? v.map((e) => labels[options.indexOf(String(e))] ?? String(e)) : [String(v)]),
      };
    }
    return { label: n.label };
  }
  return null;
}

/**
 * How the settings page of a repository names and shows its fields in the conflict dialog (R-UI-042, Q-UI-1210): the labels of its
 * sections, the option a select shows, a password never. `nodes` are the rendered members of the format's settings.
 */
export function repositoryConflictFields(nodes: readonly SettingNode[]): (pointer: string) => ConflictField | null {
  return (pointer) => {
    switch (pointer) {
      case '/description':
        return { label: $localize`:@@repo.field.description-optional:Description (optional)` };
      case '/adminNotes':
        return { label: $localize`:@@repo.field.notes:Administrator notes` };
      case '/includePatterns':
        return { label: $localize`:@@repo.field.include:Include patterns` };
      case '/excludePatterns':
        return { label: $localize`:@@repo.field.exclude:Exclude patterns` };
      case '/hideExistence':
        return { label: $localize`:@@repo.field.hide:Hide existence from callers without access`, show: hideExistenceLabel };
      case '/artifacts':
        return { label: $localize`:@@repo.visibility.level:Visibility level`, show: (v) => levelName(v as VisibilityLevel) };
      case '/wiki':
        return { label: $localize`:@@repo.visibility.wiki-fold:Wiki visibility`, show: (v) => levelName(v as VisibilityLevel) };
      case '/bastions':
        return { label: $localize`:@@repo.bastions.label:Bastions` };
      case '/remote/url':
        return { label: $localize`:@@upstream.url:Upstream URL` };
      case '/remote/authMode':
        return { label: $localize`:@@upstream.auth:Authentication`, show: (v) => upstreamAuthLabel(String(v)) };
      case '/remote/username':
        return { label: $localize`:@@upstream.username:Username` };
      case '/remote/password':
        return { label: $localize`:@@upstream.password:Password or token`, secret: true };
      case '/remote/extraHosts':
        return { label: $localize`:@@upstream.hosts:Further hosts`, show: hostEntries };
      case '/remote/cache/metadataTtlSeconds':
        return { label: $localize`:@@upstream.metadata-ttl:Metadata cache period in seconds` };
      case '/remote/cache/notFoundTtlSeconds':
        return { label: $localize`:@@upstream.notfound-ttl:Negative cache period in seconds` };
      case '/remote/availability':
        // The check box **Offline** writes `{ mode: 'offline' }`; any other availability is the box unchecked.
        return {
          label: $localize`:@@upstream.offline:Offline`,
          show: (v) =>
            (v as { mode?: unknown } | null)?.mode === 'offline' ? $localize`:@@switch.on:On` : $localize`:@@switch.off:Off`,
        };
      case '/remote/downloadOnProbe':
        return { label: $localize`:@@upstream.download-on-probe:Start the download when a version is asked for` };
      case '/remote/mode':
        return { label: $localize`:@@mirror.field.mode:Mode`, show: (v) => remoteModeLabel(String(v)) };
      case '/remote/mirror/discovery':
        return { label: $localize`:@@mirror.discovery:Discovery`, show: (v) => discoveryLabel(String(v)) };
    }
    if (pointer.startsWith('/remote/mirror/')) {
      const label = mirrorMemberLabel(pointer.slice('/remote/mirror/'.length));
      if (label) return { label };
    }
    return nodeField(nodes, pointer);
  };
}
