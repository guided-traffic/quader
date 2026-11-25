# English to German glossary

Product terms keep one German rendering across all translation files (R-UI-045). Add a row
before using a new term in a German text.

Every German text addresses the reader formally, as "Sie" ("Geben Sie … ein", "Ihre Rolle"), never as "du";
[formal-german.spec.ts](../app/shared/formal-german.spec.ts) refuses an informal pronoun or imperative in a target.

A sentence that names the reader is a unit of its own ("Started by you", "Gestartet von Ihnen"; "no rule for you", "keine Regel
für Sie"), never a sentence with a translated word for the reader put into it: German says the reader "Sie", "Ihnen" or "Ihr" by
the sentence's case, so "Gestartet von Sie" came of the cell's word "Sie" put after "von". The same holds for a kind of caller in a
sentence: a phrase of its own in the sentence's case ("für alle angemeldeten Benutzer"), never a list's label lowered to fit
([Q-UI-1219](../../../docs/spec/api-and-ui.md#q-ui-1219--a-sentence-that-names-the-reader-or-a-kind-of-caller-is-a-unit-of-its-own)).

A term keeps one grammatical gender in every text. "Token" is neuter, "das Token", the form most texts already used when the
screens' second round found 15 masculine ones beside about 36 neuter ones ([Q-UI-1052](../../../docs/spec/api-and-ui.md#q-ui-1052--token-is-neuter-in-every-german-text)); the same spec
refuses a masculine "Token" in a target.

A German word of more than 20 letters carries a soft hyphen, written `&#xAD;`, at a joint of its compound, so that no run of
letters is longer: `Authentifizierungs&#xAD;einstellungen`, `Berechtigungs&#xAD;richtlinien`. The browser draws nothing there
unless the line is too short, and then breaks at the joint with a hyphen; without it the word broke between two letters
("Authentifizierungseinstellung" / "en" in a heading at 390 px, [Q-UI-1091](../../../docs/spec/api-and-ui.md#q-ui-1091--a-long-german-word-carries-soft-hyphens-at-the-joints-of-its-compound-and-a-heading-never-breaks-inside-a-word)).
`make web-i18n-check` names a word without one. The table below writes the terms without their soft hyphens.

| English | German |
|---|---|
| permission policy | Berechtigungsrichtlinie |
| project (members; project policy, project service account) | Projekt (Mitglieder; Projekt-Richtlinie, Servicekonto des Projekts) |
| project member, project role (Reader, Developer, Maintainer, Owner) | Projektmitglied, Projektrolle (Leser, Entwickler, Maintainer, Eigentümer) |
| move (a repository to another project) | verschieben |
| repository | Repository |
| status | Status |
| management API | Management-API |
| user, group | Benutzer, Gruppe |
| service account | Servicekonto |
| system role | Systemrolle |
| token (personal, administration) | das Token, neuter in every text and in compounds (ein Token, kein aktives Token, das ID-Token; Persönliche Token, Token-Verwaltung) |
| endpoint, exposure rule | Endpunkt, Freigaberegel |
| audit log | Audit-Protokoll |
| replica | Replikat |
| drain, drain-safe | Drain, drain-sicher |
| degraded (replica, check) | eingeschränkt |
| stored-secret key, key ring | Schlüssel für gespeicherte Geheimnisse, Schlüsselring |
| sealed value | versiegelter Wert |
| sign in, sign out | anmelden, abmelden |
| session | Sitzung |
| files (of a repository), raw files | Dateien, Rohdateien |
| download address | Download-Adresse |
| principal | Prinzipal |
| role binding | Rollenzuweisung |
| system configuration (the installation's configuration file) | Systemkonfiguration |
| source (of a role binding: API, configuration) | Quelle (API, Konfiguration) |
| identity provider | Identitätsanbieter |
| break-glass account | Break-Glass-Konto |
| ceiling (configuration limit) | Obergrenze |
| draft (unsaved form content) | Entwurf |
| preset (of actions) | Vorlage |
| rotate (a token) | rotieren |
| revoke (a token) | widerrufen |
| group claim | Gruppen-Claim |
| client address | Client-Adresse |
| job, maintenance task | Job, Wartungsaufgabe |
| CI job (a pipeline job that signs in with a CI token) | CI-Job |
| upload, folder | Upload (hochladen), Ordner |
| exposure (mode, rule) | Freigabe (Modus, Regel) |
| surface (of an endpoint) | angebotene Routen |
| pre-release | Vorabversion |
| include / exclude pattern | Einschluss- / Ausschlussmuster |
| load more | Mehr laden |
| package, package name, owner (of a package) | Paket, Paketname, Eigentümer |
| release (a package name) | zurückgeben (not "freigeben", which renders exposure) |
| cache, upstream (of a remote repository) | Cache, Upstream |
| package type | Pakettyp |
| version (of a package) | Version |
| publish (a package) | veröffentlichen |
| install (a package) | installieren |
| client setup, snippet | Client-Einrichtung, Snippet |
| deployment target | Veröffentlichungsziel |
| README | README |
| ClamAV definition updates (the switch of upstream/clamav) | ClamAV-Definitions-Updates |
| set (of ClamAV databases), in use, outdated | Satz, in Verwendung, veraltet |
| detached signature (of a ClamAV container) | abgetrennte Signatur |
| recently viewed, saved view | Zuletzt angesehen, gespeicherte Ansicht |
| class (of a repository), format settings | Klasse, Formateinstellungen (`npm-Einstellungen`) |
| default (value of a setting) | Standard |
| guide (of a format), how to use (tab) | Anleitung, Verwendung |
| help (area), manual, contents, on this page | Hilfe, Handbuch, Inhalt, Auf dieser Seite |
| theme (light, dark, follow the system) | Design (Hell, Dunkel, Systemeinstellung folgen) |
| lockfile | Lockfile |
| unlisted (NuGet version), target framework | nicht gelistet, Zielframework |
| CI identity, CI issuer, mapping (of a CI issuer), tier (of mappings) | CI-Identität, CI-Aussteller, Zuordnung, Ebene |
| claim, claim tester, dry run | Claim, Claim-Tester, Testlauf |
| ID token, exchange (of an ID token for a token) | ID-Token, Austausch |
| inline grant, signing key, issuance (of a token) | Inline-Berechtigung, Signaturschlüssel, Ausstellung |
| Bastion (the read-only cache instance), registration (of a Bastion), handshake, grace period | Bastion (plural Bastionen), Registrierung, Handshake, Karenzzeit |
| direct reads from the object store (a Bastion's handover per blob), main installation | direkte Lesezugriffe auf den Objektspeicher, Hauptinstallation |
| finding (a version affected by an advisory), advisory | Befund, Advisory |
| vulnerability, malicious package | Schwachstelle, schädliches Paket |
| severity (critical, high, medium, low, none, unknown) | Schweregrad (Kritisch, Hoch, Mittel, Niedrig, Keiner, Unbekannt) |
| triage, needs triage | Triage, Triage nötig |
| confirm, dismiss (a finding), dismissed | bestätigen, verwerfen, verworfen |
| block (downloads), lift a block | sperren (Downloads sperren), Sperre aufheben |
| known exploited (CISA list) | bekannt ausgenutzt |
| resolved (finding) | behoben |
| grade (of a version, S to E and N/A), severity bar, end of life (of an OS), scanned | Bewertung, Schweregrad-Balken, Support-Ende, geprüft |
| advisory source, mirror repository | Advisory-Quelle, Spiegel-Repository |
| security dashboard (admin area), advisory ID (CVE, GHSA) | Sicherheits-Dashboard, Advisory-Kennung |
| mirror (mode of a remote repository), pull-through, pass (of a mirror) | Mirror, Pull-through, Durchlauf |
| mirror state (off, paused, no bound, scheduled, running, waiting, bound reached, failing) | Aus, Pausiert, Keine Grenze, Geplant, Läuft, Wartet, Grenze erreicht, Fehlerhaft |
| Mirror only these packages (`remote.mirror.include`), the packages to mirror | Nur diese Pakete im Mirror, die zu mirrorenden Pakete |
| pause, resume (a mirror); run a pass now, run a full pass | pausieren, fortsetzen; Durchlauf jetzt starten, Vollständigen Durchlauf starten |
| rate-limit wait, bound (of a mirror), discovery (of names) | Rate-Limit-Wartezeit, Grenze, Ermittlung |
| download queue (of a remote repository), bulk download, download state (waiting, running, failed, done, cancelled), retry, attempt, probe (a client's question about a version that starts a download) | Download-Warteschlange, Sammeldownload, Wartet, Läuft, Fehlgeschlagen, Fertig, Abgebrochen, wiederholen, Versuch, Abfrage |
| keep in sync, kept in sync, sync (a download that repeats, R-CUR-027), once, run (of a sync), run now, last run, next run, interval (of the syncs) | aktuell halten, aktuell gehalten, Synchronisierung, einmal, Lauf, jetzt ausführen, letzter Lauf, nächster Lauf, Intervall |
| allow, allowed (a delivery rule; not a gate's release, which is "zulassen", nor exposure, which is "freigeben"), block, blocked (by a rule), not allowed | erlauben, Erlaubt, sperren, Gesperrt (where a vulnerability block could be meant: "durch eine Regel gesperrt"), Nicht erlaubt |
| delivery mode (Everything except what is blocked, Only what is allowed), delivery rule, delivered, sub-section (a rule's name pattern) | Auslieferungsmodus (Alles außer Gesperrtem, Nur Erlaubtes), Auslieferungsregel, Ausgeliefert, Teilbereich |
| request (an ask for an allow), requested, decline, withdraw (a request), discard (what was fetched for examination) | beantragen, Antrag, Beantragt, ablehnen, zurückziehen, verwerfen |
| Deliver {name} {version} (a switch's name), a setting of its own (a version's rule), reset them (the version rules), Deliver only what is allowed?, change the mode | {name} {version} ausliefern, eigene Einstellung, zurücksetzen, Nur Erlaubtes ausliefern?, Modus ändern |
| range of versions (of a package's allow, R-CUR-026), Limit versions…, All versions, Versions from … up to / below …, Outside the range (a version's badge), Rule: the package's range | Versionsbereich (never "Teilbereich", which is a rule's name pattern), Versionen eingrenzen…, Alle Versionen, Versionen ab … bis / bis unter …, Außerhalb (one word: a reader's badge, Beantragen and In den Cache laden keep the table's form at 1,024 px; the rule line under it names the Versionsbereich), Regel: Versionsbereich des Pakets |
| download ahead (into the cache, before a client asks), fetch for examination, stored for examination, the tab Upstream, upstream catalogue | In den Cache laden, zur Prüfung holen, Zur Prüfung gespeichert, der Reiter Upstream, Upstream-Katalog |
| test connection, upstream URL, negative cache | Verbindung testen, Upstream-URL, negativer Cache |
| keyword (npm), maintainer (npm), package ID (NuGet), short description (Debian), the hint under the upstream search ("offers no search", "enter an image's full name") | Schlüsselwort, Maintainer, Paket-ID, Kurzbeschreibung ("bietet keine Suche", "geben Sie den vollständigen Namen eines Images ein") |
| licence (of the software the build bundles, its notices) | Lizenz (der eingebundenen Software, Hinweise) |
| Debian relations (R-DEB-038): Depends, Pre-Depends, Recommends, Suggests, Enhances, Breaks, Conflicts, Replaces, Provides; source package | the verbs of APT's German output: Hängt ab von, Hängt vorab ab von, Empfiehlt, Schlägt vor, Erweitert, Beschädigt, Kollidiert mit, Ersetzt, Stellt bereit; Quellpaket |
| changelog, copyright file, installed files, package file, data archive (Debian, R-DEB-039) | Changelog (der), Copyright-Datei, Installierte Dateien, Paketdatei, Datenarchiv |
| project switcher, start page of the projects ("Your work"), recently visited (projects) | Projektwechsler, "Ihre Arbeit", Zuletzt besucht |
| storage (the logical size a project is charged), physical size | Speicher (logische Größe, die dem Projekt angerechnet wird), physische Größe |
| backup, backup run, stored backup, backup target | Backup, Backup-Lauf, gespeichertes Backup, Backup-Ziel |
| blob, snapshot, pod (as in the backups screen) | Blob, Snapshot, Pod |
| avatar | Avatar |
| pull (an image), image index, multi-platform image | ziehen, Image-Index, Multi-Plattform-Image |
| Pull, Install, Download (the button beside a version that opens its commands, Q-UI-1066) | Pull (the owner's word and the command's, `docker pull`), Installieren, Herunterladen |
| image (OCI), tag, digest, media type, artifact type, platform (of an image index), image pull secret | Image, Tag, Digest, Medientyp, Artefakttyp, Plattform, Image-Pull-Secret |
| an image's configuration, environment, label, layer, build history, entrypoint, health check (formats/oci.md R-OCI-040) | Konfiguration, Umgebung, Label, Layer (Docker's word, kept), Build-Verlauf, Entrypoint (the Dockerfile's word, kept), Healthcheck |
| Helm chart, chart version, app version, values (a chart's `values.yaml`), maintainer (R-OCI-042) | Helm-Chart, Chart-Version, App-Version, Werte, Maintainer |
| signing key (primary, next, withheld, retired), fingerprint, certificate (public) | Signaturschlüssel (primär, nächster, zurückgehalten, außer Dienst), Fingerabdruck, öffentliches Zertifikat |
| promote (a next key), reinstate (a withheld key), confirm the retirement | hochstufen, wieder einsetzen, Außerdienststellung bestätigen |
| suite, component, architecture, index (of a suite), release (APT), generation | Suite, Komponente, Architektur, Index, Release, Generation |
| collection (Ansible), import task, tombstone (a deleted version number) | Collection, Import-Aufgabe, Grabstein |
| documentation (of a collection), module, plugin, role, parameter, suboption, return value | Dokumentation, Modul, Plugin, Rolle, Parameter, Unteroption, Rückgabewert |
| import log (tab of a version), docs site (link) | Import-Protokoll, Dokumentations-Website |
| terms, input, positional parameters, keyword parameters (sections of a lookup, a filter or a test plugin) | Terme, Eingabe, Positionsparameter, Schlüsselwortparameter |
| configuration of an option (INI entry, environment variable, variable, keyword, command line) | Konfiguration (INI-Eintrag, Umgebungsvariable, Variable, Schlüsselwort, Befehlszeile) |
| visibility, visibility level (Private, Internal, Public), wiki visibility | Sichtbarkeit, Sichtbarkeitsstufe (Privat, Intern, Öffentlich), Sichtbarkeit des Wikis |
| upstream registry (a well-known repository of the project upstream), built-in (project), listed by (virtual repositories) | Upstream-Registry (Plural Upstream-Registrys), integriert, aufgenommen von |
| registries of this installation (an administrator's registry presets, R-REPO-076), preset | Registrys dieser Installation, Vorgabe |
| switch on, switch off, on, off, switch all on | einschalten, ausschalten, an, aus, alle einschalten |
| not switched on, switched off, no longer offered, placeholder (member), take no part, skipped | nicht eingeschaltet, ausgeschaltet, nicht mehr angeboten, Platzhalter, wirkt nicht mit, übersprungen |
| account (of an upstream), archive keys, image scanning | Konto, Archivschlüssel, Image-Analyse |
| system repository (a repository of the project upstream that Quader keeps for its own work), System (its badge among other repositories) | System-Repository (Plural System-Repositories), System |
| malware, malware scan, find (a file in which the scan found malware), signatures (of the malware scan) | Malware, Malware-Prüfung, Malware-Befund, Malware-Signaturen |
| release (a file the malware scan did not pass, for download; an artifact a repository's signature gate does not pass, for that repository), withdraw (a release) | zum Download zulassen (Zulassung; not "freigeben", which renders exposure), zurückziehen |
| signature database, checked (an artifact with a verdict), verdict (of the malware scan) | Signaturdatenbank, geprüft, Prüfergebnis |
| scanner replica, image scanner, tool (that a scanner runs), slots (of a scanner), scan listener, state changed | Scanner-Replikat, Image-Scanner, Werkzeug, Slots, Scan-Listener, Zustand geändert |
| signature (of an artifact), signer, sign, invalid signature | Signatur, Signierer, signieren, ungültige Signatur |
| trust anchor, project trust anchor, trust root (of a Sigstore), public Sigstore root | Vertrauensanker, Projekt-Vertrauensanker, Vertrauensbasis, öffentliche Sigstore-Vertrauensbasis |
| Verified · public, Verified · internal, Signed · signer not trusted (the badges of a signature; the last one is the badge **Signed** with "signer not trusted" after it, Q-UI-1076) | Verifiziert · öffentlich, Verifiziert · intern, Signiert · Signierer nicht vertrauenswürdig (Badge **Signiert**, danach „Signierer nicht vertrauenswürdig“) |
| publisher signature, author signature, provenance attestation, attestation, SBOM, origin confirmed by registry | Herausgebersignatur, Autorensignatur, Provenienz-Attestierung, Attestierung, SBOM, Herkunft von der Registry bestätigt |
| block a signer, blocked signer, transparency log, timestamp, keyless | Signierer sperren, gesperrter Signierer, Transparenzprotokoll, Zeitstempel, schlüssellos |
| origin (of an upload), uploaded by, fetched from, badge (of a signature) | Herkunft, hochgeladen von, geholt von, Abzeichen |
| signing identity (Quader's own), certificate request, announced (identity), previous (identity), retire (an identity), chain, trusted root (of Quader's signatures), npm key ID, receipt attestation | Signaturidentität, Zertifikatsanforderung, angekündigt, früher, außer Dienst stellen, Kette, Vertrauensbasis, npm-Schlüssel-ID, Empfangsattestierung |
| object-store connection, fill, backlog, scrub, reconciliation, storage summary, storage key | Objektspeicher-Verbindung (kurz Verbindung), Füllstand, Rückstand, Prüflauf, Abgleich, Speicherübersicht, Speicherschlüssel |
| filling, active, draining, drained, detached, retired (the states of a connection) | Wird befüllt, Aktiv, Wird geleert, Geleert, Abgekoppelt, Stillgelegt |
| technical details (a server's own text, folded under the sentence of the screen), verifier's note (of a signature) | Technische Details, Hinweis der Prüfung |
| source network (of a remote repository) | Quellnetzwerk |
