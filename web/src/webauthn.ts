// Minimal WebAuthn glue. The server sends standard PublicKeyCredential
// options with base64url fields; the browser API wants ArrayBuffers, and the
// response has to go back the same way.

function b64urlToBytes(value: string): ArrayBuffer {
  const padded = value.replace(/-/g, "+").replace(/_/g, "/") + "=".repeat((4 - (value.length % 4)) % 4);
  const raw = atob(padded);
  const out = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
  return out.buffer;
}

function bytesToB64url(buf: ArrayBuffer): string {
  const bytes = new Uint8Array(buf);
  let raw = "";
  for (const b of bytes) raw += String.fromCharCode(b);
  return btoa(raw).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

type Json = Record<string, unknown>;

export function supportsPasskeys() {
  return typeof window !== "undefined" && !!window.PublicKeyCredential && !!navigator.credentials;
}

export async function createPasskey(optionsJson: Json) {
  const pk = optionsJson.publicKey as Json;
  const options: PublicKeyCredentialCreationOptions = {
    ...(pk as unknown as PublicKeyCredentialCreationOptions),
    challenge: b64urlToBytes(String(pk.challenge)),
    user: {
      ...(pk.user as PublicKeyCredentialUserEntity),
      id: b64urlToBytes(String((pk.user as Json).id)),
    },
  };
  const cred = (await navigator.credentials.create({ publicKey: options })) as PublicKeyCredential | null;
  if (!cred) throw new Error("cancelled");
  const response = cred.response as AuthenticatorAttestationResponse;
  return {
    id: cred.id,
    rawId: bytesToB64url(cred.rawId),
    type: cred.type,
    response: {
      attestationObject: bytesToB64url(response.attestationObject),
      clientDataJSON: bytesToB64url(response.clientDataJSON),
      transports: response.getTransports?.() ?? [],
    },
    clientExtensionResults: cred.getClientExtensionResults(),
  };
}

export async function getPasskey(optionsJson: Json) {
  const pk = optionsJson.publicKey as Json;
  const options: PublicKeyCredentialRequestOptions = {
    ...(pk as unknown as PublicKeyCredentialRequestOptions),
    challenge: b64urlToBytes(String(pk.challenge)),
    ...(pk.allowCredentials
      ? {
          allowCredentials: (pk.allowCredentials as Json[]).map((c) => ({
            ...(c as unknown as PublicKeyCredentialDescriptor),
            id: b64urlToBytes(String(c.id)),
          })),
        }
      : {}),
  };
  const cred = (await navigator.credentials.get({ publicKey: options })) as PublicKeyCredential | null;
  if (!cred) throw new Error("cancelled");
  const response = cred.response as AuthenticatorAssertionResponse;
  return {
    id: cred.id,
    rawId: bytesToB64url(cred.rawId),
    type: cred.type,
    response: {
      authenticatorData: bytesToB64url(response.authenticatorData),
      clientDataJSON: bytesToB64url(response.clientDataJSON),
      signature: bytesToB64url(response.signature),
      userHandle: response.userHandle ? bytesToB64url(response.userHandle) : null,
    },
    clientExtensionResults: cred.getClientExtensionResults(),
  };
}

/** A short, friendly label for a new passkey, based on the browser platform. */
export function defaultPasskeyName() {
  const ua = navigator.userAgent;
  if (/iPhone|iPad/.test(ua)) return "iCloud 钥匙串";
  if (/Android/.test(ua)) return "Android 通行密钥";
  if (/Windows/.test(ua)) return "Windows Hello";
  if (/Mac OS/.test(ua)) return "Mac 通行密钥";
  return "通行密钥";
}
