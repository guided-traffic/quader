import { Provider } from '@angular/core';
import { Guide, GuideSection, GuidesApi } from '../guides-api';
import { ApiError } from '../problem';
import { refOf } from '../repositories-api';
import { ANSIBLE_REPOSITORIES, FakeClientSetupReads, GENERIC_REPOSITORY, NPM_REPOSITORIES, NUGET_REPOSITORIES, OCI_REPOSITORIES } from './fakes-packages';

/*
 * The in-memory fake of the guide operations (R-UI-047 component tier). Its sections have the ids,
 * topics and order of npm's guide and of the generic format's guide for curl, and its snippets are
 * the setup fake's answer for the same repository and client, beside the sections as the server
 * sends them (R-API-053, R-API-070): a reader who may not publish to the repository gets no
 * publishing, and the guide of a class carries the publish snippets for a signed-in reader of a
 * class that takes them.
 */

const BASE = 'https://quader.example.net';

const CLIENTS = ['npm', 'pnpm', 'yarn', 'yarn-classic'];

function section(id: string, topic: GuideSection['topic'], title: string, html: string): GuideSection {
  return { id, topic, title, html };
}

export class FakeGuidesApi extends GuidesApi {
  calls: { repository?: string; format?: string; cls?: string; client: string }[] = [];
  failure: ApiError | null = null;
  /** The reader is not signed in: snippets without a credential and no publishing sections. */
  anonymous = false;
  /** The endpoint serves under exposure rules. */
  exposureRules = false;
  private readonly setup = new FakeClientSetupReads();

  override async repository(key: string, client: string): Promise<Guide> {
    this.calls.push({ repository: key, client });
    this.fail();
    if (key === refOf(GENERIC_REPOSITORY)) return this.buildGeneric(key, client);
    const repo = NPM_REPOSITORIES.find((r) => refOf(r) === key);
    if (repo) return this.build(key, repo.class, client);
    const other = [...NUGET_REPOSITORIES, ...OCI_REPOSITORIES, ...ANSIBLE_REPOSITORIES].find((r) => refOf(r) === key);
    if (!other) throw new ApiError(400, { code: 'malformed_request', detail: 'no guide for this repository' }, null);
    return this.buildOther(key, other.format, other.class, client);
  }

  /** The guide of a NuGet, OCI or Ansible repository: the setup fake's snippets and the sections every guide has. */
  private async buildOther(key: string, format: string, cls: Guide['class'], client: string): Promise<Guide> {
    const credentials = this.anonymous ? 'none' : 'env';
    const setup = await this.setup.get(key, { client, credentials });
    return {
      format,
      class: cls,
      client,
      repository: key,
      registryUrl: setup.registryUrl,
      publishTarget: this.anonymous ? null : setup.publishTarget,
      credentials,
      exposureRules: this.exposureRules,
      snippets: setup.snippets,
      sections: [
        section(`${format}.${cls}.what`, 'what', `What a ${cls} repository is`, `<p>A ${cls} repository.</p>`),
        section(`${format}.configure`, 'configure', 'Point the client at the repository', '<p>Save the snippet.</p>'),
        section(`${format}.troubleshooting`, 'troubleshooting', 'Troubleshooting', '<table><tr><td>404</td></tr></table>'),
      ],
    };
  }

  override async format(format: string, cls: string, client: string): Promise<Guide> {
    this.calls.push({ format, cls, client });
    this.fail();
    if (format === 'generic' && cls === 'local') return this.buildGeneric(null, client);
    if (format !== 'npm') throw new ApiError(404, { code: 'not_found', detail: 'no such format' }, null);
    return this.build(null, cls as Guide['class'], client);
  }

  private fail(): void {
    if (this.failure) {
      const e = this.failure;
      this.failure = null;
      throw e;
    }
  }

  private async build(key: string | null, cls: Guide['class'], client: string): Promise<Guide> {
    if (!CLIENTS.includes(client)) {
      throw new ApiError(400, { code: 'malformed_request', detail: 'unknown client' }, null);
    }
    const credentials = this.anonymous ? 'none' : 'env';
    const setup = await this.setup.get(key ?? (cls === 'remote' ? 'web/npm-remote' : 'web/npm-all'), { client, credentials });
    const registryUrl = `${BASE}/api/npm/${key ?? '<project>/<repository>'}/`;
    const mayPublish = !this.anonymous && (key ? setup.publishTarget !== null : cls !== 'remote');
    const snippets = setup.snippets
      .filter((s) => s.tab !== 'publish' || mayPublish)
      .map((s) => ({ ...s, content: key ? s.content : s.content.replace(/\/web\/npm-(all|remote)\//g, '/<project>/<repository>/') }));
    const sections: GuideSection[] = [
      section(`npm.${cls}.what`, 'what', `What a ${cls} repository is`, `<p>A ${cls} repository.</p><h3>Details</h3><p>More.</p><pre><code>npm config get registry</code></pre>`),
      section('npm.configure', 'configure', 'Point the client at the repository', '<p>Save the snippet.</p>'),
      section(
        'npm.authenticate',
        'authenticate',
        'Sign the client in',
        this.anonymous ? '<p>No credential is needed.</p>' : '<p>Keep the token in <code>QUADER_TOKEN</code>.</p>',
      ),
      section('npm.install', 'install', 'Install packages', '<p>Install a package.</p>'),
    ];
    if (cls !== 'remote' && mayPublish) {
      sections.push(section('npm.publish', 'publish', 'Publish a package', '<p><code>npm publish</code></p>'));
    }
    if (this.exposureRules) {
      sections.push(section('npm.endpoint', 'endpoint', 'On this endpoint', '<p><code>endpoint_read_only</code></p>'));
    }
    sections.push(
      section('npm.troubleshooting', 'troubleshooting', 'Troubleshooting', '<table><tr><td>404</td></tr></table>'),
    );
    return {
      format: 'npm',
      class: cls,
      client,
      repository: key,
      registryUrl,
      publishTarget: key && setup.publishTarget && !this.anonymous ? setup.publishTarget : null,
      credentials,
      exposureRules: this.exposureRules,
      snippets,
      sections,
    };
  }

  private async buildGeneric(key: string | null, client: string): Promise<Guide> {
    if (client !== 'curl' && client !== 'wget' && client !== 'powershell') {
      throw new ApiError(400, { code: 'malformed_request', detail: 'unknown client' }, null);
    }
    const credentials = this.anonymous ? 'none' : 'env';
    const setup = await this.setup.get(refOf(GENERIC_REPOSITORY), { client, credentials });
    const mayPublish = !this.anonymous && (key ? setup.publishTarget !== null : true);
    const snippets = setup.snippets
      .filter((s) => s.tab !== 'publish' || mayPublish)
      .map((s) => ({ ...s, content: key ? s.content : s.content.replaceAll(`/${refOf(GENERIC_REPOSITORY)}/`, '/<project>/<repository>/') }));
    // curl's sections keep their ids; wget's and PowerShell's are their own (R-GENERIC-017).
    const id = (topic: string) => (client === 'curl' ? `generic.${topic}` : `generic.${client}.${topic}`);
    const name = client === 'powershell' ? 'PowerShell' : client;
    const sections: GuideSection[] = [
      section('generic.local.what', 'what', 'What a generic local repository is', '<p>Files at paths you choose.</p>'),
      section(id('configure'), 'configure', `Point ${name} at the repository`, '<p>Paste the snippet.</p>'),
      section(id('authenticate'), 'authenticate', `Sign ${name} in`, this.anonymous ? '<p>No credential is needed.</p>' : '<p>Keep the token in <code>QUADER_TOKEN</code>.</p>'),
      section(id('install'), 'install', 'Download files', '<p>Download a file.</p>'),
    ];
    if (mayPublish) {
      sections.push(section(id('publish'), 'publish', 'Upload files', '<p>Upload a file.</p>'));
    }
    if (this.exposureRules) {
      sections.push(section('generic.endpoint', 'endpoint', 'On this endpoint', '<p><code>endpoint_read_only</code></p>'));
    }
    sections.push(section('generic.troubleshooting', 'troubleshooting', 'Troubleshooting', '<table><tr><td>404</td></tr></table>'));
    return {
      format: 'generic',
      class: 'local',
      client,
      repository: key,
      registryUrl: `${BASE}/files/${key ?? '<project>/<repository>'}/`,
      publishTarget: key && setup.publishTarget && !this.anonymous ? setup.publishTarget : null,
      credentials,
      exposureRules: this.exposureRules,
      snippets,
      sections,
    };
  }
}

export function provideGuideFakes(): Provider[] {
  return [{ provide: GuidesApi, useClass: FakeGuidesApi }];
}
