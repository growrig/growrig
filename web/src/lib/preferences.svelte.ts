// Instance preferences (timezone, locale) shared across the web app.
import { getPreferences, type Preferences } from './api';

class PreferencesState {
	timezone = $state('UTC');
	locale = $state('en-US');

	apply(p: Preferences) {
		this.timezone = p.timezone;
		this.locale = p.locale;
	}

	async load() {
		try {
			this.apply(await getPreferences());
		} catch {
			/* unauthenticated or offline — keep defaults */
		}
	}
}

export const preferences = new PreferencesState();

export function onPreferencesUpdated(handler: (p: Preferences) => void): () => void {
	const listener = (event: Event) => handler((event as CustomEvent<Preferences>).detail);
	window.addEventListener('growrig-preferences-updated', listener);
	return () => window.removeEventListener('growrig-preferences-updated', listener);
}
