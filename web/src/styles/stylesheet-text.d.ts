// A stylesheet imported with the `text` loader of the Angular builder
// (`import text from './main.scss' with { loader: 'text' }`) is its source as a string. Only tests do that (palette.spec.ts,
// layout-rules.spec.ts, the severity badge's vuln-parts.spec.ts); the application never imports a stylesheet.
declare module '*.scss' {
  const text: string;
  export default text;
}
