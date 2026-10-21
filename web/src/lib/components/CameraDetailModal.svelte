<script lang="ts">
	import { getCameraSnapshots, cameraArchiveURL, cameraProxyURL, type CameraSnapshot } from '$lib/api';
	import type { CameraRef } from '$lib/types';
	import { Dialog } from '$lib/components/ui';
	import CameraPreview from './CameraPreview.svelte';
	import Radio from '@lucide/svelte/icons/radio';
	import { fmtSnapshotTime, fmtTimelineDay, fmtTimelineHour } from '$lib/datetime';

	interface Props { open?: boolean; camera: CameraRef; }
	let { open = $bindable(false), camera }: Props = $props();

	const HOUR = 3_600_000;
	const ranges = [
		{ hours: 6, label: '6h', tickMs: HOUR, tickLabel: 'hour' },
		{ hours: 24, label: '24h', tickMs: 3 * HOUR, tickLabel: 'hour' },
		{ hours: 72, label: '3d', tickMs: 12 * HOUR, tickLabel: 'hour' },
		{ hours: 168, label: '7d', tickMs: 24 * HOUR, tickLabel: 'day' },
		{ hours: 720, label: '30d', tickMs: 5 * 24 * HOUR, tickLabel: 'day' }
	] as const;
	// One mark per ~2 CSS pixels of a wide dialog is plenty; the server buckets
	// the archive down to this many representative snapshots.
	const MARK_LIMIT = 600;

	let rangeHours = $state(24);
	/** Snapshots inside the fetched window, oldest first. */
	let snapshots = $state<CameraSnapshot[]>([]);
	let times = $state<number[]>([]);
	let total = $state(0);
	let loading = $state(false);
	/** Window actually plotted, frozen at fetch time so marks stay aligned. */
	let windowStart = $state(0);
	let windowEnd = $state(0);
	let hovered = $state<CameraSnapshot | null>(null);
	let pinned = $state<CameraSnapshot | null>(null);
	let cursorRatio = $state<number | null>(null);
	let track: HTMLElement | null = null;

	const range = $derived(ranges.find((r) => r.hours === rangeHours) ?? ranges[1]);
	const canUseRecordedView = $derived(camera.cameraType === 'rtsp' || (!camera.streamUrl && !camera.entity));
	const active = $derived(hovered ?? pinned);
	const activeURL = $derived(active ? cameraArchiveURL(camera.id, active.id) : cameraProxyURL(camera.id));

	$effect(() => {
		if (!open || !canUseRecordedView) return;
		void camera.id;
		const hours = rangeHours;
		const until = new Date();
		const since = new Date(until.getTime() - hours * HOUR);
		hovered = null;
		pinned = null;
		cursorRatio = null;
		loading = true;
		getCameraSnapshots(camera.id, { since, until, limit: MARK_LIMIT })
			.then((page) => {
				// The API returns newest first; plotting reads left to right.
				snapshots = page.snapshots.slice().reverse();
				times = snapshots.map((s) => new Date(s.time).getTime());
				total = page.total;
				windowStart = since.getTime();
				windowEnd = until.getTime();
			})
			.catch(() => { snapshots = []; times = []; total = 0; })
			.finally(() => (loading = false));
	});

	const span = $derived(Math.max(1, windowEnd - windowStart));
	const ratioOf = (time: number) => (time - windowStart) / span;

	const ticks = $derived.by(() => {
		if (!windowEnd) return [];
		const step = range.tickMs;
		// Align to local wall-clock boundaries so labels read as round times.
		const first = new Date(windowStart);
		first.setMinutes(0, 0, 0);
		const out: { at: number; label: string }[] = [];
		for (let at = Math.ceil(first.getTime() / step) * step; at <= windowEnd; at += step) {
			if (at < windowStart) continue;
			out.push({ at, label: range.tickLabel === 'day' ? fmtTimelineDay(at) : fmtTimelineHour(at) });
		}
		return out;
	});

	/** Snapshot whose timestamp is closest to `time`. */
	function nearest(time: number): CameraSnapshot | null {
		if (!times.length) return null;
		let low = 0;
		let high = times.length - 1;
		while (low < high) {
			const mid = (low + high) >> 1;
			if (times[mid] < time) low = mid + 1;
			else high = mid;
		}
		const after = low;
		const before = low > 0 ? low - 1 : 0;
		return Math.abs(times[after] - time) < Math.abs(times[before] - time)
			? snapshots[after]
			: snapshots[before];
	}

	function scrub(event: PointerEvent) {
		if (!track) return;
		const rect = track.getBoundingClientRect();
		const ratio = Math.min(1, Math.max(0, (event.clientX - rect.left) / rect.width));
		cursorRatio = ratio;
		hovered = nearest(windowStart + ratio * span);
	}
	function leave() {
		hovered = null;
		cursorRatio = null;
	}
	function pin() {
		if (hovered) pinned = hovered;
	}
	function step(delta: number) {
		if (!snapshots.length) return;
		const current = active ? snapshots.indexOf(active) : snapshots.length - 1;
		const next = Math.min(snapshots.length - 1, Math.max(0, current + delta));
		pinned = snapshots[next];
		hovered = null;
		cursorRatio = ratioOf(times[next]);
	}
	function keydown(event: KeyboardEvent) {
		if (event.key === 'ArrowLeft') step(-1);
		else if (event.key === 'ArrowRight') step(1);
		else if (event.key === 'Home') step(-snapshots.length);
		else if (event.key === 'End') step(snapshots.length);
		else if (event.key === 'Escape') { pinned = null; hovered = null; cursorRatio = null; return; }
		else return;
		event.preventDefault();
	}

	const activeRatio = $derived(active ? ratioOf(new Date(active.time).getTime()) : null);
	// Keep the timestamp bubble inside the track at both ends.
	const bubbleShift = $derived(activeRatio === null ? 0 : activeRatio < 0.08 ? 0 : activeRatio > 0.92 ? -100 : -50);
</script>

<Dialog bind:open title={camera.name} description="Live camera and recorded snapshots" size="3xl">
	<div class="space-y-4">
		<div class="relative">
			<CameraPreview url={activeURL} liveUrl={!active && canUseRecordedView ? cameraProxyURL(camera.id, true) : ''} type={active ? 'mjpeg' : canUseRecordedView ? 'snapshot' : camera.cameraType} class="border-rig-700" emptyLabel="Connecting to camera…" errorLabel="Connecting to camera…" />
			{#if active}
				<span class="absolute left-3 top-3 rounded-md bg-rig-950/85 px-2.5 py-1 text-xs tabular-nums text-rig-100 shadow">{fmtSnapshotTime(active.time)}</span>
				<button type="button" onclick={() => { pinned = null; hovered = null; cursorRatio = null; }} class="absolute right-3 top-3 inline-flex items-center gap-1.5 rounded-md bg-rig-950/85 px-3 py-1.5 text-xs font-medium text-rig-100 shadow hover:bg-rig-800"><Radio size={14} class="text-leaf" /> Return to live</button>
			{:else if canUseRecordedView}
				<span class="absolute right-3 top-3 inline-flex items-center gap-1.5 rounded-md bg-rig-950/75 px-2.5 py-1 text-xs text-leaf"><span class="h-1.5 w-1.5 animate-pulse rounded-full bg-leaf"></span> Live</span>
			{/if}
		</div>

		<div>
			<div class="mb-2 flex items-center justify-between gap-3">
				<h3 class="text-sm font-semibold uppercase tracking-wide text-rig-400">Snapshot timeline</h3>
				<div class="flex items-center gap-3">
					<span class="text-xs tabular-nums text-rig-500">{total} snapshot{total === 1 ? '' : 's'}</span>
					<div class="flex rounded-lg border border-rig-800 p-0.5">
						{#each ranges as option (option.hours)}
							<button type="button" onclick={() => (rangeHours = option.hours)} class="rounded-md px-2.5 py-1 text-xs font-medium tabular-nums transition-colors {rangeHours === option.hours ? 'bg-rig-800 text-rig-50' : 'text-rig-400 hover:text-rig-100'}">{option.label}</button>
						{/each}
					</div>
				</div>
			</div>

			<div class="rounded-lg border border-rig-800 bg-rig-950/40 px-3 pb-1.5 pt-3">
				<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
				<div
					bind:this={track}
					role="slider"
					tabindex="0"
					aria-label="Snapshot timeline"
					aria-valuemin={0}
					aria-valuemax={Math.max(0, snapshots.length - 1)}
					aria-valuenow={active ? snapshots.indexOf(active) : 0}
					aria-valuetext={active ? fmtSnapshotTime(active.time) : 'Live'}
					onpointermove={scrub}
					onpointerleave={leave}
					onpointerdown={(event) => { scrub(event); pin(); }}
					onkeydown={keydown}
					class="relative h-16 w-full cursor-crosshair rounded-md bg-rig-900/60 outline-none ring-leaf/40 focus-visible:ring-2"
				>
					{#each ticks as tick (tick.at)}
						<span class="pointer-events-none absolute inset-y-0 w-px bg-rig-800" style="left:{ratioOf(tick.at) * 100}%"></span>
					{/each}

					{#if loading}
						<span class="pointer-events-none absolute inset-0 flex items-center justify-center text-xs text-rig-500">Loading snapshots…</span>
					{:else if !snapshots.length}
						<span class="pointer-events-none absolute inset-0 flex items-center justify-center text-xs text-rig-500">No snapshots in this range.</span>
					{:else}
						{#each snapshots as snapshot, index (snapshot.id)}
							<span class="pointer-events-none absolute inset-y-2 w-px bg-leaf/45" style="left:{ratioOf(times[index]) * 100}%"></span>
						{/each}
					{/if}

					{#if cursorRatio !== null}
						<span class="pointer-events-none absolute inset-y-0 w-px bg-rig-500/70" style="left:{cursorRatio * 100}%"></span>
					{/if}
					{#if activeRatio !== null}
						<span class="pointer-events-none absolute inset-y-0 w-0.5 -translate-x-px bg-leaf" style="left:{activeRatio * 100}%"></span>
						<span class="pointer-events-none absolute -top-0.5 whitespace-nowrap rounded bg-leaf px-1.5 py-0.5 text-[10px] font-medium tabular-nums text-rig-950" style="left:{activeRatio * 100}%;transform:translateX({bubbleShift}%)">{fmtSnapshotTime(active!.time)}</span>
					{/if}
				</div>

				<div class="relative mt-1 h-4">
					{#each ticks as tick (tick.at)}
						<span class="absolute -translate-x-1/2 whitespace-nowrap text-[10px] tabular-nums text-rig-500" style="left:{ratioOf(tick.at) * 100}%">{tick.label}</span>
					{/each}
				</div>
			</div>
			<p class="mt-1.5 text-[11px] text-rig-500">Hover the timeline to preview a snapshot, click to keep it.</p>
		</div>
	</div>
</Dialog>
