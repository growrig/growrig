<script lang="ts">
	import { onMount } from 'svelte';
	import { getCameraStats, type CameraStats } from '$lib/api';
	interface Props { cameraId: string; class?: string; showProtocol?: boolean; }
	let { cameraId, class: className = '', showProtocol = false }: Props = $props();
	let stats = $state<CameraStats | null>(null);
	let unavailable = $state(false);
	function refresh() {
		getCameraStats(cameraId)
			.then((value) => { stats = value; unavailable = false; })
			.catch(() => { unavailable = true; });
	}
	onMount(() => { refresh(); const timer = setInterval(refresh, 2000); return () => clearInterval(timer); });
	function bitrate(value: number): string {
		if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)} Mbps`;
		if (value >= 1_000) return `${Math.round(value / 1_000)} kbps`;
		return `${value} bps`;
	}
	const title = $derived(stats?.lastError || (stats?.lastFrame ? `Last frame ${new Date(stats.lastFrame).toLocaleString()}` : 'Waiting for the first camera frame'));
</script>

<span class="inline-flex items-center gap-1.5 tabular-nums {className}" {title}>
	{#if unavailable}
		<span class="h-2 w-2 rounded-full bg-danger"></span><span class="text-danger">Status unavailable</span>
	{:else if stats?.online}
		<span class="h-2 w-2 rounded-full bg-leaf"></span>
		<span>{stats.fps.toFixed(1)} FPS · {bitrate(stats.bitrateBps)}{showProtocol ? ' · Live RTSP' : ''}</span>
	{:else if stats?.status === 'reconnecting'}
		<span class="h-2 w-2 animate-pulse rounded-full bg-danger"></span>
		<span class="text-danger">Reconnecting{stats.retryCount ? ` · attempt ${stats.retryCount}` : ''}{showProtocol ? ' · RTSP' : ''}</span>
	{:else if stats?.status === 'stalled'}
		<span class="h-2 w-2 animate-pulse rounded-full bg-danger"></span>
		<span class="text-danger">Stream stalled{showProtocol ? ' · RTSP' : ''}</span>
	{:else}
		<span class="h-2 w-2 animate-pulse rounded-full bg-warn"></span>
		<span class="text-warn">Connecting{showProtocol ? ' · RTSP' : ''}</span>
	{/if}
</span>
