<script lang="ts">
	import '../app.css';
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { live } from '$lib/live.svelte';

	let { children } = $props();

	onMount(() => {
		live.start();
		return () => live.stop();
	});

	const nav = [
		{ href: '/', label: 'Dashboard' },
		{ href: '/setup', label: 'Setup' }
	];

	const statusMeta = {
		live: { label: 'Live', dot: 'bg-leaf' },
		connecting: { label: 'Connecting', dot: 'bg-warn animate-pulse' },
		offline: { label: 'Offline', dot: 'bg-danger' }
	} as const;
</script>

<div class="min-h-screen">
	<header class="sticky top-0 z-10 border-b border-rig-800 bg-rig-900/60 backdrop-blur">
		<div class="mx-auto flex max-w-5xl items-center gap-6 px-4 py-3">
			<a href="/" class="flex items-center gap-2 font-semibold tracking-tight">
				<span class="grid h-7 w-7 place-items-center rounded-md bg-rig-500 text-rig-950">🌱</span>
				<span>GrowRig</span>
			</a>
			<nav class="flex gap-1 text-sm">
				{#each nav as item (item.href)}
					<a
						href={item.href}
						class="rounded-md px-3 py-1.5 transition-colors {page.url.pathname === item.href
							? 'bg-rig-800 text-rig-50'
							: 'text-rig-300 hover:bg-rig-800/50 hover:text-rig-100'}"
					>
						{item.label}
					</a>
				{/each}
			</nav>
			<div class="ml-auto flex items-center gap-2 text-sm text-rig-300">
				<span class="h-2 w-2 rounded-full {statusMeta[live.status].dot}"></span>
				{statusMeta[live.status].label}
			</div>
		</div>
	</header>

	<main class="mx-auto max-w-5xl px-4 py-6">
		{@render children()}
	</main>
</div>
