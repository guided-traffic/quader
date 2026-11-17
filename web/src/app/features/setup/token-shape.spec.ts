import { base62, checkTokenShape, crc32 } from './token-shape';

/** The test vector of R-AUTH-022: 32 zero bytes behind the personal-token prefix. */
const VECTOR = 'qdr_pat_00000000000000000000000000000000000000000003INAaE';

describe('token shape check', () => {
  it('R-AUTH-022 R-UI-016 CRC-32/IEEE of a known string and the base62 form of the test vector agree with the server', () => {
    expect(crc32('123456789')).toBe(0xcbf43926);
    expect(crc32('qdr_pat_' + '0'.repeat(43))).toBe(0xb3ffeff6);
    expect(base62(0xb3ffeff6, 6)).toBe('3INAaE');
    expect(base62(0, 6)).toBe('000000');
  });

  it('R-AUTH-022 R-UI-016 the test vector passes, with or without surrounding space', () => {
    expect(VECTOR).toHaveLength(57);
    expect(checkTokenShape(VECTOR)).toBe('ok');
    expect(checkTokenShape(`  ${VECTOR}\n`)).toBe('ok');
  });

  it('R-AUTH-022 R-UI-016 flipping any single character after the prefix fails the checksum', () => {
    for (let i = 8; i < VECTOR.length; i++) {
      const flipped = VECTOR.slice(0, i) + (VECTOR[i] === 'z' ? 'y' : 'z') + VECTOR.slice(i + 1);
      expect(checkTokenShape(flipped), `position ${i}`).toBe('checksum');
    }
  });

  it('R-UI-016 names why a value is not a token: empty, prefix, length, alphabet', () => {
    expect(checkTokenShape('')).toBe('empty');
    expect(checkTokenShape('   ')).toBe('empty');
    expect(checkTokenShape('ghp_' + 'a'.repeat(53))).toBe('prefix');
    expect(checkTokenShape(VECTOR.slice(0, -1))).toBe('length');
    expect(checkTokenShape(VECTOR + '0')).toBe('length');
    expect(checkTokenShape('qdr_pat_' + '-'.repeat(49))).toBe('alphabet');
  });

  it('R-UI-016 the other two token types are recognised by their prefix', () => {
    for (const prefix of ['qdr_sat_', 'qdr_cit_']) {
      const body = prefix + '0'.repeat(43);
      const token = body + base62(crc32(body), 6);
      expect(checkTokenShape(token)).toBe('ok');
    }
  });
});
