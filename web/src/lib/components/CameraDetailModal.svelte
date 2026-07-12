<script lang="ts">
	import { getCameraSnapshots, cameraArchiveURL, cameraProxyURL, type CameraSnapshot } from '$lib/api';
	import type { CameraRef } from '$lib/types';
	import { Dialog } from '$lib/components/ui';
	import CameraPreview from './CameraPreview.svelte';
	import Radio from '@lucide/svelte/icons/radio';
	import { fmtSnapshotTime } from '$lib/datetime';

	interface Props { open?: boolean; camera: CameraRef; }
	let { open = $bindable(false), camera }: Props = $props();
	let snapshots = $state<CameraSnapshot[]>([]);
	let selected = $state<CameraSnapshot | null>(null);
	let loading = $state(false);
	let viewport: HTMLElement | null = null;
	let viewportWidth = $state(0);
	let contentWidth = $state(0);
	let scrollLeft = $state(0);
	let dragging = $state(false);
	let dragX = 0;
	let dragScroll = 0;
	let suppressClick = false;
	const canUseRecordedView = $derived(camera.cameraType === 'rtsp' || (!camera.streamUrl && !camera.entity));

	$effect(() => {
		if (!open || !canUseRecordedView) return;
		void camera.id;
		selected = null;
		loading = true;
		getCameraSnapshots(camera.id)
			.then((items) => (snapshots = items))
			.catch(() => (snapshots = []))
			.finally(() => (loading = false));
	});

	const selectedURL = $derived(selected ? cameraArchiveURL(camera.id, selected.id) : cameraProxyURL(camera.id));
	const formatTime = (value: string) => fmtSnapshotTime(value);
	const maxScroll = $derived(Math.max(0, contentWidth - viewportWidth));
	const thumbWidth = $derived(contentWidth > 0 ? Math.max(32, (viewportWidth / contentWidth) * viewportWidth) : viewportWidth);
	const thumbLeft = $derived(maxScroll > 0 ? (scrollLeft / maxScroll) * Math.max(0, viewportWidth - thumbWidth) : 0);
	function scrollNode(node: HTMLElement) {
		viewport = node;
		const sync = () => { viewportWidth = node.clientWidth; contentWidth = node.scrollWidth; scrollLeft = node.scrollLeft; };
		const observer = new ResizeObserver(sync);
		observer.observe(node); if (node.firstElementChild) observer.observe(node.firstElementChild);
		node.addEventListener('scroll', sync, { passive: true });
		queueMicrotask(sync);
		return { destroy() { observer.disconnect(); node.removeEventListener('scroll', sync); if (viewport === node) viewport = null; } };
	}
	function pointerDown(event: PointerEvent) {
		if (event.pointerType !== 'mouse' || !viewport) return;
		dragging = true; suppressClick = false; dragX = event.clientX; dragScroll = viewport.scrollLeft;
	}
	function pointerMove(event: PointerEvent) {
		if (!dragging || !viewport) return;
		const distance = event.clientX - dragX;
		if (Math.abs(distance) > 4 && !suppressClick) {
			suppressClick = true;
			viewport.setPointerCapture(event.pointerId);
		}
		viewport.scrollLeft = dragScroll - distance;
	}
	function pointerUp(event: PointerEvent) {
		if (!dragging || !viewport) return;
		dragging = false;
		if (viewport.hasPointerCapture(event.pointerId)) viewport.releasePointerCapture(event.pointerId);
		setTimeout(() => (suppressClick = false), 0);
	}
	function wheel(event: WheelEvent) {
		if (!viewport || Math.abs(event.deltaY) <= Math.abs(event.deltaX)) return;
		viewport.scrollLeft += event.deltaY;
		event.preventDefault();
	}
	function trackClick(event: MouseEvent) {
		if (!viewport || maxScroll <= 0) return;
		const rect = (event.currentTarget as HTMLElement).getBoundingClientRect();
		viewport.scrollLeft = ((event.clientX - rect.left) / rect.width) * maxScroll;
	}
</script>

<Dialog bind:open title={camera.name} description="Live camera and recorded snapshots" size="3xl">
	<div class="space-y-4">
		<div class="relative">
			<CameraPreview url={selectedURL} liveUrl={!selected && canUseRecordedView ? cameraProxyURL(camera.id, true) : ''} type={selected ? 'mjpeg' : canUseRecordedView ? 'snapshot' : camera.cameraType} class="border-rig-700" emptyLabel="Connecting to camera…" errorLabel="Connecting to camera…" />
			{#if selected}
				<button type="button" onclick={() => (selected = null)} class="absolute right-3 top-3 inline-flex items-center gap-1.5 rounded-md bg-rig-950/85 px-3 py-1.5 text-xs font-medium text-rig-100 shadow hover:bg-rig-800"><Radio size={14} class="text-leaf" /> Return to live</button>
			{:else if canUseRecordedView}
				<span class="absolute right-3 top-3 inline-flex items-center gap-1.5 rounded-md bg-rig-950/75 px-2.5 py-1 text-xs text-leaf"><span class="h-1.5 w-1.5 animate-pulse rounded-full bg-leaf"></span> Live</span>
			{/if}
		</div>

		<div>
			<div class="mb-2 flex items-center justify-between"><h3 class="text-sm font-semibold uppercase tracking-wide text-rig-400">Snapshot timeline</h3><span class="text-xs text-rig-500">{snapshots.length} most recent</span></div>
			{#if loading}
				<div class="py-6 text-center text-sm text-rig-500">Loading snapshots…</div>
			{:else if snapshots.length}
				<div class="relative w-full overflow-hidden">
					<div use:scrollNode role="region" aria-label="Snapshot timeline" class="snapshot-scroll w-full touch-pan-x overflow-x-auto pb-3 {dragging ? 'cursor-grabbing select-none' : 'cursor-grab'}" onpointerdown={pointerDown} onpointermove={pointerMove} onpointerup={pointerUp} onpointercancel={pointerUp} onwheel={wheel}>
						<div class="flex w-max gap-2">
							{#each snapshots as snapshot (snapshot.id)}
								<button type="button" onclick={() => { if (!suppressClick) selected = snapshot; }} class="w-32 shrink-0 overflow-hidden rounded-lg border text-left transition-colors {selected?.id === snapshot.id ? 'border-leaf ring-1 ring-leaf/40' : 'border-rig-800 hover:border-rig-500'}">
									<img src={cameraArchiveURL(camera.id, snapshot.id)} alt="Snapshot at {formatTime(snapshot.time)}" loading="lazy" draggable="false" class="aspect-video w-full select-none object-cover" />
									<span class="block truncate px-2 py-1.5 text-[10px] text-rig-400">{formatTime(snapshot.time)}</span>
								</button>
							{/each}
						</div>
					</div>
					{#if maxScroll > 0}
						<button type="button" aria-label="Snapshot timeline scrollbar" onclick={trackClick} class="relative block h-2 w-full rounded-full bg-rig-800/70 p-0.5">
							<span class="pointer-events-none absolute left-0 top-0.5 h-1 rounded-full bg-rig-500 transition-colors" style="width:{thumbWidth}px;transform:translateX({thumbLeft}px)"></span>
						</button>
					{/if}
				</div>
			{:else}
				<div class="rounded-lg border border-dashed border-rig-800 py-6 text-center text-sm text-rig-500">No archived snapshots yet.</div>
			{/if}
		</div>
	</div>
</Dialog>

<style>
	.snapshot-scroll { scrollbar-width: none; -ms-overflow-style: none; }
	.snapshot-scroll::-webkit-scrollbar { display: none; }
</style>
