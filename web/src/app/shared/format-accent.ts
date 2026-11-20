/**
 * A format's accent (Q-UI-803): the one colour a format adds to Quader's pictures, a pair of a light and a dark value in
 * `_tokens.scss` (`--quader-format-accent-<format>`). It is used only inside a picture, the parcels and the glyph of the flow on
 * **Set up client** and the line over a code window's tab; the page's own controls and text keep the app's palette. The UI knows no
 * list of formats (FMT-001): a format not named here takes the neutral pair, so a new format looks right before it has one.
 */
export function formatAccent(format: string | null | undefined): string {
  switch (format) {
    case 'npm':
    case 'oci':
    case 'nuget':
    case 'deb':
    case 'ansible':
    case 'generic':
      return `var(--quader-format-accent-${format})`;
    default:
      return 'var(--quader-format-accent)';
  }
}

/**
 * The glyph a format adds to the picture of the flow, on a grid of 24: a stack of layers for an image, a parcel for every other
 * format. Drawn here, never a logo of the ecosystem (Q-UI-225 to Q-UI-227).
 */
export function formatGlyph(format: string | null | undefined): 'layers' | 'parcel' {
  return format === 'oci' ? 'layers' : 'parcel';
}
