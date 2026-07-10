// Live connection to Grow Core over WebSocket, exposed as runes-based state.
// Auto-reconnects and keeps the most recent snapshot.
import { wsURL } from './api';
import type { Snapshot } from './types';

export type ConnStatus = 'connecting' | 'live' | 'offline';

class LiveState {
	snapshot = $state<Snapshot | null>(null);
	status = $state<ConnStatus>('connecting');

	#ws: WebSocket | null = null;
	#retry = 0;
	#timer: ReturnType<typeof setTimeout> | null = null;
	#stopped = false;

	start() {
		this.#stopped = false;
		this.#connect();
	}

	stop() {
		this.#stopped = true;
		if (this.#timer) clearTimeout(this.#timer);
		this.#ws?.close();
		this.#ws = null;
	}

	#connect() {
		if (this.#stopped) return;
		this.status = this.snapshot ? this.status : 'connecting';
		const ws = new WebSocket(wsURL());
		this.#ws = ws;

		ws.onopen = () => {
			this.#retry = 0;
			this.status = 'live';
		};
		ws.onmessage = (ev) => {
			try {
				this.snapshot = JSON.parse(ev.data) as Snapshot;
				this.status = 'live';
			} catch {
				/* ignore malformed frame */
			}
		};
		ws.onclose = () => {
			this.#ws = null;
			this.status = 'offline';
			this.#scheduleReconnect();
		};
		ws.onerror = () => ws.close();
	}

	#scheduleReconnect() {
		if (this.#stopped) return;
		const delay = Math.min(1000 * 2 ** this.#retry, 8000);
		this.#retry++;
		this.#timer = setTimeout(() => this.#connect(), delay);
	}
}

export const live = new LiveState();
