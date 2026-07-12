<script lang="ts">
	import '../app.css';
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { live } from '$lib/live.svelte';
	import { auth } from '$lib/auth.svelte';
	import { preferences, onPreferencesUpdated } from '$lib/preferences.svelte';
	import { fmtClock } from '$lib/datetime';
	import { Button } from '$lib/components/ui';
	import Sprout from '@lucide/svelte/icons/sprout';
	import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
	import RotateCcw from '@lucide/svelte/icons/rotate-ccw';
	import Shield from '@lucide/svelte/icons/shield';
	import LogOut from '@lucide/svelte/icons/log-out';

	let { children } = $props();
	let clockNow = $state(new Date());
	const instanceTime = $derived(fmtClock(clockNow));

	// Routes that render without the app chrome and without requiring a session.
	const authRoutes = ['/login', '/setup'];
	const isAuthRoute = $derived(authRoutes.includes(page.url.pathname));

	onMount(() => {
		auth.init();
		return () => live.stop();
	});
	onMount(() => {
		const timer = setInterval(() => (clockNow = new Date()), 1000);
		const stop = onPreferencesUpdated((p) => preferences.apply(p));
		return () => {
			clearInterval(timer);
			stop();
		};
	});
	$effect(() => {
		if (auth.phase === 'authed') void preferences.load();
	});

	// Route guard: keep the URL consistent with the auth phase.
	$effect(() => {
		const path = page.url.pathname;
		switch (auth.phase) {
			case 'needs-setup':
				if (path !== '/setup') goto('/setup');
				break;
			case 'anonymous':
				if (!authRoutes.includes(path)) goto('/login');
				break;
			case 'authed':
				if (authRoutes.includes(path)) goto('/');
				else if (path.startsWith('/admin') && !auth.isAdmin) goto('/');
				break;
		}
	});

	// The live feed runs only for a signed-in user; it stops (and clears cached
	// state) whenever we leave the authed phase.
	let feedRunning = false;
	$effect(() => {
		if (auth.phase === 'authed' && !feedRunning) {
			live.start();
			feedRunning = true;
		} else if (auth.phase !== 'authed' && feedRunning) {
			live.stop();
			feedRunning = false;
		}
	});

	const nav = $derived([
		{ href: '/', label: 'Dashboard' },
		{ href: '/grows', label: 'Grows' },
		{ href: '/activity', label: 'Activity' },
		...(auth.isAdmin ? [{ href: '/admin', label: 'Admin' }] : [])
	]);

	const statusMeta = {
		live: { label: 'Live', dot: 'bg-leaf' },
		connecting: { label: 'Connecting', dot: 'bg-warn animate-pulse' },
		offline: { label: 'Offline', dot: 'bg-danger' }
	} as const;
</script>

{#if isAuthRoute && auth.phase !== 'authed'}
	<!-- Bare shell for /login and /setup. -->
	<div class="grid min-h-screen place-items-center px-4">
		<div class="w-full max-w-sm">{@render children()}</div>
	</div>
{:else if auth.phase !== 'authed'}
	<!-- Loading or mid-redirect: never mount app pages (which call authed APIs)
	     until a session exists, or a stray 401 would bounce us to /login. -->
	<div class="grid min-h-screen place-items-center text-rig-400">
		<div class="flex items-center gap-2">
			<span class="grid h-8 w-8 place-items-center rounded-md bg-rig-500 text-rig-950">
				<Sprout size={20} />
			</span>
			<span>Starting GrowRig…</span>
		</div>
	</div>
{:else}
	<div class="min-h-screen">
		<header class="sticky top-0 z-10 border-b border-rig-800 bg-rig-900/60 backdrop-blur">
			<div class="mx-auto flex max-w-5xl items-center gap-6 px-4 py-3">
				<a href="/" class="flex items-center gap-2 font-semibold tracking-tight">
					<span class="grid h-7 w-7 place-items-center rounded-md bg-rig-500 text-rig-950">
						<Sprout size={18} />
					</span>
					<span>GrowRig Hub</span>
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
				<div class="ml-auto flex items-center gap-4">
					<div class="hidden text-sm tabular-nums text-rig-400 sm:block" title="{preferences.timezone} · {preferences.locale}">{instanceTime}</div>
					<div class="flex items-center gap-2 text-sm text-rig-300">
						<span class="h-2 w-2 rounded-full {statusMeta[live.status].dot}"></span>
						<span class="tabular-nums">{live.status === 'live' ? (live.latencyMs == null ? '— ms' : `${live.latencyMs} ms`) : statusMeta[live.status].label}</span>
					</div>
					{#if auth.user}
						<div class="flex items-center gap-2 border-l border-rig-800 pl-4">
							<a
								href="/account"
								class="flex items-center gap-1.5 rounded-md px-2 py-1 text-sm text-rig-300 transition-colors hover:bg-rig-800/50 hover:text-rig-100"
								title="Account & passkeys"
							>
								{#if auth.isAdmin}<Shield size={14} class="text-leaf" />{/if}
								{auth.user.username}
							</a>
							<button
								onclick={() => auth.logout()}
								class="flex items-center gap-1 rounded-md px-2 py-1 text-sm text-rig-400 transition-colors hover:bg-rig-800/50 hover:text-rig-100"
								title="Log out"
							>
								<LogOut size={15} />
							</button>
						</div>
					{/if}
				</div>
			</div>
		</header>

		<main class="mx-auto max-w-5xl px-4 py-6">
			<svelte:boundary onerror={(e) => console.error('[GrowRig] page error:', e)}>
				{@render children()}

				{#snippet failed(error, reset)}
					{@const err = error instanceof Error ? error : new Error(String(error))}
					<div class="mx-auto max-w-2xl rounded-xl border border-danger/40 bg-danger/5 p-6">
						<div class="mb-2 flex items-center gap-2 text-danger">
							<TriangleAlert size={22} />
							<h1 class="text-lg font-semibold">Something broke on this page</h1>
						</div>
						<p class="mb-4 text-sm text-rig-300">
							The rest of GrowRig is still running. You can retry this view, reload, or open the
							Debug page to inspect the live state.
						</p>
						<div class="mb-4 overflow-hidden rounded-lg border border-rig-800 bg-rig-950/60">
							<div class="border-b border-rig-800 px-4 py-2 text-xs font-medium text-rig-400">
								{err.name}: {err.message}
							</div>
							{#if err.stack}
								<pre class="max-h-56 overflow-auto p-4 text-xs leading-relaxed text-rig-400"><code>{err.stack}</code></pre>
							{/if}
						</div>
						<div class="flex flex-wrap gap-2">
							<Button onclick={reset}><RotateCcw size={15} /> Try again</Button>
							<Button variant="secondary" onclick={() => location.reload()}>Reload page</Button>
							{#if auth.isAdmin}
								<Button variant="ghost" href="/admin/debug">Open Debug</Button>
							{/if}
						</div>
					</div>
				{/snippet}
			</svelte:boundary>
		</main>
	</div>
{/if}
