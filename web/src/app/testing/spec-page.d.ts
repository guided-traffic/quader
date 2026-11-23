// A page of the specification imported with the `text` loader of the Angular builder
// (`import text from '…/docs/spec/api-and-ui.md' with { loader: 'text' }`) is its source as a string. Only app.route-table.spec.ts
// does that, to hold the route table of R-UI-003 to the router; the application imports no page of the specification.
declare module '*.md' {
  const text: string;
  export default text;
}
