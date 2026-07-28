<script lang="ts">
	// A grow's History. Everything — merging its care events, photos, stage
	// changes and activity, ordering them, hiding automation and paging — happens
	// server-side; this only names the scope.
	import type { GrowDetail } from '$lib/types';
	import HistoryPanel from '$lib/components/HistoryPanel.svelte';

	interface Props {
		grow: GrowDetail;
	}
	let { grow }: Props = $props();
</script>

<HistoryPanel
	growId={grow.id}
	empty="Nothing here yet — care actions, photos and milestones will appear as the grow progresses."
/>
