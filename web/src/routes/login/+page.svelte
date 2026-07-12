<script lang="ts">
	import { auth } from '$lib/auth.svelte';
	import { passkeysSupported } from '$lib/webauthn';
	import Sprout from '@lucide/svelte/icons/sprout';
	import KeyRound from '@lucide/svelte/icons/key-round';

	let mode = $state<'login' | 'register'>('login');
	let username = $state('');
	let password = $state('');
	let error = $state<string | null>(null);
	let saving = $state(false);
	let passkeyBusy = $state(false);

	const canRegister = $derived(auth.signupEnabled);
	const valid = $derived(username.trim().length > 0 && password.length > 0);

	async function signInWithPasskey() {
		if (passkeyBusy) return;
		passkeyBusy = true;
		error = null;
		try {
			await auth.loginWithPasskey();
			// The layout guard routes to the dashboard once authed.
		} catch (err) {
			// A user cancelling the native prompt shouldn't read as a hard error.
			const msg = err instanceof Error ? err.message : 'Passkey sign-in failed';
			if (!/cancel|abort|not allowed/i.test(msg)) error = msg;
		} finally {
			passkeyBusy = false;
		}
	}

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		if (!valid || saving) return;
		saving = true;
		error = null;
		try {
			if (mode === 'register') await auth.register(username.trim(), password);
			else await auth.login(username.trim(), password);
			// The layout guard routes to the dashboard once authed.
		} catch (err) {
			error = err instanceof Error ? err.message : 'Sign in failed';
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
		<h1 class="text-xl font-semibold">
			{mode === 'register' ? 'Create your account' : 'Sign in to GrowRig'}
		</h1>
	</div>

	{#if error}
		<div class="mb-4 rounded-lg bg-danger/15 px-4 py-2 text-sm text-danger">{error}</div>
	{/if}

	{#if mode === 'login' && passkeysSupported()}
		<button
			onclick={signInWithPasskey}
			disabled={passkeyBusy}
			class="mb-4 flex w-full items-center justify-center gap-2 rounded-md bg-rig-500 px-5 py-2.5 text-sm font-medium text-rig-950 transition-colors hover:bg-rig-400 disabled:opacity-50"
		>
			<KeyRound size={16} />
			{passkeyBusy ? 'Waiting for passkey…' : 'Sign in with a passkey'}
		</button>
		<div class="mb-4 flex items-center gap-3 text-xs text-rig-600">
			<span class="h-px flex-1 bg-rig-800"></span>
			or use your password
			<span class="h-px flex-1 bg-rig-800"></span>
		</div>
	{/if}

	<form onsubmit={submit} class="space-y-3">
		<label class="block">
			<span class="text-sm text-rig-400">Username</span>
			<input bind:value={username} autocomplete="username" class="{field} mt-1" />
		</label>
		<label class="block">
			<span class="text-sm text-rig-400">Password</span>
			<input
				type="password"
				bind:value={password}
				autocomplete={mode === 'register' ? 'new-password' : 'current-password'}
				class="{field} mt-1"
			/>
		</label>
		<button
			type="submit"
			disabled={!valid || saving}
			class="w-full rounded-md border border-rig-700 px-5 py-2 text-sm font-medium text-rig-100 transition-colors hover:border-rig-500 disabled:opacity-40"
		>
			{saving ? 'Please wait…' : mode === 'register' ? 'Create account' : 'Sign in'}
		</button>
	</form>

	{#if canRegister}
		<p class="mt-4 text-center text-sm text-rig-400">
			{#if mode === 'login'}
				No account?
				<button class="text-leaf hover:underline" onclick={() => { mode = 'register'; error = null; }}>
					Create one
				</button>
			{:else}
				Already have an account?
				<button class="text-leaf hover:underline" onclick={() => { mode = 'login'; error = null; }}>
					Sign in
				</button>
			{/if}
		</p>
	{/if}
</div>
