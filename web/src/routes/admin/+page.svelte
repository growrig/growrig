<script lang="ts">
	import { onMount } from 'svelte';
	import { auth } from '$lib/auth.svelte';
	import {
		getUsers,
		createUser,
		updateUser,
		deleteUser,
		getEnvironments,
		getSignupSetting,
		setSignupSetting
	} from '$lib/api';
	import type { User, Environment, EnvAccess, AccessLevel, UserRole } from '$lib/types';
	import { Switch } from '$lib/components/ui';
	import Shield from '@lucide/svelte/icons/shield';
	import UserIcon from '@lucide/svelte/icons/user';
	import Trash2 from '@lucide/svelte/icons/trash-2';
	import Pencil from '@lucide/svelte/icons/pencil';
	import Plus from '@lucide/svelte/icons/plus';

	type AccessChoice = 'none' | 'read' | 'write';

	let users = $state<User[]>([]);
	let environments = $state<Environment[]>([]);
	let signupEnabled = $state(false);
	let error = $state<string | null>(null);
	let loading = $state(true);

	// New-user form.
	let showNew = $state(false);
	let nuName = $state('');
	let nuPassword = $state('');
	let nuRole = $state<UserRole>('user');
	let nuAccess = $state<Record<string, AccessChoice>>({});
	let creating = $state(false);

	// Inline edit.
	let editingId = $state<string | null>(null);
	let edRole = $state<UserRole>('user');
	let edAccess = $state<Record<string, AccessChoice>>({});
	let edPassword = $state('');
	let savingEdit = $state(false);

	onMount(load);

	async function load() {
		loading = true;
		error = null;
		try {
			[users, environments, signupEnabled] = await Promise.all([
				getUsers(),
				getEnvironments(),
				getSignupSetting().then((s) => s.enabled)
			]);
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to load';
		} finally {
			loading = false;
		}
	}

	function accessToChoices(access: EnvAccess[]): Record<string, AccessChoice> {
		const out: Record<string, AccessChoice> = {};
		for (const g of access) out[g.environmentId] = g.access;
		return out;
	}
	function choicesToAccess(choices: Record<string, AccessChoice>): EnvAccess[] {
		const out: EnvAccess[] = [];
		for (const [environmentId, choice] of Object.entries(choices)) {
			if (choice === 'read' || choice === 'write')
				out.push({ environmentId, access: choice as AccessLevel });
		}
		return out;
	}
	function envName(id: string): string {
		return environments.find((e) => e.id === id)?.name ?? id;
	}
	function accessSummary(u: User): string {
		if (u.role === 'admin') return 'All environments';
		if (!u.access?.length) return 'No environments';
		return u.access.map((g) => `${envName(g.environmentId)} (${g.access})`).join(', ');
	}

	async function toggleSignup(next: boolean) {
		const prev = signupEnabled;
		signupEnabled = next;
		try {
			await setSignupSetting(next);
		} catch (e) {
			signupEnabled = prev;
			error = e instanceof Error ? e.message : 'Failed to update setting';
		}
	}

	function openNew() {
		showNew = true;
		nuName = '';
		nuPassword = '';
		nuRole = 'user';
		nuAccess = {};
		error = null;
	}

	async function submitNew() {
		creating = true;
		error = null;
		try {
			await createUser({
				username: nuName.trim(),
				password: nuPassword,
				role: nuRole,
				access: nuRole === 'admin' ? [] : choicesToAccess(nuAccess)
			});
			showNew = false;
			await load();
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to create user';
		} finally {
			creating = false;
		}
	}

	function openEdit(u: User) {
		editingId = u.id;
		edRole = u.role;
		edAccess = accessToChoices(u.access ?? []);
		edPassword = '';
		error = null;
	}

	async function submitEdit(u: User) {
		savingEdit = true;
		error = null;
		try {
			await updateUser(u.id, {
				role: edRole,
				access: edRole === 'admin' ? [] : choicesToAccess(edAccess),
				...(edPassword ? { password: edPassword } : {})
			});
			editingId = null;
			await load();
			if (u.id === auth.user?.id) await auth.refresh();
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to save user';
		} finally {
			savingEdit = false;
		}
	}

	async function setDisabled(u: User, disabled: boolean) {
		error = null;
		try {
			await updateUser(u.id, { disabled });
			await load();
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to update user';
		}
	}

	async function remove(u: User) {
		if (!confirm(`Delete user "${u.username}"? This cannot be undone.`)) return;
		error = null;
		try {
			await deleteUser(u.id);
			await load();
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to delete user';
		}
	}

	const field =
		'rounded-md border border-rig-700 bg-rig-950 px-3 py-1.5 text-sm focus:border-rig-500 focus:outline-none';
</script>

<!-- Per-environment access editor, shared by the create and edit forms. -->
{#snippet accessEditor(choices: Record<string, AccessChoice>, set: (id: string, v: AccessChoice) => void)}
	{#if environments.length === 0}
		<p class="text-sm text-rig-500">No environments exist yet.</p>
	{:else}
		<div class="space-y-1.5">
			{#each environments as env (env.id)}
				<div class="flex items-center justify-between gap-3 rounded-md bg-rig-950/40 px-3 py-1.5">
					<span class="text-sm">{env.name} <span class="text-xs text-rig-500">{env.kind}</span></span>
					<div class="flex gap-1">
						{#each ['none', 'read', 'write'] as const as level (level)}
							<button
								type="button"
								onclick={() => set(env.id, level)}
								class="rounded px-2 py-0.5 text-xs capitalize transition-colors {(choices[env.id] ?? 'none') === level
									? 'bg-rig-500 text-rig-950'
									: 'bg-rig-800 text-rig-300 hover:bg-rig-700'}"
							>
								{level}
							</button>
						{/each}
					</div>
				</div>
			{/each}
		</div>
	{/if}
{/snippet}

<div class="space-y-6">
	<div class="flex items-center justify-between">
		<h2 class="text-lg font-semibold">Users</h2>
		<button
			onclick={openNew}
			class="flex items-center gap-1.5 rounded-md bg-rig-500 px-4 py-1.5 text-sm font-medium text-rig-950 transition-colors hover:bg-rig-400"
		>
			<Plus size={16} /> New user
		</button>
	</div>

	{#if error}
		<div class="rounded-lg bg-danger/15 px-4 py-2 text-sm text-danger">{error}</div>
	{/if}

	<!-- Self-registration setting. -->
	<div class="flex items-center justify-between rounded-xl border border-rig-800 bg-rig-900/40 p-4">
		<div>
			<h2 class="font-medium">Allow self-registration</h2>
			<p class="text-sm text-rig-400">
				When on, anyone who can reach GrowRig can create their own account (with no
				environment access until you grant it). Off by default.
			</p>
		</div>
		<Switch checked={signupEnabled} onCheckedChange={toggleSignup} />
	</div>

	{#if showNew}
		<div class="space-y-3 rounded-xl border border-rig-700 bg-rig-900/40 p-4">
			<h2 class="font-medium">New user</h2>
			<div class="flex flex-wrap gap-3">
				<label class="flex-1">
					<span class="text-sm text-rig-400">Username</span>
					<input bind:value={nuName} autocomplete="off" class="{field} mt-1 w-full" />
				</label>
				<label class="flex-1">
					<span class="text-sm text-rig-400">Password</span>
					<input type="password" bind:value={nuPassword} autocomplete="new-password" class="{field} mt-1 w-full" />
				</label>
				<label>
					<span class="text-sm text-rig-400">Role</span>
					<select bind:value={nuRole} class="{field} mt-1 block">
						<option value="user">User</option>
						<option value="admin">Admin</option>
					</select>
				</label>
			</div>
			{#if nuRole === 'user'}
				<div>
					<span class="mb-1.5 block text-sm text-rig-400">Environment access</span>
					{@render accessEditor(nuAccess, (id, v) => (nuAccess = { ...nuAccess, [id]: v }))}
				</div>
			{/if}
			<div class="flex justify-end gap-2">
				<button onclick={() => (showNew = false)} class="rounded-md border border-rig-700 px-4 py-1.5 text-sm text-rig-300 hover:border-rig-500">Cancel</button>
				<button
					onclick={submitNew}
					disabled={creating || nuName.trim().length < 3 || nuPassword.length < 8}
					class="rounded-md bg-rig-500 px-4 py-1.5 text-sm font-medium text-rig-950 hover:bg-rig-400 disabled:opacity-40"
				>
					{creating ? 'Creating…' : 'Create user'}
				</button>
			</div>
		</div>
	{/if}

	{#if loading}
		<p class="text-sm text-rig-400">Loading…</p>
	{:else}
		<div class="overflow-hidden rounded-xl border border-rig-800">
			<table class="w-full text-sm">
				<thead class="bg-rig-900/60 text-left text-xs uppercase tracking-wide text-rig-500">
					<tr>
						<th class="px-4 py-2 font-medium">User</th>
						<th class="px-4 py-2 font-medium">Role</th>
						<th class="px-4 py-2 font-medium">Access</th>
						<th class="px-4 py-2 font-medium">Status</th>
						<th class="px-4 py-2"></th>
					</tr>
				</thead>
				<tbody>
					{#each users as u (u.id)}
						<tr class="border-t border-rig-800 align-top">
							<td class="px-4 py-3 font-medium">
								<span class="flex items-center gap-1.5">
									{#if u.role === 'admin'}<Shield size={14} class="text-leaf" />{:else}<UserIcon size={14} class="text-rig-400" />{/if}
									{u.username}
									{#if u.id === auth.user?.id}<span class="text-xs text-rig-500">(you)</span>{/if}
								</span>
							</td>
							<td class="px-4 py-3 capitalize text-rig-300">{u.role}</td>
							<td class="px-4 py-3 text-rig-400">{accessSummary(u)}</td>
							<td class="px-4 py-3">
								{#if u.disabled}
									<span class="rounded-full bg-danger/15 px-2 py-0.5 text-xs text-danger">Disabled</span>
								{:else}
									<span class="rounded-full bg-leaf/15 px-2 py-0.5 text-xs text-leaf">Active</span>
								{/if}
							</td>
							<td class="px-4 py-3">
								<div class="flex justify-end gap-1">
									<button onclick={() => openEdit(u)} title="Edit" class="rounded p-1.5 text-rig-400 hover:bg-rig-800 hover:text-rig-100"><Pencil size={15} /></button>
									<button onclick={() => setDisabled(u, !u.disabled)} class="rounded px-2 py-1 text-xs text-rig-400 hover:bg-rig-800 hover:text-rig-100">
										{u.disabled ? 'Enable' : 'Disable'}
									</button>
									<button onclick={() => remove(u)} title="Delete" class="rounded p-1.5 text-rig-400 hover:bg-danger/15 hover:text-danger"><Trash2 size={15} /></button>
								</div>
							</td>
						</tr>
						{#if editingId === u.id}
							<tr class="border-t border-rig-800 bg-rig-950/40">
								<td colspan="5" class="px-4 py-4">
									<div class="space-y-3">
										<div class="flex flex-wrap items-end gap-3">
											<label>
												<span class="text-sm text-rig-400">Role</span>
												<select bind:value={edRole} class="{field} mt-1 block">
													<option value="user">User</option>
													<option value="admin">Admin</option>
												</select>
											</label>
											<label class="flex-1">
												<span class="text-sm text-rig-400">Reset password <span class="text-rig-600">(optional)</span></span>
												<input type="password" bind:value={edPassword} autocomplete="new-password" placeholder="Leave blank to keep current" class="{field} mt-1 w-full" />
											</label>
										</div>
										{#if edRole === 'user'}
											<div>
												<span class="mb-1.5 block text-sm text-rig-400">Environment access</span>
												{@render accessEditor(edAccess, (id, v) => (edAccess = { ...edAccess, [id]: v }))}
											</div>
										{/if}
										<div class="flex justify-end gap-2">
											<button onclick={() => (editingId = null)} class="rounded-md border border-rig-700 px-4 py-1.5 text-sm text-rig-300 hover:border-rig-500">Cancel</button>
											<button
												onclick={() => submitEdit(u)}
												disabled={savingEdit || (edPassword.length > 0 && edPassword.length < 8)}
												class="rounded-md bg-rig-500 px-4 py-1.5 text-sm font-medium text-rig-950 hover:bg-rig-400 disabled:opacity-40"
											>
												{savingEdit ? 'Saving…' : 'Save changes'}
											</button>
										</div>
									</div>
								</td>
							</tr>
						{/if}
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
</div>
