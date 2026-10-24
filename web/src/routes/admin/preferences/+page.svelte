<script lang="ts">
	import { errMsg } from '$lib/errors';
	import { onMount } from 'svelte';
	import { getPreferences, updatePreferences } from '$lib/api';
	import { Button, Select } from '$lib/components/ui';

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
			error = errMsg(e, 'Failed to load preferences');
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
			error = errMsg(e, 'Save failed');
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
			<p class="mt-1 text-xs text-rig-500">Used for light schedules, the instance clock, and localized timestamps.</p>
			<Select
				class="mt-3"
				bind:value={timezone}
				disabled={loading}
				items={(timezones.includes(timezone) ? timezones : [timezone, ...timezones]).map((zone) => ({
					value: zone,
					label: zone
				}))}
			/>
		</label>
		<label class="block">
			<span class="text-sm font-medium text-rig-200">Locale</span>
			<p class="mt-1 text-xs text-rig-500">Controls date and time formatting across the web app (order, separators, 12h vs 24h).</p>
			<Select
				class="mt-3"
				bind:value={locale}
				disabled={loading}
				items={[
					...(localeOptions.some((o) => o.tag === locale) ? [] : [{ value: locale, label: locale }]),
					...localeOptions.map((opt) => ({ value: opt.tag, label: `${opt.label} (${opt.tag})` }))
				]}
			/>
		</label>
		<div class="flex justify-end"><Button onclick={save} disabled={loading || saving}>{saving ? 'Saving…' : 'Save preferences'}</Button></div>
	</div>
</section>
