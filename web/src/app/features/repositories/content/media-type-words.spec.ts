import { mediaTypeWord } from './media-type-words';

describe('The word of a media type', () => {
  it('R-GENERIC-017 names the guessed text types and common kinds of file, whatever their parameters and case; an unknown type and octet-stream get none', () => {
    expect(mediaTypeWord('text/markdown')).toBe('Markdown');
    expect(mediaTypeWord('Text/Plain; charset=utf-8')).toBe('Text');
    expect(mediaTypeWord('application/yaml')).toBe('YAML');
    expect(mediaTypeWord('application/zip')).toBe('ZIP archive');
    expect(mediaTypeWord('application/octet-stream')).toBe('');
    expect(mediaTypeWord('application/x-widget')).toBe('');
    expect(mediaTypeWord('')).toBe('');
  });
});
