import { personName } from './person-name';

describe('personName (Q-UI-1006)', () => {
  it('Q-UI-1006 a user is named by the username, every other kind with its kind as a word, never in a reference\'s syntax', () => {
    expect(personName('user:priya')).toBe('priya');
    expect(personName('sa:ci-build')).toBe('service account ci-build');
    expect(personName('sa:web/deploy')).toBe('service account web/deploy');
    expect(personName('group:gh-ops')).toBe('group gh-ops');
    expect(personName('bastion:dmz-frankfurt')).toBe('Bastion dmz-frankfurt');
    expect(personName('ci:acme/web')).toBe('CI job of acme/web');
  });

  it('Q-UI-1006 no reference is someone; a value without a kind and a kind it does not know stay as they are', () => {
    expect(personName(null)).toBe('someone');
    expect(personName('')).toBe('someone');
    expect(personName('0193a000-0000-7000-8000-000000000001')).toBe('0193a000-0000-7000-8000-000000000001');
    expect(personName('oidc:corp')).toBe('oidc:corp');
  });
});
