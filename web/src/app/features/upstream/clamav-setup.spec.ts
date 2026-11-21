import { freshclamLines } from './clamav-setup';

describe('The one line of freshclam.conf (R-CLAMAV-006)', () => {
  it('R-CLAMAV-006 Q-CLAMAV-16 writes the address with its scheme and without its trailing slash, and the token as the password of the address', () => {
    expect(freshclamLines('https://quader.example.com/api/clamav/upstream/clamav/')).toEqual({
      plain: 'PrivateMirror https://quader.example.com/api/clamav/upstream/clamav',
      token: 'PrivateMirror https://clamav:${QUADER_TOKEN}@quader.example.com/api/clamav/upstream/clamav',
    });
    expect(freshclamLines('')).toEqual({ plain: '', token: '' });
  });
});
