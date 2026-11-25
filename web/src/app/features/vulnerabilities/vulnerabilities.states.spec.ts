import { describe, it } from 'vitest';
import { MalwareApi } from '../../api/malware-api';
import { VulnerabilitiesApi } from '../../api/vulnerabilities-api';
import { eicarFind, goProgramFind } from '../../api/testing/fakes-malware';
import { SignaturesApi } from '../../api/signatures-api';
import { AnchorDialog } from '../../shared/signatures/anchor-dialog';
import { BlockDialog } from '../../shared/signatures/block-dialog';
import { GateReleaseDialog } from '../../shared/signatures/gate-release-dialog';
import { LONG_TEXT, click, expectState, failNext, fieldRefusal, fill, openDialog, serverFailure, submit } from '../../testing/states';
import { MalwareReleaseDialog } from './malware-release-dialog';
import { TriageDialog, TriageDialogData, TriageTarget } from './triage-dialog';

// The dialogs of the security screens: a finding's triage (vulnerability-finding.ts, vulnerability-report.ts), a malware find's
// release (malware-finds.ts), and the signatures' trust anchors, blocks and gate releases (signature-settings.ts,
// project-signature-anchors.ts, gate-releases.ts).

/** Twelve findings of a selection, labelled as the report labels them: package@version · advisory · repository. */
const targets: TriageTarget[] = Array.from({ length: 12 }, (_, i) => ({
  id: `finding-${i}`,
  label: `@platform-engineering/kubernetes-infrastructure-operators@18.6.0-rc.${i} · GHSA-${String(1000 + i)}-q8v7-m2k5 · platform-engineering/release-candidates-npm`,
  repository: i % 2 === 0 ? 'platform-engineering/release-candidates-npm' : 'kubernetes-infrastrukturbetrieb/npmjs-remote',
}));
const bulk: TriageDialogData = { mode: 'bulk', targets, status: 'blocked', choices: ['needs_triage', 'confirmed', 'blocked', 'dismissed'] };
const triage = (data: TriageDialogData = bulk) => openDialog(TriageDialog, { maxWidth: '40rem', data });

const find = eicarFind(['platform-engineering/release-candidates-generic', 'kubernetes-infrastrukturbetrieb/artefakte-generic'], {
  items: [
    {
      repository: 'platform-engineering/release-candidates-generic',
      format: 'generic',
      path: 'kubernetes/operators/2026/bereitstellungsverantwortlichkeitsuebertragung/installer-18.6.0-rc.12.tar.gz',
      package: null,
      version: null,
    },
  ],
});
/** A layer of an image whose Go program clamd's RAR parser read past the scan-size limit (Q-MAL-106), in two repositories. */
const goFind = goProgramFind(['platform-engineering/release-candidates-oci', 'kubernetes-infrastrukturbetrieb/dockerhub-remote'], {
  part: 'usr/local/bin/kubernetes-infrastructure-operator-manager',
});
const gate = () =>
  openDialog(GateReleaseDialog, {
    maxWidth: '40rem',
    data: {
      repository: 'platform-engineering/release-candidates-oci',
      sha256: 'sha256:9f2c4e8a1b7d3f6e0a5c9b2d8e4f1a7c3b6d0e9f2a5c8b1d4e7f0a3c6b9d2e5f',
      artifact: 'kubernetes-infrastructure-operators:18.6.0-rc.12',
      reason: 'unsigned',
    },
  });

describe('dialogs of the security screens in a browser (R-VULN-022, R-MAL-022, R-SIG-015, R-SIG-070, R-UI-042, R-UI-173, R-UI-175)', () => {
  it('Change the status of twelve selected findings to Blocked, as it opens', async () => {
    await expectState('triage-bulk', () => triage());
  });

  it('Dismiss a finding, submitted without a reason: the sentence under the field', async () => {
    await expectState('triage-invalid', async () => {
      const { panel } = await triage({ ...bulk, status: 'dismissed' });
      await fill(panel, 'Comment', LONG_TEXT);
      await submit(panel);
    });
  });

  it('Change the status of the selected findings, failed: the error notice with its request ID', async () => {
    await expectState('triage-failed', async () => {
      const { panel, get } = await triage({ ...bulk, status: 'confirmed' });
      failNext(get(VulnerabilitiesApi), 'setStatus', serverFailure());
      await submit(panel);
    });
  });

  it("Release a malware find, failed: the error notice with its request ID", async () => {
    await expectState('malware-release-failed', async () => {
      const { panel, get } = await openDialog(MalwareReleaseDialog, { maxWidth: '40rem', data: find });
      failNext(get(MalwareApi), 'release', serverFailure());
      await fill(panel, 'Reason', LONG_TEXT);
      await submit(panel);
    });
  });

  it('Release a layer whose Go program passed the scan-size limit: the limit, the false alarm, the scanner\'s text unfolded', async () => {
    await expectState('malware-release-go-program', async () => {
      const { panel } = await openDialog(MalwareReleaseDialog, { maxWidth: '40rem', data: goFind });
      await click(panel, /Technical details/);
    });
  });

  it('Add a trust anchor, its fields refused: each sentence under its field', async () => {
    await expectState('signature-anchor-invalid', async () => {
      const { panel, get } = await openDialog(AnchorDialog, { maxWidth: '44rem', data: { project: null } });
      failNext(
        get(SignaturesApi),
        'createAnchor',
        fieldRefusal({
          '/key': 'Another anchor has the key platform-engineering-release-signing-ca-2026.',
          '/ca/certificates': 'The second certificate is not a CA certificate: its basic constraints say CA:FALSE.',
        }),
      );
      await fill(panel, 'Title', 'Platform engineering release signing CA 2026');
      await fill(panel, 'Certificates (PEM)', '-----BEGIN CERTIFICATE-----\nMIIDdzCCAl+gAwIBAgIUQ3VhZGVy\n-----END CERTIFICATE-----');
      await submit(panel);
    });
  });

  it('Add a trust anchor, failed: the error notice with its request ID', async () => {
    await expectState('signature-anchor-failed', async () => {
      const { panel, get } = await openDialog(AnchorDialog, { maxWidth: '44rem', data: { project: 'platform-engineering' } });
      failNext(get(SignaturesApi), 'createProjectAnchor', serverFailure());
      await fill(panel, 'Title', 'Platform engineering release signing CA 2026');
      await fill(panel, 'Certificates (PEM)', '-----BEGIN CERTIFICATE-----\nMIIDdzCCAl+gAwIBAgIUQ3VhZGVy\n-----END CERTIFICATE-----');
      await submit(panel);
    });
  });

  it('Block a signer, its fields refused: each sentence under its field', async () => {
    await expectState('signature-block-invalid', async () => {
      const { panel, get } = await openDialog(BlockDialog, { maxWidth: '36rem' });
      failNext(
        get(SignaturesApi),
        'createBlock',
        fieldRefusal({
          '/value': 'An identity is an e-mail address or a URI: release-signing@platform-engineering is neither.',
          '/reason': 'A block needs a reason of 1 to 1,000 characters.',
        }),
      );
      await fill(panel, 'Value', 'release-signing@platform-engineering');
      await submit(panel);
    });
  });

  it('Block a signer, failed: the error notice with its request ID', async () => {
    await expectState('signature-block-failed', async () => {
      const { panel, get } = await openDialog(BlockDialog, { maxWidth: '36rem' });
      failNext(get(SignaturesApi), 'createBlock', serverFailure());
      await fill(panel, 'Value', 'https://github.com/platform-engineering/release-tooling/.github/workflows/release.yml@refs/heads/main');
      await fill(panel, 'Reason', LONG_TEXT);
      await submit(panel);
    });
  });

  it('Release an artifact for one repository, submitted without a reason: the sentence under the field', async () => {
    await expectState('signature-gate-release-invalid', async () => {
      const { panel } = await gate();
      await submit(panel);
    });
  });

  it('Release an artifact for one repository, failed: the error notice with its request ID', async () => {
    await expectState('signature-gate-release-failed', async () => {
      const { panel, get } = await gate();
      failNext(get(SignaturesApi), 'release', serverFailure());
      await fill(panel, 'Reason', LONG_TEXT);
      await submit(panel);
    });
  });
});
