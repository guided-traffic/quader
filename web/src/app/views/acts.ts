import { InjectionToken, Signal, Type, WritableSignal } from '@angular/core';
import { ApiError } from '../api/problem';
import { SignatureGateRefusedReason } from '../api/signatures-api';

/*
 * The places where a view offers an act of the main installation (R-UI-223). A view never acts: it reads through the abstract reads of
 * api/ and, at each place below, draws the component that the page composing it provides through the place's token (a page's
 * `providers`), while the slot says it is offered. The main installation's pages provide their acts (a dialog, a write, a check of the
 * session's permissions); an application without acts, the Bastion's web interface, provides none, and every place stays empty. A place
 * at the head of a page (a version's deprecation) is content the page projects into the view instead; the view names those slots itself.
 */

/** What a page provides at a place: the component that acts there, and whether it is offered now (a reader who may not act sees none). */
export interface ActSlot {
  readonly component: Type<unknown>;
  readonly offered: Signal<boolean>;
}

/** The artifact an act of a signature gate is about (R-SIG-070): its name as a reader names it, its digest and why the gate refuses it. */
export interface GateActArtifact {
  readonly name: string;
  readonly sha256: string;
  readonly reason: SignatureGateRefusedReason;
}

/** What a gate's acts share with the gate that shows them: whether one runs, and its refusal, which the gate shows above its decision. */
export interface GateActState {
  readonly busy: WritableSignal<boolean>;
  readonly error: WritableSignal<ApiError | null>;
}

/**
 * An act of a repository's signature gate (R-SIG-070, R-SIG-074): **Release for this repository** under a refused artifact, **Withdraw
 * release** under a released one, in the gate's box of a version's tab Signatures or of a file's page (`place: 'gate'`) and in the banner
 * of a refused artifact above a version's tabs (`place: 'banner'`). Offered to the holder of the triage right. The component receives the
 * inputs of {@link GateActInputs}.
 */
export const GATE_ACT = new InjectionToken<ActSlot>('GATE_ACT');

/**
 * The inputs of the component at {@link GATE_ACT}; `changed` is called after a release was granted or withdrawn. A record: the component
 * outlet takes its inputs as one.
 */
export interface GateActInputs extends Readonly<Record<string, unknown>> {
  readonly act: 'release' | 'withdraw';
  readonly place: 'gate' | 'banner';
  readonly repository: string;
  readonly artifact: GateActArtifact;
  /** The gate's state, null in a banner. */
  readonly state: GateActState | null;
  readonly changed: () => void;
}

/**
 * **Upload collection** under a role's README note (R-ANS-061, Q-UI-1323): the component receives `project`, the project of the page, and
 * draws nothing for a reader who may publish into none of its Ansible Galaxy repositories.
 */
export const ROLE_UPLOAD_ACT = new InjectionToken<ActSlot>('ROLE_UPLOAD_ACT');

/**
 * The tab **Import log** of an Ansible collection's version (R-UI-063): the tasks of its local repository, which only the main
 * installation keeps. The component receives `repository`, `collection` and `version`; without the slot the version has no such tab.
 */
export const VERSION_IMPORT_LOG = new InjectionToken<ActSlot>('VERSION_IMPORT_LOG');
