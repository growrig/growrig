<script lang="ts">
	import { NavigationMenu } from 'bits-ui';
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { live } from '$lib/live.svelte';
	import { getLocations } from '$lib/api';
	import { buildEnvTree } from '$lib/location';
	import type { Location } from '$lib/types';
	import Sprout from '@lucide/svelte/icons/sprout';
	import CalendarDays from '@lucide/svelte/icons/calendar-days';
	import Boxes from '@lucide/svelte/icons/boxes';
	import BookOpen from '@lucide/svelte/icons/book-open';
	import ChevronDown from '@lucide/svelte/icons/chevron-down';
	import MapPin from '@lucide/svelte/icons/map-pin';
	import Tent from '@lucide/svelte/icons/tent';
	import Wind from '@lucide/svelte/icons/wind';
	import LayoutList from '@lucide/svelte/icons/layout-list';
	import Cpu from '@lucide/svelte/icons/cpu';

	let locations = $state<Location[]>([]);
	onMount(() => {
		getLocations().then((l) => (locations = l)).catch(() => {});
	});

	const environments = $derived(live.snapshot?.environments ?? []);
	const tree = $derived(buildEnvTree(environments, locations));

	const path = $derived(page.url.pathname);
	const isActive = (href: string) => (href === '/' ? path === '/' : path.startsWith(href));

	const healthDot = (h: string) =>
		h === 'online' ? 'bg-leaf' : h === 'stale' ? 'bg-warn' : 'bg-danger';

	const growing = [
		{ href: '/grows', label: 'Grows', icon: Sprout },
		{ href: '/calendar', label: 'Calendar', icon: CalendarDays }
	];
	const resources = [
		{ href: '/inventory', label: 'Inventory', icon: Boxes },
		{ href: '/library', label: 'Library', icon: BookOpen }
	];

	const triggerBase =
		'group flex items-center gap-1 rounded-md px-3 py-1.5 text-sm transition-colors outline-none';
	const linkBase = 'rounded-md px-3 py-1.5 text-sm transition-colors outline-none';
	const activeClass = 'bg-rig-800 text-rig-50';
	const idleClass = 'text-rig-300 hover:bg-rig-800/50 hover:text-rig-100';
	const panelItem =
		'flex items-center gap-2 rounded-md px-3 py-2 text-sm text-rig-200 transition-colors hover:bg-rig-800 hover:text-rig-50';
	// Each dropdown panel renders in place under its own trigger (no shared
	// Viewport), so it aligns to the item the user opened.
	const panel =
		'absolute left-0 top-full z-50 mt-2 rounded-lg border border-rig-700 bg-rig-900 shadow-xl outline-none';
</script>

<NavigationMenu.Root class="relative hidden lg:block">
	<NavigationMenu.List class="flex items-center gap-1">
		<NavigationMenu.Item>
			<NavigationMenu.Link href="/" class="{linkBase} {isActive('/') ? activeClass : idleClass}">
				Home
			</NavigationMenu.Link>
		</NavigationMenu.Item>

		<NavigationMenu.Item class="relative">
			<NavigationMenu.Trigger
				class="{triggerBase} {[...growing, ...resources].some((i) => isActive(i.href))
					? activeClass
					: idleClass}"
			>
				Growing
				<ChevronDown
					size={14}
					class="transition-transform duration-200 group-data-[state=open]:rotate-180"
				/>
			</NavigationMenu.Trigger>
			<!-- One dropdown, two visual sections: the grow itself, then the shared
			     resources supporting it. Avoids a separate top-level Resources menu. -->
			<NavigationMenu.Content class="{panel} p-1">
				<div class="w-52">
					<p class="px-3 pb-1 pt-1.5 text-[11px] font-semibold uppercase tracking-wide text-rig-500">
						Growing
					</p>
					{#each growing as item (item.href)}
						<NavigationMenu.Link href={item.href} class={panelItem}>
							<item.icon size={16} class="text-rig-400" />
							{item.label}
						</NavigationMenu.Link>
					{/each}
					<div class="my-1 border-t border-rig-800"></div>
					<p class="px-3 pb-1 pt-1 text-[11px] font-semibold uppercase tracking-wide text-rig-500">
						Resources
					</p>
					{#each resources as item (item.href)}
						<NavigationMenu.Link href={item.href} class={panelItem}>
							<item.icon size={16} class="text-rig-400" />
							{item.label}
						</NavigationMenu.Link>
					{/each}
				</div>
			</NavigationMenu.Content>
		</NavigationMenu.Item>

		<NavigationMenu.Item class="relative">
			<NavigationMenu.Trigger
				class="{triggerBase} {isActive('/env') || isActive('/equipment') ? activeClass : idleClass}"
			>
				Environments
				<ChevronDown
					size={14}
					class="transition-transform duration-200 group-data-[state=open]:rotate-180"
				/>
			</NavigationMenu.Trigger>
			<NavigationMenu.Content class="{panel} p-2">
				<div class="w-72">
					<div class="max-h-[60vh] overflow-y-auto">
					{#if !tree.length}
						<p class="px-3 py-2 text-sm text-rig-500">No environments yet.</p>
					{:else}
						{#each tree as loc (loc.key)}
							<div class="mb-2 last:mb-0">
								<div
									class="flex items-center gap-1.5 px-3 py-1 text-[11px] font-semibold uppercase tracking-wide {loc.located
										? 'text-rig-400'
										: 'text-rig-500'}"
								>
									<MapPin size={12} />{loc.name}
								</div>
								{#each loc.rooms as node (node.room.id)}
									<NavigationMenu.Link href="/env/{node.room.id}" class={panelItem}>
										<Wind size={15} class="text-rig-400" />
										<span class="flex-1 truncate">{node.room.name}</span>
										<span class="h-2 w-2 rounded-full {healthDot(node.room.health)}"></span>
									</NavigationMenu.Link>
									{#each node.boxes as box (box.id)}
										<NavigationMenu.Link href="/env/{box.id}" class="{panelItem} pl-8">
											<Tent size={15} class="text-rig-400" />
											<span class="flex-1 truncate">{box.name}</span>
											<span class="h-2 w-2 rounded-full {healthDot(box.health)}"></span>
										</NavigationMenu.Link>
									{/each}
								{/each}
								{#each loc.looseBoxes as box (box.id)}
									<NavigationMenu.Link href="/env/{box.id}" class={panelItem}>
										<Tent size={15} class="text-rig-400" />
										<span class="flex-1 truncate">{box.name}</span>
										<span class="h-2 w-2 rounded-full {healthDot(box.health)}"></span>
									</NavigationMenu.Link>
								{/each}
							</div>
						{/each}
					{/if}
					</div>
					<div class="my-1 border-t border-rig-800"></div>
					<NavigationMenu.Link href="/env" class={panelItem}>
						<LayoutList size={16} class="text-rig-400" />
						All environments
					</NavigationMenu.Link>
					<NavigationMenu.Link href="/equipment" class={panelItem}>
						<Cpu size={16} class="text-rig-400" />
						All Equipment
					</NavigationMenu.Link>
				</div>
			</NavigationMenu.Content>
		</NavigationMenu.Item>

		<NavigationMenu.Item>
			<NavigationMenu.Link
				href="/log"
				class="{linkBase} {isActive('/log') ? activeClass : idleClass}"
			>
				Log
			</NavigationMenu.Link>
		</NavigationMenu.Item>
	</NavigationMenu.List>
</NavigationMenu.Root>
