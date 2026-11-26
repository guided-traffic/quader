import { Bastion } from '../../api/bastions-api';

/*
 * The addresses a Bastion connects from (bastion.md R-BAS-017, api-and-ui.md R-UI-243): the card compares the address of each replica's
 * last contact with the registration's networks, to warn where one lies outside them and to offer it as a one-click entry
 * (Q-UI-1482). The server validates every list (Q-BAS-431); this reading only has to tell inside from outside.
 */

/** An address as a number of its family's width. */
interface Address {
  readonly v6: boolean;
  readonly n: bigint;
}

/** A network: an address and the length of its prefix. */
interface Network extends Address {
  readonly bits: number;
}

function parseV4(s: string): bigint | null {
  const parts = s.split('.');
  if (parts.length !== 4) return null;
  let n = 0n;
  for (const p of parts) {
    if (!/^\d{1,3}$/.test(p) || Number(p) > 255) return null;
    n = (n << 8n) | BigInt(Number(p));
  }
  return n;
}

function parseV6(s: string): bigint | null {
  let text = s;
  let tail: bigint | null = null;
  const lastColon = text.lastIndexOf(':');
  if (text.includes('.', lastColon)) {
    tail = parseV4(text.slice(lastColon + 1));
    if (tail === null) return null;
    text = `${text.slice(0, lastColon + 1)}0:0`;
  }
  const halves = text.split('::');
  if (halves.length > 2) return null;
  const words = (h: string): string[] => (h === '' ? [] : h.split(':'));
  const head = words(halves[0] ?? '');
  const rest = halves.length === 2 ? words(halves[1] ?? '') : [];
  const missing = 8 - head.length - rest.length;
  if ((halves.length === 1 && missing !== 0) || missing < 0 || (halves.length === 2 && missing < 1)) return null;
  const all = [...head, ...Array<string>(halves.length === 2 ? missing : 0).fill('0'), ...rest];
  let n = 0n;
  for (const w of all) {
    if (!/^[0-9a-fA-F]{1,4}$/.test(w)) return null;
    n = (n << 16n) | BigInt(parseInt(w, 16));
  }
  if (tail !== null) n = (n & ~0xffffffffn) | tail;
  return n;
}

/** Reads an IPv4 or IPv6 address; an IPv4 address in IPv6 form is the IPv4 one, a zone is dropped. Null for anything else. */
export function parseAddress(text: string): Address | null {
  const s = text.trim().replace(/%.*$/, '');
  const v4 = parseV4(s);
  if (v4 !== null) return { v6: false, n: v4 };
  const v6 = s.includes(':') ? parseV6(s) : null;
  if (v6 === null) return null;
  if (v6 >> 32n === 0xffffn) return { v6: false, n: v6 & 0xffffffffn };
  return { v6: true, n: v6 };
}

/** Reads a network in CIDR form or an address alone (its own network). Null for anything else. */
export function parseNetwork(text: string): Network | null {
  const [addr, len, more] = text.trim().split('/');
  if (more !== undefined) return null;
  const a = parseAddress(addr ?? '');
  if (!a) return null;
  const width = a.v6 ? 128 : 32;
  if (len === undefined) return { ...a, bits: width };
  if (!/^\d{1,3}$/.test(len) || Number(len) > width) return null;
  return { ...a, bits: Number(len) };
}

/** Whether the address lies in one of the networks. */
export function inNetworks(address: string, networks: readonly string[]): boolean {
  const a = parseAddress(address);
  if (!a) return false;
  return networks.some((text) => {
    const p = parseNetwork(text);
    if (!p || p.v6 !== a.v6) return false;
    const shift = BigInt((a.v6 ? 128 : 32) - p.bits);
    return a.n >> shift === p.n >> shift;
  });
}

/**
 * The addresses the replicas' last contacts came from that the registration does not list, each once, the most recent contact first
 * (the contacts' order): the card warns with each and offers it (Q-UI-1482).
 */
export function unlistedContacts(bastion: Pick<Bastion, 'contacts' | 'egress'>): string[] {
  const out: string[] = [];
  for (const c of bastion.contacts) {
    const addr = c.clientAddress;
    if (addr && !out.includes(addr) && parseAddress(addr) && !inNetworks(addr, bastion.egress)) out.push(addr);
  }
  return out;
}
