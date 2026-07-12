// Browser-side WebAuthn (passkey) ceremonies.
//
// Grow Core speaks the standard WebAuthn JSON wire format, where binary fields
// (challenge, credential ids, attestation/assertion buffers) are base64url
// strings. The browser's credential API works in ArrayBuffers, so this module
// converts between the two around navigator.credentials.create/get.
import {
	passkeyRegisterBegin,
	passkeyRegisterFinish,
	passkeyLoginBegin,
	passkeyLoginFinish,
	type Passkey
} from './api';
import type { AuthResult } from './types';

/** True when this browser exposes the WebAuthn platform API. */
export function passkeysSupported(): boolean {
	return typeof window !== 'undefined' && !!window.PublicKeyCredential;
}

function b64urlToBuf(value: string): ArrayBuffer {
	const pad = value.length % 4 === 0 ? '' : '='.repeat(4 - (value.length % 4));
	const base64 = value.replace(/-/g, '+').replace(/_/g, '/') + pad;
	const bin = atob(base64);
	const buf = new ArrayBuffer(bin.length);
	const view = new Uint8Array(buf);
	for (let i = 0; i < bin.length; i++) view[i] = bin.charCodeAt(i);
	return buf;
}

function bytesToB64url(buf: ArrayBuffer): string {
	const bytes = new Uint8Array(buf);
	let bin = '';
	for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i]);
	return btoa(bin).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

interface CredentialDescriptor {
	id: string;
	type: string;
	transports?: AuthenticatorTransport[];
}

// Decode the base64url fields the server sends inside the `publicKey` options
// into the BufferSources the browser API requires.
function decodeCreateOptions(pk: Record<string, unknown>): PublicKeyCredentialCreationOptions {
	const options = { ...pk } as unknown as PublicKeyCredentialCreationOptions;
	options.challenge = b64urlToBuf(pk.challenge as string);
	const user = pk.user as { id: string; name: string; displayName: string };
	options.user = { ...user, id: b64urlToBuf(user.id) };
	if (Array.isArray(pk.excludeCredentials)) {
		options.excludeCredentials = (pk.excludeCredentials as CredentialDescriptor[]).map((c) => ({
			...c,
			id: b64urlToBuf(c.id)
		})) as PublicKeyCredentialDescriptor[];
	}
	return options;
}

function decodeRequestOptions(pk: Record<string, unknown>): PublicKeyCredentialRequestOptions {
	const options = { ...pk } as unknown as PublicKeyCredentialRequestOptions;
	options.challenge = b64urlToBuf(pk.challenge as string);
	if (Array.isArray(pk.allowCredentials)) {
		options.allowCredentials = (pk.allowCredentials as CredentialDescriptor[]).map((c) => ({
			...c,
			id: b64urlToBuf(c.id)
		})) as PublicKeyCredentialDescriptor[];
	}
	return options;
}

/** Register a new passkey for the signed-in user; returns the stored summary. */
export async function registerPasskey(name: string): Promise<Passkey> {
	const { publicKey, handle } = await passkeyRegisterBegin();
	const credential = (await navigator.credentials.create({
		publicKey: decodeCreateOptions(publicKey)
	})) as PublicKeyCredential | null;
	if (!credential) throw new Error('Passkey registration was cancelled');
	const response = credential.response as AuthenticatorAttestationResponse;
	const body = {
		id: credential.id,
		rawId: bytesToB64url(credential.rawId),
		type: credential.type,
		response: {
			attestationObject: bytesToB64url(response.attestationObject),
			clientDataJSON: bytesToB64url(response.clientDataJSON),
			transports:
				typeof response.getTransports === 'function' ? response.getTransports() : undefined
		},
		clientExtensionResults: credential.getClientExtensionResults()
	};
	return passkeyRegisterFinish(handle, name, body);
}

/** Sign in with a discoverable passkey; returns the session token + user. */
export async function loginWithPasskey(): Promise<AuthResult> {
	const { publicKey, handle } = await passkeyLoginBegin();
	const credential = (await navigator.credentials.get({
		publicKey: decodeRequestOptions(publicKey)
	})) as PublicKeyCredential | null;
	if (!credential) throw new Error('Passkey sign-in was cancelled');
	const response = credential.response as AuthenticatorAssertionResponse;
	const body = {
		id: credential.id,
		rawId: bytesToB64url(credential.rawId),
		type: credential.type,
		response: {
			authenticatorData: bytesToB64url(response.authenticatorData),
			clientDataJSON: bytesToB64url(response.clientDataJSON),
			signature: bytesToB64url(response.signature),
			userHandle: response.userHandle ? bytesToB64url(response.userHandle) : null
		},
		clientExtensionResults: credential.getClientExtensionResults()
	};
	return passkeyLoginFinish(handle, body);
}
