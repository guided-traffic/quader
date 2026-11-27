/*
 * Whether a format's packages lead with their documentation, apart from the words of content-shape.ts: the links of a package (the
 * top bar's search among them) ask it on the first page, the content's words load with the pages that show content.
 */

/**
 * The formats whose packages lead with their documentation (R-UI-218): documentation that belongs to the format and is regularly
 * present is what a package's name opens. One line per format: a format joins by its key here.
 */
const LEADS_WITH_DOCUMENTATION: ReadonlySet<string> = new Set(['ansible', 'npm', 'nuget']);

/** Whether the packages of `format` lead with their documentation (R-UI-218): the routes and the links ask it without a shape. */
export function leadsWithDocumentation(format: string | null | undefined): boolean {
  return !!format && LEADS_WITH_DOCUMENTATION.has(format);
}
