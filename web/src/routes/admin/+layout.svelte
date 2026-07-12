<script lang="ts">
	import { page } from '$app/state';
	import Users from '@lucide/svelte/icons/users';
	import HousePlug from '@lucide/svelte/icons/house-plug';
	import Bug from '@lucide/svelte/icons/bug';

	let { children } = $props();

	const tabs = [
		{ href: '/admin', label: 'Users', icon: Users },
		{ href: '/admin/home-assistant', label: 'Home Assistant', icon: HousePlug },
		{ href: '/admin/debug', label: 'Debug', icon: Bug }
	];
	const isActive = (href: string) => page.url.pathname === href;
</script>

<div class="space-y-6">
	<div>
		<h1 class="text-2xl font-semibold">Control panel</h1>
		<p class="mt-1 text-sm text-rig-400">Manage this GrowRig instance.</p>
	</div>

	<nav class="flex gap-1 border-b border-rig-800">
		{#each tabs as tab (tab.href)}
			<a
				href={tab.href}
				class="-mb-px flex items-center gap-2 border-b-2 px-4 py-2.5 text-sm font-medium transition-colors {isActive(
					tab.href
				)
					? 'border-leaf text-rig-50'
					: 'border-transparent text-rig-400 hover:text-rig-100'}"
			>
				<tab.icon size={16} />
				{tab.label}
			</a>
		{/each}
	</nav>

	{@render children()}
</div>
