// Live connection to Grow Core, exposed as runes-based state.
//
// Data flow: the initial snapshot is fetched over REST (`GET /api/state`) so
// the UI paints immediately, even if the WebSocket is slow to connect or
// unavailable. The WebSocket is then only responsible for *live updates* — it
// pushes a fresh snapshot on every reconciliation tick. Auto-reconnects with
// backoff and keeps the most recent snapshot across drops.
import { getState, wsURL } from './api';
import type { Snapshot } from './types';

export type ConnStatus = 'connecting' | 'live' | 'offline';

class LiveState {
	snapshot = $state<Snapshot | null>(null);
	status = $state<ConnStatus>('connecting');
	/** Epoch ms of the last snapshot applied (REST or WS), for diagnostics. */
	lastMessageAt = $state<number | null>(null);
	/** How the most recent snapshot arrived. */
	lastSource = $state<'rest' | 'ws' | null>(null);
	/** Last connection error text, surfaced on the debug page. */
	lastError = $state<string | null>(null);
	/** Application-level round-trip time over the active WebSocket. */
	latencyMs = $state<number | null>(null);

	#ws: WebSocket | null = null;
	#retry = 0;
	#timer: ReturnType<typeof setTimeout> | null = null;
	#pingTimer: ReturnType<typeof setInterval> | null = null;
	#stopped = false;

	start() {
		this.#stopped = false;
		// Kick off the REST prime and the live socket in parallel; whichever
		// arrives first paints, and WS frames win thereafter.
		void this.#prime();
		this.#connect();
	}

	stop() {
		this.#stopped = true;
		if (this.#timer) clearTimeout(this.#timer);
		if (this.#pingTimer) clearInterval(this.#pingTimer);
		this.#ws?.close();
		this.#ws = null;
		// Drop cached state so a signed-out (or switched) user never sees the
		// previous session's environments.
		this.snapshot = null;
		this.status = 'connecting';
		this.latencyMs = null;
		this.lastSource = null;
	}

	/** One-shot REST fetch for the initial snapshot. */
	async #prime() {
		try {
			const snap = await getState();
			// Don't clobber a fresher WS frame that may have already landed.
			if (this.#stopped || this.lastSource === 'ws') return;
			this.#apply(snap, 'rest');
		} catch (err) {
			this.lastError = err instanceof Error ? err.message : String(err);
		}
	}

	#apply(snap: Snapshot, source: 'rest' | 'ws') {
		this.snapshot = snap;
		this.lastMessageAt = Date.now();
		this.lastSource = source;
	}

	#connect() {
		if (this.#stopped) return;
		this.status = this.snapshot ? this.status : 'connecting';
		const ws = new WebSocket(wsURL());
		this.#ws = ws;

		ws.onopen = () => {
			this.#retry = 0;
			this.status = 'live';
			this.lastError = null;
			this.#sendPing();
			if (this.#pingTimer) clearInterval(this.#pingTimer);
			this.#pingTimer = setInterval(() => this.#sendPing(), 5000);
		};
		ws.onmessage = (ev) => {
			try {
				const message = JSON.parse(ev.data);
				if (message?.type === 'pong' && typeof message.id === 'number') {
					this.latencyMs = Math.max(0, Date.now() - message.id);
					return;
				}
				this.#apply(message as Snapshot, 'ws');
				this.status = 'live';
			} catch {
				/* ignore malformed frame */
			}
		};
		ws.onclose = () => {
			if (this.#pingTimer) clearInterval(this.#pingTimer);
			this.#pingTimer = null;
			this.#ws = null;
			this.status = 'offline';
			this.latencyMs = null;
			this.#scheduleReconnect();
		};
		ws.onerror = () => ws.close();
	}

	#sendPing() {
		if (this.#ws?.readyState === WebSocket.OPEN) this.#ws.send(JSON.stringify({ type: 'ping', id: Date.now() }));
	}

	#scheduleReconnect() {
		if (this.#stopped) return;
		const delay = Math.min(1000 * 2 ** this.#retry, 8000);
		this.#retry++;
		this.#timer = setTimeout(() => this.#connect(), delay);
	}
}

export const live = new LiveState();
