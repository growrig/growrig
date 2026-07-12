<script lang="ts">
	import { auth } from '$lib/auth.svelte';
	import Sprout from '@lucide/svelte/icons/sprout';

	let username = $state('');
	let password = $state('');
	let confirm = $state('');
	let error = $state<string | null>(null);
	let saving = $state(false);

	const valid = $derived(
		username.trim().length >= 3 && password.length >= 8 && password === confirm
	);

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		if (!valid || saving) return;
		saving = true;
		error = null;
		try {
			await auth.bootstrap(username.trim(), password);
			// The layout guard routes to the dashboard once authed.
		} catch (err) {
			error = err instanceof Error ? err.message : 'Setup failed';
			saving = false;
		}
	}

	const field =
		'w-full rounded-md border border-rig-700 bg-rig-950 px-3 py-2 text-sm focus:border-rig-500 focus:outline-none';
</script>

<div class="rounded-2xl border border-rig-800 bg-rig-900/40 p-6">
	<div class="mb-5 flex flex-col items-center text-center">
		<span class="mb-3 grid h-11 w-11 place-items-center rounded-xl bg-rig-500 text-rig-950">
			<Sprout size={24} />
		</span>
		<h1 class="text-xl font-semibold">Welcome to GrowRig</h1>
		<p class="mt-1 text-sm text-rig-400">Create the administrator account to get started.</p>
	</div>

	{#if error}
		<div class="mb-4 rounded-lg bg-danger/15 px-4 py-2 text-sm text-danger">{error}</div>
	{/if}

	<form onsubmit={submit} class="space-y-3">
		<label class="block">
			<span class="text-sm text-rig-400">Username</span>
			<input bind:value={username} autocomplete="username" class="{field} mt-1" />
		</label>
		<label class="block">
			<span class="text-sm text-rig-400">Password</span>
			<input type="password" bind:value={password} autocomplete="new-password" class="{field} mt-1" />
		</label>
		<label class="block">
			<span class="text-sm text-rig-400">Confirm password</span>
			<input type="password" bind:value={confirm} autocomplete="new-password" class="{field} mt-1" />
			{#if confirm && password !== confirm}
				<span class="mt-1 block text-xs text-danger">Passwords don't match.</span>
			{:else if password && password.length < 8}
				<span class="mt-1 block text-xs text-rig-500">Use at least 8 characters.</span>
			{/if}
		</label>
		<button
			type="submit"
			disabled={!valid || saving}
			class="w-full rounded-md bg-rig-500 px-5 py-2 text-sm font-medium text-rig-950 transition-colors hover:bg-rig-400 disabled:opacity-40"
		>
			{saving ? 'Creating…' : 'Create administrator'}
		</button>
	</form>
</div>
