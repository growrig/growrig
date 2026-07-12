// Authentication state, exposed as runes-based state.
//
// The bearer token is persisted to localStorage so a reload stays signed in.
// On init we confirm the token with the server (GET /api/auth/me) and learn
// whether first-run setup is required. The API client (api.ts) reads the token
// for REST + WebSocket auth and calls back here on a 401 so an expired session
// routes the user to /login.
import {
	getAuthStatus,
	getMe,
	login as apiLogin,
	bootstrap as apiBootstrap,
	register as apiRegister,
	logout as apiLogout,
	setAuthToken,
	setUnauthorizedHandler
} from './api';
import type { User } from './types';

const TOKEN_KEY = 'growrig.token';

export type AuthPhase = 'loading' | 'needs-setup' | 'anonymous' | 'authed';

class AuthState {
	phase = $state<AuthPhase>('loading');
	user = $state<User | null>(null);
	signupEnabled = $state(false);

	#token: string | null = null;

	get isAdmin(): boolean {
		return this.user?.role === 'admin';
	}

	/** Highest access level the current user has on an environment. */
	#access(envId: string): 'read' | 'write' | null {
		if (!this.user) return null;
		if (this.user.role === 'admin') return 'write';
		const grant = this.user.access?.find((a) => a.environmentId === envId);
		return grant ? grant.access : null;
	}
	canRead(envId: string): boolean {
		return this.#access(envId) !== null;
	}
	canWrite(envId: string): boolean {
		return this.#access(envId) === 'write';
	}

	/** Resolve the session on app start; call once before starting the live feed. */
	async init() {
		setUnauthorizedHandler(() => this.#clear('anonymous'));
		this.#token = localStorage.getItem(TOKEN_KEY);
		setAuthToken(this.#token);

		let status;
		try {
			status = await getAuthStatus();
		} catch {
			// Server unreachable — treat as anonymous; the live feed's own retry
			// loop will recover once it's back.
			this.phase = 'anonymous';
			return;
		}
		this.signupEnabled = status.signupEnabled;
		if (status.needsSetup) {
			this.phase = 'needs-setup';
			return;
		}
		if (!this.#token) {
			this.phase = 'anonymous';
			return;
		}
		try {
			this.user = await getMe();
			this.phase = 'authed';
		} catch {
			this.#clear('anonymous');
		}
	}

	async login(username: string, password: string) {
		this.#apply(await apiLogin(username, password));
	}
	async loginWithPasskey() {
		const { loginWithPasskey } = await import('./webauthn');
		this.#apply(await loginWithPasskey());
	}
	async bootstrap(username: string, password: string) {
		this.#apply(await apiBootstrap(username, password));
	}
	async register(username: string, password: string) {
		this.#apply(await apiRegister(username, password));
	}

	async logout() {
		try {
			await apiLogout();
		} catch {
			/* best effort; clear locally regardless */
		}
		this.#clear('anonymous');
	}

	/** Re-fetch the current user (e.g. after an admin changes own access). */
	async refresh() {
		if (this.phase !== 'authed') return;
		try {
			this.user = await getMe();
		} catch {
			/* ignore; a 401 will route to login via the handler */
		}
	}

	#apply(result: { token: string; user: User }) {
		this.#token = result.token;
		localStorage.setItem(TOKEN_KEY, result.token);
		setAuthToken(result.token);
		this.user = result.user;
		this.phase = 'authed';
	}

	#clear(phase: AuthPhase) {
		this.#token = null;
		localStorage.removeItem(TOKEN_KEY);
		setAuthToken(null);
		this.user = null;
		this.phase = phase;
	}
}

export const auth = new AuthState();
