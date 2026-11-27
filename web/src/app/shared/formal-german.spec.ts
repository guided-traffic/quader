import german from '../../locale/messages.de.xlf' with { loader: 'text' };

/**
 * The German pages address the reader formally, as "Sie" (web/src/locale/GLOSSARY.md). The look at the
 * German Set up client of the screens' second round (G5-60) found "Die Adresse, die du gerade nutzt." under the target **This
 * installation**, and a search found twelve more texts of the Bastions, the tokens and the members in the informal form. A German
 * target holds none of the informal pronouns, and none of the informal imperatives those texts used.
 */
const INFORMAL = /\b(du|dich|dir|dein|deine|deinen|deinem|deiner|deines)\b/i;
const IMPERATIVES =
  /(^|[.,;:]\s+|\bdann\s+)(Gib|Stelle|Trage|Registriere|Widerrufe|Prüfe|Wähle|Klicke|Öffne|Lies|Kopiere|Füge|Lege|Speichere|Erstelle|Lösche|Entferne|Ändere|Aktiviere|Nutze|Warte|Starte|Setze|Nenne)\s+(?!Sie\b)/i;

function targets(): { id: string; text: string }[] {
  const out: { id: string; text: string }[] = [];
  for (const m of german.matchAll(/<unit id="([^"]+)">[\s\S]*?<target>([\s\S]*?)<\/target>/g)) {
    out.push({ id: m[1]!, text: m[2]!.replace(/<ph [^>]*\/>/g, ' ').replace(/<[^>]+>/g, ' ') });
  }
  return out;
}

describe('The German texts (G5-60)', () => {
  it('G5-60 every German target addresses the reader as Sie: no informal pronoun and no informal imperative', () => {
    const all = targets();
    expect(all.length).toBeGreaterThan(1000);
    const informal = all.filter((t) => INFORMAL.test(t.text) || IMPERATIVES.test(t.text)).map((t) => `${t.id}: ${t.text}`);
    expect(informal).toEqual([]);
  });

  it('G5-60 the check finds the informal forms it was written against', () => {
    for (const text of ['Die Adresse, die du gerade nutzt.', 'Gib eine Anzahl Tage ein.', 'Bastion angelegt. Stelle als Nächstes ein Token aus.', 'Für die Erneuerung: Nenne das Jahr.', 'Wer das Projekt besitzt, deine Rolle darin.']) {
      expect(INFORMAL.test(text) || IMPERATIVES.test(text), text).toBe(true);
    }
    for (const text of ['Die Adresse, die Sie gerade nutzen.', 'Geben Sie eine Anzahl Tage ein.', 'An anderer Stelle abgeschwächt', 'Stellen Sie eines aus.']) {
      expect(INFORMAL.test(text) || IMPERATIVES.test(text), text).toBe(false);
    }
  });
});

/**
 * "Token" is neuter in every German text, "das Token" (web/src/locale/GLOSSARY.md): the screens' second round (W9) found about 36
 * texts with "das Token" and 15 with "der Token", one gender on one page and the other on the next. The forms below are masculine
 * singular only; "den Token" and "seinen Token" are left out, since they are also the dative plural of the neuter ("mit den Token").
 */
const MASCULINE_TOKEN = [
  // An article or a possessive, or nothing at a sentence's start, with an adjective of the masculine nominative: "kein aktiver Token",
  // "Ausgestellter Token".
  /\b(ein|kein|Ihr|ihr|sein)\s+[a-zäöüß]+er\s+[\wÄÖÜäöüß-]*Token(?![\wÄÖÜäöüß-])/i,
  /(^|[.:;]\s+)[A-ZÄÖÜ][a-zäöüß]+er\s+[\wÄÖÜäöüß-]*Token(?![\wÄÖÜäöüß-])/,
  // The accusative: "einen ID-Token", "keinen Token", "einen kurzlebigen Token".
  /\b(einen|keinen)\s+([a-zäöüß]+en\s+)?[\wÄÖÜäöüß-]*Token(?![\wÄÖÜäöüß-])/i,
  // The nominative at a sentence's start: "Der Token", "Dieser Token", "Jeder Token", "Der eingefügte Token".
  /(^|[.:;]\s+)(Der|Dieser|Jeder|Welcher)\s+([a-zäöüß]+e\s+)?[\wÄÖÜäöüß-]*Token(?![\wÄÖÜäöüß-])/,
  // The masculine relative pronoun right after the noun: "Der Token, der ausgestellt würde", "den Token, den Sie einfügen".
  /[\wÄÖÜäöüß-]*Token,\s+(der|den)\b/,
];
const masculineToken = (text: string): boolean => MASCULINE_TOKEN.some((r) => r.test(text));

describe('The German word "Token" (R-UI-045)', () => {
  it('R-UI-045 every German target uses "Token" as a neuter noun, as the glossary has it', () => {
    const masculine = targets().filter((t) => masculineToken(t.text)).map((t) => `${t.id}: ${t.text}`);
    expect(masculine).toEqual([]);
  });

  it('R-UI-045 the check finds the masculine forms the texts had and passes the neuter and the plural', () => {
    for (const text of [
      'Der Token, der ausgestellt würde',
      'Kein aktiver Token passt zu den Filtern.',
      'Claims als JSON oder einen ID-Token einfügen',
      'Ausgestellter Token',
      'Jeder Token, den ein Job erhalten hat.',
      'Dieser Token wird nur einmal angezeigt.',
      'Der eingefügte Token wurde verworfen.',
      'Die Snippets enthalten den Token, den Sie einfügen.',
      'geben Sie jedem Werkzeug einen eigenen Token.',
    ]) {
      expect(masculineToken(text), text).toBe(true);
    }
    for (const text of [
      'Das Token, das ausgestellt würde',
      'Kein aktives Token passt zu den Filtern.',
      'Ausgestelltes Token',
      'Konten, jedes mit seinen technischen Token.',
      'Das Limit technischer Tokens.',
      'Hier nicht ausgewertet: der Token-Geltungsbereich.',
      'Der Job-Token-Fallback ist nicht verfügbar.',
      'Öffentlicher Aussteller (jeder kann Token von ihm erhalten)',
      'Persönliche Token',
    ]) {
      expect(masculineToken(text), text).toBe(false);
    }
  });
});

/**
 * A signer is "Signierer" in every German text (web/src/locale/GLOSSARY.md): ten texts of the signature gate, the trust anchors and the
 * refused downloads said "Unterzeichner" beside 38 with "Signierer", so the badge of a signature read "Signierer nicht
 * vertrauenswürdig" and the refusal beside it "Unterzeichner nicht akzeptiert".
 */
describe('The German word for a signer (R-UI-045)', () => {
  it('R-UI-045 every German target names a signer "Signierer", as the glossary has it, never "Unterzeichner"', () => {
    const other = targets().filter((t) => /Unterzeichner/i.test(t.text)).map((t) => `${t.id}: ${t.text}`);
    expect(other).toEqual([]);
  });
});
