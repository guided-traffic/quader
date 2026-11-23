import german from '../../locale/messages.de.xlf' with { loader: 'text' };

/**
 * The German pages address the reader formally, as "Sie" (web/src/locale/GLOSSARY.md). The look at the
 * German Set up client of the screens' second round (G5-60) found "Die Adresse, die du gerade nutzt." under the target **This
 * installation**, and a search found twelve more texts of the Bastions, the tokens and the members in the informal form. A German
 * target holds none of the informal pronouns, and none of the informal imperatives those texts used.
 */
const INFORMAL = /\b(du|dich|dir|dein|deine|deinen|deinem|deiner|deines)\b/i;
const IMPERATIVES =
  /(^|[.,;:]\s+|\bdann\s+)(Gib|Stelle|Trage|Registriere|Widerrufe|Prüfe|Wähle|Klicke|Öffne|Lies|Kopiere|Füge|Lege|Speichere|Erstelle|Lösche|Entferne|Ändere|Aktiviere|Nutze|Warte|Starte|Setze)\s+(?!Sie\b)/i;

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
    for (const text of ['Die Adresse, die du gerade nutzt.', 'Gib eine Anzahl Tage ein.', 'Bastion angelegt. Stelle als Nächstes ein Token aus.', 'Wer das Projekt besitzt, deine Rolle darin.']) {
      expect(INFORMAL.test(text) || IMPERATIVES.test(text), text).toBe(true);
    }
    for (const text of ['Die Adresse, die Sie gerade nutzen.', 'Geben Sie eine Anzahl Tage ein.', 'An anderer Stelle abgeschwächt', 'Stellen Sie eines aus.']) {
      expect(INFORMAL.test(text) || IMPERATIVES.test(text), text).toBe(false);
    }
  });
});
