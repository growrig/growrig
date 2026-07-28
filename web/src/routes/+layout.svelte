<script lang="ts">
	import '../app.css';
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { live } from '$lib/live.svelte';
	import { auth } from '$lib/auth.svelte';
	import { theme } from '$lib/theme.svelte';
	import { attention } from '$lib/attention.svelte';
	import { preferences, onPreferencesUpdated } from '$lib/preferences.svelte';
	import { fmtClock } from '$lib/datetime';
	import { fmtLatencyMs } from '$lib/format';
	import { NavigationMenu } from 'bits-ui';
	import { Button, DropdownMenu, type DropdownItem } from '$lib/components/ui';
	import NavMenu from '$lib/components/NavMenu.svelte';
	import GrowAIChat from '$lib/components/GrowAIChat.svelte';
	import Toaster from '$lib/components/Toaster.svelte';
	import EnvironmentFormModal from '$lib/components/EnvironmentFormModal.svelte';
	import { addEnv } from '$lib/addEnv.svelte';
	import Sprout from '@lucide/svelte/icons/sprout';
	import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
	import RotateCcw from '@lucide/svelte/icons/rotate-ccw';
	import Shield from '@lucide/svelte/icons/shield';
	import Settings from '@lucide/svelte/icons/settings';
	import UserIcon from '@lucide/svelte/icons/user';
	import LogOut from '@lucide/svelte/icons/log-out';
	import ChevronDown from '@lucide/svelte/icons/chevron-down';
	import Plus from '@lucide/svelte/icons/plus';
	import Image from '@lucide/svelte/icons/image';
	import Droplet from '@lucide/svelte/icons/droplet';
	import Tent from '@lucide/svelte/icons/tent';
	import Menu from '@lucide/svelte/icons/menu';
	import X from '@lucide/svelte/icons/x';

	let { children } = $props();
	let clockNow = $state(new Date());
	// Mobile nav drawer: collapsed by default, and closed again on every
	// navigation so a tapped link doesn't leave the menu hanging open.
	let menuOpen = $state(false);
	$effect(() => {
		page.url.pathname;
		menuOpen = false;
	});

	// Routes that render without the app chrome and without requiring a session.
	const authRoutes = ['/login', '/setup'];
	const isAuthRoute = $derived(authRoutes.includes(page.url.pathname));

	onMount(() => theme.init());
	onMount(() => {
		auth.init();
		return () => live.stop();
	});
	const instanceTime = $derived(fmtClock(clockNow));

	onMount(() => {
		const clock = setInterval(() => (clockNow = new Date()), 1000);
		// Refresh the "needs attention" projection periodically; it's cheap to
		// recompute server-side and keeps Home's panel current.
		const refresh = setInterval(() => {
			if (auth.phase === 'authed') void attention.load();
		}, 60_000);
		const stop = onPreferencesUpdated((p) => preferences.apply(p));
		return () => {
			clearInterval(clock);
			clearInterval(refresh);
			stop();
		};
	});
	$effect(() => {
		if (auth.phase === 'authed') {
			void preferences.load();
			void attention.load();
		}
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
			attention.reset();
		}
	});

	// Grouped navigation for the mobile drawer (desktop uses <NavMenu />).
	const navGroups = $derived([
		{ label: '', items: [{ href: '/', label: 'Home' }] },
		{
			label: 'Environments',
			items: [
				{ href: '/env', label: 'All environments' },
				{ href: '/equipment', label: 'All Equipment' }
			]
		},
		{
			label: 'Growing',
			items: [
				{ href: '/grows', label: 'Grows' },
				{ href: '/calendar', label: 'Calendar' }
			]
		},
		{
			label: 'Resources',
			items: [
				{ href: '/inventory', label: 'Inventory' },
				{ href: '/library', label: 'Library' }
			]
		},
		{ label: '', items: [{ href: '/log', label: 'Log' }] }
	]);

	const statusMeta = {
		live: { label: 'Live', dot: 'bg-leaf' },
		connecting: { label: 'Connecting', dot: 'bg-warn animate-pulse' },
		offline: { label: 'Offline', dot: 'bg-danger' }
	} as const;

	// Quick-add menu. Grow/environment creation are global; Log care and Add
	// photo need a grow, so they jump to the single active grow when there is
	// exactly one, otherwise to the Grows list to pick one. (Add photo can't
	// auto-open the OS file picker across a navigation, so it lands on the grow.)
	const activeGrows = $derived((live.snapshot?.grows ?? []).filter((g) => g.status === 'active'));
	const soleActive = $derived(activeGrows.length === 1 ? activeGrows[0] : null);
	const addMenu = $derived<DropdownItem[]>([
		{ label: 'Add photo', icon: Image, onSelect: () => goto(soleActive ? `/grows/${soleActive.id}` : '/grows') },
		{ label: 'Log care', icon: Droplet, onSelect: () => goto(soleActive ? `/grows/${soleActive.id}?action=logcare` : '/grows') },
		{ label: 'Add grow', icon: Sprout, onSelect: () => goto('/grows?new=1') },
		{ label: 'Add environment', icon: Tent, onSelect: () => addEnv.start() }
	]);

	// The username's dropdown submenu: account, admin settings, and log out.
	const userMenu = $derived<DropdownItem[]>([
		{ label: 'Account & passkeys', href: '/account', icon: UserIcon },
		...(auth.isAdmin ? [{ label: 'Settings', href: '/admin', icon: Settings }] : []),
		{ label: 'Log out', onSelect: () => auth.logout(), icon: LogOut }
	]);
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
			<span class="grid h-8 w-8 place-items-center rounded-md bg-leaf text-rig-950">
				<Sprout size={20} />
			</span>
			<span>Starting GrowRig…</span>
		</div>
	</div>
{:else}
	<div class="min-h-screen">
		<header class="sticky top-0 z-10 border-b border-rig-800 bg-rig-900/60 backdrop-blur">
			<div class="mx-auto flex max-w-5xl items-center gap-4 px-4 py-3 lg:gap-6">
				<a href="/" class="flex shrink-0 items-center gap-2 font-semibold tracking-tight">
					<span class="grid h-7 w-7 place-items-center rounded-md bg-leaf text-rig-950">
						<Sprout size={18} />
					</span>
					<span>GrowRig</span>
				</a>
				<NavMenu />
				<div class="ml-auto flex items-center gap-3 lg:gap-4">
					{#if auth.isAdmin}
						<DropdownMenu
							items={addMenu}
							align="end"
							triggerClass="inline-flex items-center gap-1 rounded-md border border-rig-700 px-2.5 py-1 text-sm text-rig-200 outline-none transition-colors hover:border-leaf hover:text-rig-50"
						>
							{#snippet trigger()}
								<Plus size={15} /> <span class="hidden sm:inline">Add</span>
							{/snippet}
						</DropdownMenu>
					{/if}
					<div class="hidden text-sm tabular-nums text-rig-400 sm:block" title="{preferences.timezone} · {preferences.locale}">{instanceTime}</div>
					<div class="flex items-center gap-2 text-sm text-rig-300">
						<span class="h-2 w-2 rounded-full {statusMeta[live.status].dot}"></span>
						<span class="hidden tabular-nums sm:inline">{live.status === 'live' ? (live.latencyMs == null ? '— ms' : `${fmtLatencyMs(live.latencyMs)} ms`) : statusMeta[live.status].label}</span>
					</div>
					{#if auth.user}
						<div class="hidden border-l border-rig-800 pl-4 lg:block">
							<NavigationMenu.Root class="relative">
								<NavigationMenu.List>
									<NavigationMenu.Item class="relative">
										<NavigationMenu.Trigger
											class="group flex items-center gap-1.5 rounded-md px-2 py-1 text-sm text-rig-300 outline-none transition-colors hover:bg-rig-800/50 hover:text-rig-100"
										>
											{auth.user?.username}
											<ChevronDown
												size={14}
												class="transition-transform duration-200 group-data-[state=open]:rotate-180"
											/>
										</NavigationMenu.Trigger>
										<NavigationMenu.Content
											class="absolute right-0 top-full z-50 mt-2 w-52 rounded-lg border border-rig-700 bg-rig-900 p-1 shadow-xl outline-none"
										>
											{#each userMenu as item (item.label)}
												{#if item.href}
													<NavigationMenu.Link
														href={item.href}
														class="flex items-center gap-2 rounded-md px-3 py-2 text-sm text-rig-200 transition-colors hover:bg-rig-800 hover:text-rig-50"
													>
														{#if item.icon}<item.icon size={16} class="text-rig-400" />{/if}
														{item.label}
													</NavigationMenu.Link>
												{:else}
													<button
														onclick={item.onSelect}
														class="flex w-full items-center gap-2 rounded-md px-3 py-2 text-left text-sm text-rig-200 transition-colors hover:bg-rig-800 hover:text-rig-50"
													>
														{#if item.icon}<item.icon size={16} class="text-rig-400" />{/if}
														{item.label}
													</button>
												{/if}
											{/each}
										</NavigationMenu.Content>
									</NavigationMenu.Item>
								</NavigationMenu.List>
							</NavigationMenu.Root>
						</div>
					{/if}
					<button
						onclick={() => (menuOpen = !menuOpen)}
						class="grid h-9 w-9 place-items-center rounded-md text-rig-300 transition-colors hover:bg-rig-800/50 hover:text-rig-100 lg:hidden"
						aria-label={menuOpen ? 'Close menu' : 'Open menu'}
						aria-expanded={menuOpen}
					>
						{#if menuOpen}<X size={20} />{:else}<Menu size={20} />{/if}
					</button>
				</div>
			</div>

			<!-- Mobile drawer: grouped nav plus the account actions, stacked. -->
			{#if menuOpen}
				<nav class="mx-auto max-w-5xl border-t border-rig-800 px-2 py-2 lg:hidden">
					{#each navGroups as group (group.label)}
						{#if group.label}
							<div class="px-3 pb-1 pt-3 text-[11px] font-semibold uppercase tracking-wide text-rig-500">
								{group.label}
							</div>
						{/if}
						{#each group.items as item (item.href)}
							<a
								href={item.href}
								class="block rounded-md px-3 py-2.5 text-sm transition-colors {page.url.pathname === item.href
									? 'bg-rig-800 text-rig-50'
									: 'text-rig-300 hover:bg-rig-800/50 hover:text-rig-100'}"
							>
								{item.label}
							</a>
						{/each}
					{/each}
					{#if auth.user}
						<!-- Account section mirrors the desktop username dropdown. -->
						<div class="mt-2 border-t border-rig-800 pt-2">
							<div class="flex items-center gap-1.5 px-3 pb-1 pt-1 text-[11px] font-semibold uppercase tracking-wide text-rig-500">
								{#if auth.isAdmin}<Shield size={12} class="text-leaf" />{/if}
								{auth.user.username}
							</div>
							{#each userMenu as item (item.label)}
								{#if item.href}
									<a
										href={item.href}
										class="flex items-center gap-2 rounded-md px-3 py-2.5 text-sm text-rig-300 transition-colors hover:bg-rig-800/50 hover:text-rig-100"
									>
										{#if item.icon}<item.icon size={16} class="text-rig-400" />{/if}
										{item.label}
									</a>
								{:else}
									<button
										onclick={item.onSelect}
										class="flex w-full items-center gap-2 rounded-md px-3 py-2.5 text-left text-sm text-rig-300 transition-colors hover:bg-rig-800/50 hover:text-rig-100"
									>
										{#if item.icon}<item.icon size={16} class="text-rig-400" />{/if}
										{item.label}
									</button>
								{/if}
							{/each}
						</div>
					{/if}
				</nav>
			{/if}
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
		<footer class="mx-auto max-w-5xl px-4 pb-10 pt-2">
			<div class="border-t border-rig-800/80 pt-6 text-center text-xs text-rig-600">
				GrowRig
			</div>
		</footer>
		<GrowAIChat />
		<Toaster />
		{#if auth.isAdmin}
			<EnvironmentFormModal
				bind:open={addEnv.open}
				defaultKind={addEnv.kind}
				defaultAirSourceId={addEnv.airSourceId}
				defaultLocationId={addEnv.locationId}
				onSaved={(e) => goto(`/env/${e.id}`)}
			/>
		{/if}
	</div>
{/if}
