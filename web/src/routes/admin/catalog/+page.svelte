<script lang="ts">
	import { onMount } from 'svelte';
	import GitFork from '@lucide/svelte/icons/git-fork';
	import Plus from '@lucide/svelte/icons/plus';
	import RefreshCw from '@lucide/svelte/icons/refresh-cw';
	import Trash2 from '@lucide/svelte/icons/trash-2';
	import ExternalLink from '@lucide/svelte/icons/external-link';
	import { createCatalogSource, deleteCatalogSource, getCatalogSources, refreshCatalogSource } from '$lib/api';
	import type { CatalogSource } from '$lib/types';

	let sources = $state<CatalogSource[]>([]);
	let mergedKinds = $state<string[]>([]);
	let repo = $state('');
	let ref = $state('');
	let loading = $state(true);
	let saving = $state(false);
	let refreshing = $state<string | null>(null);
	let error = $state<string | null>(null);

	const fieldClass = 'w-full rounded-md border border-rig-700 bg-rig-950 px-3 py-2 text-sm text-rig-100 outline-none placeholder:text-rig-600 focus:border-rig-500';

	onMount(load);

	async function load() {
		loading = true;
		error = null;
		try {
			const result = await getCatalogSources();
			sources = result.sources;
			mergedKinds = result.mergedKinds;
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to load catalog sources';
		} finally {
			loading = false;
		}
	}

	async function add() {
		if (!repo.trim()) return;
		saving = true;
		error = null;
		try {
			await createCatalogSource(repo.trim(), ref.trim());
			repo = '';
			ref = '';
			await load();
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to add catalog source';
		} finally {
			saving = false;
		}
	}

	async function refresh(source: CatalogSource) {
		refreshing = source.id;
		error = null;
		try {
			await refreshCatalogSource(source.id);
			await load();
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to refresh catalog source';
		} finally {
			refreshing = null;
		}
	}

	async function remove(source: CatalogSource) {
		if (!confirm(`Remove “${source.name}”? Its devices and integrations will no longer be available.`)) return;
		error = null;
		try {
			await deleteCatalogSource(source.id);
			await load();
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to remove catalog source';
		}
	}

	function repoURL(source: CatalogSource) {
		return `https://github.com/${source.repo}`;
	}

	function formattedDate(value: string) {
		return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value));
	}
</script>

<div class="space-y-8">
	<div>
		<h2 class="text-lg font-semibold">Catalog sources</h2>
		<p class="mt-1 text-sm text-rig-400">Add public GitHub repositories that follow the GrowRig catalog layout.</p>
	</div>

	{#if error}
		<div class="rounded-lg bg-danger/15 px-4 py-2 text-sm text-danger">{error}</div>
	{/if}

	<form onsubmit={(event) => { event.preventDefault(); add(); }} class="rounded-xl border border-rig-800 bg-rig-900/30 p-4">
		<div class="flex items-center gap-2">
			<GitFork size={18} class="text-rig-400" />
			<h3 class="font-medium">Add a repository</h3>
		</div>
		<p class="mt-1 text-xs text-rig-500">The repository must contain a valid <code>catalog.yaml</code>. Only public GitHub repositories are supported.</p>
		<div class="mt-4 grid gap-3 md:grid-cols-[minmax(0,1fr)_minmax(10rem,0.35fr)_auto]">
			<label>
				<span class="mb-1 block text-xs text-rig-400">Repository</span>
				<input class={fieldClass} bind:value={repo} placeholder="owner/repository" required />
			</label>
			<label>
				<span class="mb-1 block text-xs text-rig-400">Branch, tag, or commit</span>
				<input class={fieldClass} bind:value={ref} placeholder="Default branch" />
			</label>
			<button type="submit" disabled={saving || !repo.trim()} class="mt-5 flex items-center justify-center gap-1.5 rounded-md bg-rig-500 px-4 py-2 text-sm font-medium text-rig-950 disabled:opacity-50">
				<Plus size={15} /> {saving ? 'Fetching…' : 'Add source'}
			</button>
		</div>
	</form>

	<section class="space-y-3">
		<div>
			<h3 class="font-medium">Additional sources</h3>
			<p class="text-xs text-rig-500">Devices and integrations are merged immediately. Other declared content is cached for future support.</p>
		</div>
		{#if loading}
			<p class="text-sm text-rig-400">Loading catalog sources…</p>
		{:else if sources.length === 0}
			<div class="rounded-xl border border-dashed border-rig-700 p-8 text-center">
				<GitFork class="mx-auto text-rig-500" size={28} />
				<p class="mt-2 font-medium">Only the built-in catalog is active</p>
				<p class="text-sm text-rig-400">Add a repository above to extend it.</p>
			</div>
		{:else}
			<div class="grid gap-3 lg:grid-cols-2">
				{#each sources as source (source.id)}
					<div class="rounded-xl border border-rig-800 bg-rig-900/40 p-4">
						<div class="flex items-start gap-3">
							<GitFork size={22} class="mt-0.5 shrink-0 text-rig-400" />
							<div class="min-w-0 flex-1">
								<div class="flex flex-wrap items-center gap-2">
									<h4 class="font-medium">{source.name}</h4>
									{#each source.provides as kind}
										<span class={`rounded px-1.5 py-0.5 text-[10px] uppercase ${mergedKinds.includes(kind) ? 'bg-leaf/15 text-leaf' : 'bg-rig-800 text-rig-500'}`}>{kind}</span>
									{/each}
								</div>
								{#if source.description}<p class="mt-1 text-sm text-rig-400">{source.description}</p>{/if}
								<a href={repoURL(source)} target="_blank" rel="noreferrer" class="mt-2 inline-flex items-center gap-1 text-xs text-rig-400 hover:text-rig-100">{source.repo}{source.ref ? ` @ ${source.ref}` : ''}<ExternalLink size={11} /></a>
								<p class="mt-1 text-[11px] text-rig-600">Fetched {formattedDate(source.fetchedAt)}{source.maintainer ? ` · ${source.maintainer}` : ''}</p>
							</div>
						</div>
						<div class="mt-4 flex gap-2 border-t border-rig-800 pt-3">
							<button onclick={() => refresh(source)} disabled={refreshing === source.id} class="flex items-center gap-1.5 rounded-md bg-rig-800 px-3 py-1.5 text-xs hover:bg-rig-700 disabled:opacity-50"><RefreshCw size={14} class={refreshing === source.id ? 'animate-spin' : ''} />{refreshing === source.id ? 'Refreshing…' : 'Refresh'}</button>
							<button onclick={() => remove(source)} class="ml-auto rounded-md p-1.5 text-rig-500 hover:bg-danger/10 hover:text-danger" aria-label={`Remove ${source.name}`}><Trash2 size={15} /></button>
						</div>
					</div>
				{/each}
			</div>
		{/if}
	</section>
</div>
