import { FormatDescriptor } from '../../app/api/repositories-api';

/**
 * A format as the Bastion's views read it: its key, the name a screen shows, the clients a version's commands name, whether it is the
 * system's alone, how a name holds a namespace, its documentation and the kinds of signature it knows. What a screen of a repository's
 * settings would read (presets, periods, the settings' schema) is left at a neutral value: no page of the Bastion shows it.
 */
function format(f: Pick<FormatDescriptor, 'name' | 'displayName' | 'clients'> & Partial<FormatDescriptor>): FormatDescriptor {
  return {
    classes: ['local', 'remote', 'virtual'],
    // The client addresses are the server's (R-ARCH-007): the set-up documents carry them, no page reads them from here.
    protocolBasePath: '',
    layout: f.name,
    upstreamPresets: [],
    defaultMetadataTtlSeconds: null,
    defaultNotFoundTtlSeconds: null,
    patternSubject: 'name',
    priorityMembers: false,
    pinsAndClaims: false,
    caps: false,
    vulnerabilityScanning: false,
    mirror: false,
    settingsSchema: null,
    wiki: false,
    systemOnly: false,
    namespaces: null,
    signatureKinds: [],
    ...f,
  };
}

/**
 * The formats of the product as the main installation's `GET /api/v1/formats` names them (internal/formats/*, R-REPO-003), for the
 * Bastion's application, whose catalogue holds no format descriptor (Q-UI-1436): a page shows a format's name, never its key
 * (R-UI-006). A format the table does not know shows its key. The spec `formats.spec.ts` holds the table to the fakes' catalogue,
 * which mirrors the server's.
 */
export const BASTION_FORMATS: readonly FormatDescriptor[] = [
  format({
    name: 'npm',
    displayName: 'npm',
    clients: [
      { id: 'npm', displayName: 'npm' },
      { id: 'pnpm', displayName: 'pnpm' },
      { id: 'yarn', displayName: 'Yarn Berry (2 and later)' },
      { id: 'yarn-classic', displayName: 'Yarn Classic (1.x)' },
    ],
    namespaces: { kind: 'scope', separator: '/', marker: '@', nested: false },
    vulnerabilityScanning: true,
    wiki: true,
    signatureKinds: [
      { name: 'registry signature', role: 'registry' },
      { name: 'provenance attestation', role: 'provenance' },
      { name: 'publish attestation', role: 'registry' },
    ],
  }),
  format({
    name: 'nuget',
    displayName: 'NuGet',
    clients: [
      { id: 'dotnet', displayName: '.NET CLI (dotnet)' },
      { id: 'nuget', displayName: 'nuget.exe' },
      { id: 'visual-studio', displayName: 'Visual Studio 2022' },
    ],
    vulnerabilityScanning: true,
    wiki: true,
    signatureKinds: [
      { name: 'author signature', role: 'publisher' },
      { name: 'repository signature', role: 'registry' },
    ],
  }),
  format({
    name: 'oci',
    displayName: 'OCI (Docker)',
    clients: [
      { id: 'docker', displayName: 'Docker CLI (with Buildx)' },
      { id: 'podman', displayName: 'Podman' },
      { id: 'helm', displayName: 'Helm' },
    ],
    namespaces: { kind: 'path', separator: '/', marker: '', nested: true },
    vulnerabilityScanning: true,
    signatureKinds: [
      { name: 'Notation', role: 'publisher' },
      { name: 'Sigstore bundle', role: 'publisher' },
      { name: 'cosign signature', role: 'publisher' },
      { name: 'provenance attestation', role: 'provenance' },
      { name: 'attestation or SBOM', role: 'attestation' },
    ],
  }),
  format({
    name: 'deb',
    displayName: 'Debian (APT)',
    clients: [{ id: 'apt', displayName: 'APT (apt, apt-get)' }],
    signatureKinds: [{ name: 'release signature', role: 'registry' }],
  }),
  format({
    name: 'ansible',
    displayName: 'Ansible Galaxy',
    clients: [{ id: 'ansible-galaxy', displayName: 'Ansible Galaxy (ansible-galaxy)' }],
    namespaces: { kind: 'namespace', separator: '.', marker: '', nested: false },
    wiki: true,
    signatureKinds: [
      { name: 'signing service signature', role: 'registry' },
      { name: 'uploaded signature', role: 'publisher' },
    ],
  }),
  format({
    name: 'generic',
    displayName: 'Generic',
    classes: ['local'],
    patternSubject: 'path',
    clients: [
      { id: 'curl', displayName: 'curl' },
      { id: 'wget', displayName: 'wget' },
      { id: 'powershell', displayName: 'PowerShell (Invoke-WebRequest)' },
    ],
    signatureKinds: [{ name: 'signature file', role: 'publisher' }],
  }),
  format({
    name: 'clamav',
    displayName: 'ClamAV definitions',
    classes: ['local'],
    patternSubject: 'path',
    clients: [{ id: 'freshclam', displayName: 'freshclam' }],
    systemOnly: true,
  }),
];
