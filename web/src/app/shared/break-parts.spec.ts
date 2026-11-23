import { renderScreen } from '../testing/screen';
import { ADDRESS_MARKS, IDENTITY_MARKS, KEY_MARKS, PartsText, breakParts } from './break-parts';

describe('breakParts', () => {
  it('Q-UI-959 an address breaks after its dots, slashes and colons, a run of them staying with the piece before', () => {
    expect(breakParts('https://packages.frankfurt.example.com:8443/bastion/', ADDRESS_MARKS)).toEqual([
      'https://',
      'packages.',
      'frankfurt.',
      'example.',
      'com:',
      '8443/',
      'bastion/',
    ]);
  });

  it('Q-UI-567 an audit action\'s key breaks after a dot or an underscore, never inside a word', () => {
    expect(breakParts('bastion.data_key.handed', KEY_MARKS)).toEqual(['bastion.', 'data_', 'key.', 'handed']);
    expect(breakParts('user.groups.claim_missing', KEY_MARKS)).toEqual(['user.', 'groups.', 'claim_', 'missing']);
  });

  it('Q-UI-969 a signer\'s identity breaks after a slash, a dot, an at sign or a comma; a word stays whole', () => {
    const url = 'https://gitlab.example.com/web/chart-render-kit//.gitlab-ci.yml@refs/heads/legacy-build';
    const parts = breakParts(url, IDENTITY_MARKS);
    expect(parts.join('')).toBe(url);
    expect(parts).toContain('legacy-build');
    expect(parts).toContain('chart-render-kit//.');
    expect(parts).toContain('yml@');
    expect(breakParts('CN=Example Code Signing CA, O=Example GmbH, C=DE', IDENTITY_MARKS)).toEqual([
      'CN=Example Code Signing CA,',
      ' O=Example GmbH,',
      ' C=DE',
    ]);
  });

  it('Q-UI-969 a version kept whole stays one piece in a path wherever it stands as a whole, with the marks after it', () => {
    const path = 'acme.logging.abstractions/2.0.0-preview.1/acme.logging.abstractions.2.0.0-preview.1.nupkg';
    expect(breakParts(path, IDENTITY_MARKS, '2.0.0-preview.1')).toEqual([
      'acme.',
      'logging.',
      'abstractions/',
      '2.0.0-preview.1/',
      'acme.',
      'logging.',
      'abstractions.',
      '2.0.0-preview.1.',
      'nupkg',
    ]);
    expect(breakParts('chart-render-kit/-/chart-render-kit-3.8.0.tgz', IDENTITY_MARKS, '3.8.0')).toEqual([
      'chart-render-kit/',
      '-/',
      'chart-render-kit-',
      '3.8.0.',
      'tgz',
    ]);
    // Inside a word it is no version: "1" in "node-v18" or "v1.2" is cut as usual.
    expect(breakParts('node-v18/v1.2/1.tgz', IDENTITY_MARKS, '1')).toEqual(['node-v18/', 'v1.', '2/', '1.', 'tgz']);
  });

  it('a value without a mark is one piece, an empty one none; the marks of a character class are taken as text', () => {
    expect(breakParts('legacy-build', IDENTITY_MARKS)).toEqual(['legacy-build']);
    expect(breakParts('', IDENTITY_MARKS)).toEqual([]);
    expect(breakParts('a-b]c^d', '-]^')).toEqual(['a-', 'b]', 'c^', 'd']);
  });

  it('Q-UI-969 a value is shown in whole pieces: a hyphen inside a piece is no place to break, the space before a piece stands between them', async () => {
    const url = await renderScreen(PartsText, {
      inputs: { value: 'https://gitlab.example.com/web/chart-render-kit//.gitlab-ci.yml@refs/heads/legacy-build', marks: IDENTITY_MARKS },
    });
    const pieces = [...url.root.querySelectorAll('.piece')].map((p) => p.textContent);
    expect(pieces).toContain('legacy-build');
    expect(pieces).toContain('chart-render-kit//.');
    expect(url.root.querySelectorAll('wbr')).toHaveLength(pieces.length);
    expect(url.root.textContent).toBe('https://gitlab.example.com/web/chart-render-kit//.gitlab-ci.yml@refs/heads/legacy-build');

    const dn = await renderScreen(PartsText, { inputs: { value: 'CN=Example Code Signing CA, O=Example GmbH, C=DE', marks: IDENTITY_MARKS } });
    expect([...dn.root.querySelectorAll('.piece')].map((p) => p.textContent)).toEqual(['CN=Example Code Signing CA,', 'O=Example GmbH,', 'C=DE']);
    expect(dn.root.textContent).toBe('CN=Example Code Signing CA, O=Example GmbH, C=DE');
  });
});
