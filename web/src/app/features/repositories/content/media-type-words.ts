/**
 * The word a person knows for a file's media type (R-GENERIC-017, Q-UI-1333), shown with the media type beside it, as an image's page
 * shows an artifact's kind: the text types a file deployed without one is given from its name, and the kinds of file a generic
 * repository commonly holds. Parameters (`; charset=utf-8`) and case do not matter. A type without a word here, and
 * `application/octet-stream`, which says only that the type is not known, give '': the page shows the media type alone.
 */
export function mediaTypeWord(mediaType: string): string {
  const base = mediaType.split(';')[0]!.trim().toLowerCase();
  switch (base) {
    case 'text/markdown':
      return 'Markdown';
    case 'text/plain':
      return $localize`:@@files.media.text:Text`;
    case 'application/json':
      return 'JSON';
    case 'application/yaml':
    case 'application/x-yaml':
    case 'text/yaml':
      return 'YAML';
    case 'application/toml':
      return 'TOML';
    case 'text/csv':
      return 'CSV';
    case 'text/html':
      return 'HTML';
    case 'application/xml':
    case 'text/xml':
      return 'XML';
    case 'application/pdf':
      return 'PDF';
    case 'image/svg+xml':
      return $localize`:@@files.media.svg:SVG image`;
    case 'image/png':
      return $localize`:@@files.media.png:PNG image`;
    case 'image/jpeg':
      return $localize`:@@files.media.jpeg:JPEG image`;
    case 'application/zip':
      return $localize`:@@files.media.zip:ZIP archive`;
    case 'application/gzip':
    case 'application/x-gzip':
      return $localize`:@@files.media.gzip:gzip archive`;
    case 'application/x-tar':
      return $localize`:@@files.media.tar:tar archive`;
  }
  return '';
}
