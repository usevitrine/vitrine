// Passkeys in the browser (2026-10-31; the server: internal/auth/passkeys.go). The server's options come as JSON with
// binary values in base64url; the browser's API wants them as bytes, and its answer goes back the same way.
import { api, type Session } from './api';

/** Whether this browser can use passkeys here (a secure page: HTTPS, or localhost). */
export const passkeysWork = () => typeof window !== 'undefined' && window.isSecureContext && 'PublicKeyCredential' in window;

const bytes = (s: string) => {
  const b = atob(s.replace(/-/g, '+').replace(/_/g, '/') + '='.repeat((4 - (s.length % 4)) % 4));
  return Uint8Array.from(b, (c) => c.charCodeAt(0));
};
const text = (buf: ArrayBuffer | null | undefined) => {
  if (!buf) return undefined;
  let s = '';
  for (const b of new Uint8Array(buf)) s += String.fromCharCode(b);
  return btoa(s).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
};

type Desc = { id: string; type: string; transports?: string[] };
const descriptors = (list?: Desc[]) => list?.map((d) => ({ ...d, id: bytes(d.id) })) as PublicKeyCredentialDescriptor[] | undefined;

/** The browser's "Cancel" (or a timeout): no error to show. */
export const cancelled = (e: unknown) => e instanceof DOMException && (e.name === 'NotAllowedError' || e.name === 'AbortError');

/** Signs in with a passkey (the browser asks which one); the session as a password login gives it. */
export async function signInWithPasskey(): Promise<Session> {
  const { publicKey: o } = await api.passkeyLoginBegin();
  const cred = (await navigator.credentials.get({
    publicKey: { ...o, challenge: bytes(o.challenge), allowCredentials: descriptors(o.allowCredentials) } as unknown as PublicKeyCredentialRequestOptions,
  })) as PublicKeyCredential | null;
  if (!cred) throw new DOMException('no passkey', 'NotAllowedError');
  const r = cred.response as AuthenticatorAssertionResponse;
  return api.passkeyLoginFinish({
    id: cred.id,
    rawId: text(cred.rawId),
    type: cred.type,
    authenticatorAttachment: cred.authenticatorAttachment ?? undefined,
    clientExtensionResults: cred.getClientExtensionResults(),
    response: {
      clientDataJSON: text(r.clientDataJSON),
      authenticatorData: text(r.authenticatorData),
      signature: text(r.signature),
      userHandle: text(r.userHandle),
    },
  });
}

/** Makes a passkey for the account on this device (or a phone, a key: the browser asks) and keeps it under a name. */
export async function addPasskey(name: string) {
  const { publicKey: o } = await api.passkeyBegin();
  const cred = (await navigator.credentials.create({
    publicKey: {
      ...o,
      challenge: bytes(o.challenge),
      user: { ...o.user, id: bytes(o.user.id) },
      excludeCredentials: descriptors(o.excludeCredentials),
    } as unknown as PublicKeyCredentialCreationOptions, // (rp, pubKeyCredParams...: as the server sent them)
  })) as PublicKeyCredential | null;
  if (!cred) throw new DOMException('no passkey', 'NotAllowedError');
  const r = cred.response as AuthenticatorAttestationResponse;
  return api.passkeyFinish(name, {
    id: cred.id,
    rawId: text(cred.rawId),
    type: cred.type,
    authenticatorAttachment: cred.authenticatorAttachment ?? undefined,
    clientExtensionResults: cred.getClientExtensionResults(),
    response: {
      clientDataJSON: text(r.clientDataJSON),
      attestationObject: text(r.attestationObject),
      transports: typeof r.getTransports === 'function' ? r.getTransports() : undefined,
    },
  });
}

/** A name for a new passkey: the device it's made on, to start with. */
export function deviceName(): string {
  const ua = navigator.userAgent;
  if (/iPhone/.test(ua)) return 'iPhone';
  if (/iPad/.test(ua) || (/Macintosh/.test(ua) && navigator.maxTouchPoints > 1)) return 'iPad';
  if (/Android/.test(ua)) return 'Android';
  if (/Macintosh/.test(ua)) return 'Mac';
  if (/Windows/.test(ua)) return 'Windows';
  if (/Linux/.test(ua)) return 'Linux';
  return 'Passkey';
}
