import { SignatureGatePreview, SignatureGateReason, SignatureGateRefusedReason, SignatureImpact } from '../../api/signatures-api';

/**
 * The words of a repository's gate (R-SIG-068, R-SIG-074): one place for what each reason is called and what it says, so that the
 * version's banner, a file's fact, the details, the release dialog and the security dashboard say the same as the client's answer. They
 * speak of the signature of the publisher or the build, the only one that counts (R-SIG-062, Q-SIG-212), and never name a signer.
 */

/** The short word of a reason: a fact, a column, a chip. */
export function gateReasonWord(reason: SignatureGateRefusedReason): string {
  switch (reason) {
    case 'missing':
      return $localize`:@@sig.gate.word.missing:No publisher's signature`;
    case 'untrusted':
      return $localize`:@@sig.gate.word.untrusted:Signer not accepted`;
    case 'not_chosen':
      return $localize`:@@sig.gate.word.not-chosen:Anchor not accepted`;
    case 'invalid':
      return $localize`:@@sig.gate.word.invalid:Invalid signature`;
    case 'blocked':
      return $localize`:@@sig.gate.word.blocked:Signer blocked`;
    case 'not_checked':
      return $localize`:@@sig.gate.word.not-checked:Signature not checked`;
    default:
      return $localize`:@@sig.state.not-checked:Not checked yet`;
  }
}

/** What a reason says of the artifact, as a sentence of its own (the details, the release dialog); the texts of R-SIG-068 in the UI's voice. */
export function gateReasonSentence(reason: SignatureGateRefusedReason): string {
  switch (reason) {
    case 'missing':
      return $localize`:@@sig.gate.sentence.missing:It has no signature of its publisher or its build.`;
    case 'untrusted':
      return $localize`:@@sig.gate.sentence.untrusted:No trust anchor this repository accepts knows the signer of its publisher's or its build's signature.`;
    case 'not_chosen':
      return $localize`:@@sig.gate.sentence.not-chosen:Its publisher's or its build's signature is verified only by trust anchors this repository does not accept.`;
    case 'invalid':
      return $localize`:@@sig.gate.sentence.invalid:Its publisher's or its build's signature does not match its content.`;
    case 'blocked':
      return $localize`:@@sig.gate.sentence.blocked:An administrator blocked the signer of its publisher's or its build's signature.`;
    case 'not_checked':
      return $localize`:@@sig.gate.sentence.not-checked:Its publisher's or its build's signature could not be checked: Quader cannot read it, or its check failed.`;
    default:
      return $localize`:@@sig.gate.sentence.pending:Its signatures are not checked yet.`;
  }
}

/** The refused counts of a preview with their reason, in the order of R-SIG-062, those above zero only. */
export function previewRefusals(p: SignatureGatePreview): readonly { readonly reason: SignatureGateReason; readonly count: number }[] {
  const r = p.refused;
  const all: { reason: SignatureGateReason; count: number }[] = [
    { reason: 'missing', count: r.missing },
    { reason: 'untrusted', count: r.untrusted },
    { reason: 'not_chosen', count: r.notChosen },
    { reason: 'invalid', count: r.invalid },
    { reason: 'blocked', count: r.blocked },
    { reason: 'not_checked', count: r.notChecked },
  ];
  return all.filter((x) => x.count > 0);
}

/** How many artifacts a preview refuses, every reason together. */
export function previewRefused(p: SignatureGatePreview): number {
  return previewRefusals(p).reduce((sum, x) => sum + x.count, 0);
}

/** A number as the build's language writes it (1,240 or 1.240). */
function n(value: number): string {
  return value.toLocaleString((typeof $localize !== 'undefined' && $localize.locale) || 'en-US');
}

/**
 * The state line of the stored setting (R-SIG-074): "<n> of <m> artifacts pass. <k> are refused.", with the checks that wait, and
 * "At least" where the preview stopped early (R-SIG-072).
 */
export function previewStateLine(p: SignatureGatePreview): string {
  const pass = n(p.pass);
  const of = n(p.artifacts);
  const refused = n(previewRefused(p));
  const head = p.complete
    ? $localize`:@@sig.gate.state:${pass}:pass: of ${of}:artifacts: artifacts pass. ${refused}:refused: are refused.`
    : $localize`:@@sig.gate.state-partial:Of the first ${of}:artifacts: artifacts read, ${pass}:pass: pass and ${refused}:refused: are refused; the preview stopped before the end.`;
  if (p.pending === 0) return head;
  const pending = n(p.pending);
  return `${head} ${$localize`:@@sig.gate.state-pending:${pending}:pending: are still being checked.`}`;
}

/**
 * The question before a narrowing is saved (R-SIG-074): "<n> of <m> artifacts in this repository have no verified signature that this
 * setting accepts and will be refused. <k> are still being checked."
 */
export function previewQuestion(p: SignatureGatePreview): string {
  const refused = n(previewRefused(p));
  const of = n(p.artifacts);
  const head = p.complete
    ? $localize`:@@sig.gate.ask:${refused}:refused: of ${of}:artifacts: artifacts in this repository have no verified signature that this setting accepts and will be refused.`
    : $localize`:@@sig.gate.ask-partial:At least ${refused}:refused: of the first ${of}:artifacts: artifacts read in this repository have no verified signature that this setting accepts and will be refused. The preview stopped before the end.`;
  if (p.pending === 0) return head;
  const pending = n(p.pending);
  return `${head} ${$localize`:@@sig.gate.state-pending:${pending}:pending: are still being checked.`}`;
}

/** The refused counts per reason, one line each: "1,180 · No publisher's signature". */
export function previewReasonLines(p: SignatureGatePreview): readonly string[] {
  return previewRefusals(p).map((x) => `${n(x.count)} · ${gateReasonWord(x.reason)}`);
}

/**
 * The question before a change of anchors, blocks or the switch is saved (R-SIG-071, R-SIG-074): "This change makes 132 artifacts in 3
 * repositories that serve only verified artifacts refused:", and the repositories with their counts.
 */
export function impactQuestion(i: SignatureImpact): string {
  const artifacts = n(i.artifacts);
  const repositories = n(i.repositories);
  if (i.bound === 'upper') {
    return $localize`:@@sig.impact.upper:This change makes at most ${artifacts}:artifacts: artifacts in ${repositories}:repositories: repositories that serve only verified artifacts refused:`;
  }
  return i.complete
    ? $localize`:@@sig.impact.exact:This change makes ${artifacts}:artifacts: artifacts in ${repositories}:repositories: repositories that serve only verified artifacts refused:`
    : $localize`:@@sig.impact.partial:This change makes at least ${artifacts}:artifacts: artifacts in ${repositories}:repositories: repositories that serve only verified artifacts refused:`;
}

/** The repositories of an impact, "web/docker-local (120)", with "and N more" where the answer names 20 of more. */
export function impactItems(i: SignatureImpact): readonly string[] {
  const out = i.byRepository.map((b) => `${b.repository} (${n(b.artifacts)})`);
  const more = i.repositories - i.byRepository.length;
  if (more > 0) out.push($localize`:@@sig.impact.more:and ${n(more)}:count: more repositories`);
  return out;
}
