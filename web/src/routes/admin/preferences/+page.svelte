<script lang="ts">
	import { onMount } from 'svelte';
	import { getPreferences, updatePreferences } from '$lib/api';
	import { Button } from '$lib/components/ui';

	const localeOptions: { tag: string; label: string }[] = [
		'en-US',
		'en-GB',
		'cs-CZ',
		'de-DE',
		'de-AT',
		'de-CH',
		'fr-FR',
		'es-ES',
		'it-IT',
		'nl-NL',
		'pl-PL',
		'pt-PT',
		'pt-BR',
		'sv-SE',
		'da-DK',
		'nb-NO',
		'fi-FI',
		'ja-JP',
		'ko-KR',
		'zh-CN',
		'zh-TW'
	].map((tag) => ({
		tag,
		label: (() => {
			try {
				return new Intl.DisplayNames([tag], { type: 'language' }).of(tag) ?? tag;
			} catch {
				return tag;
			}
		})()
	}));

	let timezone = $state('UTC');
	let locale = $state('en-US');
	let loading = $state(true);
	let saving = $state(false);
	let message = $state('');
	let error = $state('');
	const timezones: string[] = ['UTC', ...(typeof Intl.supportedValuesOf === 'function' ? Intl.supportedValuesOf('timeZone') : [])];

	onMount(async () => {
		try {
			const prefs = await getPreferences();
			timezone = prefs.timezone;
			locale = prefs.locale;
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to load preferences';
		} finally {
			loading = false;
		}
	});

	async function save() {
		saving = true;
		error = '';
		message = '';
		try {
			const saved = await updatePreferences({ timezone, locale });
			timezone = saved.timezone;
			locale = saved.locale;
			message = 'Preferences saved';
			window.dispatchEvent(new CustomEvent('growrig-preferences-updated', { detail: saved }));
		} catch (e) {
			error = e instanceof Error ? e.message : 'Save failed';
		} finally {
			saving = false;
		}
	}
</script>

<section class="max-w-2xl space-y-5">
	<div><h2 class="text-lg font-semibold">Instance preferences</h2><p class="mt-1 text-sm text-rig-400">Settings shared by everyone using this GrowRig instance.</p></div>
	{#if error}<div class="rounded-lg bg-danger/10 px-3 py-2 text-sm text-danger">{error}</div>{/if}
	{#if message}<div class="rounded-lg bg-leaf/10 px-3 py-2 text-sm text-leaf">{message}</div>{/if}
	<div class="rounded-xl border border-rig-800 bg-rig-900/40 p-5 space-y-5">
		<label class="block">
			<span class="text-sm font-medium text-rig-200">Timezone</span>
			<p class="mt-1 text-xs text-rig-500">Used for the instance clock and localized timestamps.</p>
			<select bind:value={timezone} disabled={loading} class="mt-3 w-full rounded-md border border-rig-700 bg-rig-950 px-3 py-2 text-sm focus:border-rig-500 focus:outline-none">
				{#if !timezones.includes(timezone)}<option value={timezone}>{timezone}</option>{/if}
				{#each timezones as zone}<option value={zone}>{zone}</option>{/each}
			</select>
		</label>
		<label class="block">
			<span class="text-sm font-medium text-rig-200">Locale</span>
			<p class="mt-1 text-xs text-rig-500">Controls date and time formatting across the web app (order, separators, 12h vs 24h).</p>
			<select bind:value={locale} disabled={loading} class="mt-3 w-full rounded-md border border-rig-700 bg-rig-950 px-3 py-2 text-sm focus:border-rig-500 focus:outline-none">
				{#if !localeOptions.some((o) => o.tag === locale)}<option value={locale}>{locale}</option>{/if}
				{#each localeOptions as opt (opt.tag)}<option value={opt.tag}>{opt.label} ({opt.tag})</option>{/each}
			</select>
		</label>
		<div class="flex justify-end"><Button onclick={save} disabled={loading || saving}>{saving ? 'Saving…' : 'Save preferences'}</Button></div>
	</div>
</section>
