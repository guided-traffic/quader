// A translation file imported with the `text` loader of the Angular builder
// (`import text from '…/messages.de.xlf' with { loader: 'text' }`) is its source as a string. Only clients.spec.ts does that, to
// hold the German fold labels of Set up client to their bound (Q-UI-810); the application is built with the translations inlined.
declare module '*.xlf' {
  const text: string;
  export default text;
}
