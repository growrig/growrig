<script lang="ts">
	import { onMount } from 'svelte';
	import { live } from '$lib/live.svelte';
	import { getInfo, CORE_URL, wsURL } from '$lib/api';
	import StatTile from '$lib/components/StatTile.svelte';
	import { Button } from '$lib/components/ui';
	import { fmtDateTime } from '$lib/datetime';

	let adapter = $state<string>('…');
	let infoError = $state<string | null>(null);

	// A ticking clock so the "age" readouts stay live.
	let now = $state(Date.now());

	async function loadInfo() {
		try {
			adapter = (await getInfo()).adapter;
			infoError = null;
		} catch (e) {
			infoError = e instanceof Error ? e.message : String(e);
		}
	}

	onMount(() => {
		loadInfo();
		const t = setInterval(() => (now = Date.now()), 1000);
		return () => clearInterval(t);
	});

	const snap = $derived(live.snapshot);
	const envs = $derived(snap?.environments ?? []);

	const counts = $derived({
		environments: envs.length,
		controls: envs.reduce((n, e) => n + (e.controls?.length ?? 0), 0),
		sensors: envs.reduce((n, e) => n + (e.sensors?.length ?? 0), 0),
		cameras: envs.reduce((n, e) => n + (e.cameras?.length ?? 0), 0)
	});

	const ageMs = $derived(live.lastMessageAt ? now - live.lastMessageAt : null);

	function fmtAge(ms: number | null): string {
		if (ms === null) return 'never';
		if (ms < 1000) return 'just now';
		const s = Math.floor(ms / 1000);
		if (s < 60) return `${s}s ago`;
		return `${Math.floor(s / 60)}m ${s % 60}s ago`;
	}

	const statusMeta = {
		live: { label: 'Live', dot: 'bg-leaf', tone: 'good' as const },
		connecting: { label: 'Connecting', dot: 'bg-warn animate-pulse', tone: 'warn' as const },
		offline: { label: 'Offline', dot: 'bg-danger', tone: 'danger' as const }
	};

	const snapshotJson = $derived(snap ? JSON.stringify(snap, null, 2) : '// no snapshot yet');

	let copied = $state(false);
	async function copyJson() {
		try {
			await navigator.clipboard.writeText(snapshotJson);
			copied = true;
			setTimeout(() => (copied = false), 1500);
		} catch {
			/* clipboard unavailable */
		}
	}

	const rows = $derived([
		{ k: 'Connection', v: statusMeta[live.status].label },
		{ k: 'Last snapshot', v: `${fmtAge(ageMs)} · via ${live.lastSource ?? '—'}` },
		{ k: 'Snapshot time', v: snap?.time ? fmtDateTime(snap.time) : '—' },
		{ k: 'Adapter', v: adapter },
		{ k: 'Core URL', v: CORE_URL || `${'same-origin'} (${location.origin})` },
		{ k: 'WebSocket URL', v: wsURL() },
		{ k: 'Last error', v: live.lastError ?? infoError ?? 'none' }
	]);
</script>

<div class="space-y-6">
	<div class="flex items-center justify-between">
		<div class="flex items-center gap-3">
			<h2 class="text-lg font-semibold">Debug</h2>
			<span class="flex items-center gap-2 rounded-full bg-rig-800 px-3 py-1 text-xs text-rig-300">
				<span class="h-2 w-2 rounded-full {statusMeta[live.status].dot}"></span>
				{statusMeta[live.status].label}
			</span>
		</div>
		<Button variant="secondary" size="sm" onclick={loadInfo}>Refresh info</Button>
	</div>

	<section class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
		<StatTile label="Environments" value={String(counts.environments)} tone="good" />
		<StatTile label="Controls" value={String(counts.controls)} />
		<StatTile label="Sensors" value={String(counts.sensors)} />
		<StatTile label="Cameras" value={String(counts.cameras)} />
	</section>

	<section class="overflow-hidden rounded-xl border border-rig-800 bg-rig-900/40">
		<h3 class="border-b border-rig-800 px-5 py-3 text-sm font-semibold uppercase tracking-wide text-rig-400">
			Runtime
		</h3>
		<dl class="divide-y divide-rig-800/70">
			{#each rows as row (row.k)}
				<div class="flex items-center justify-between gap-4 px-5 py-2.5 text-sm">
					<dt class="text-rig-400">{row.k}</dt>
					<dd class="truncate text-right font-mono text-xs text-rig-100">{row.v}</dd>
				</div>
			{/each}
		</dl>
	</section>

	<section class="overflow-hidden rounded-xl border border-rig-800 bg-rig-900/40">
		<div class="flex items-center justify-between border-b border-rig-800 px-5 py-3">
			<h3 class="text-sm font-semibold uppercase tracking-wide text-rig-400">Live snapshot</h3>
			<Button variant="ghost" size="sm" onclick={copyJson}>{copied ? 'Copied ✓' : 'Copy JSON'}</Button>
		</div>
		<pre class="max-h-[28rem] overflow-auto p-4 text-xs leading-relaxed text-rig-200"><code>{snapshotJson}</code></pre>
	</section>
</div>
