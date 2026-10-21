<script lang="ts">
	// An environment's History: what happened in this room or box — device and
	// setting changes, control failures, and plants moving in.
	//
	// Routine control-loop chatter is excluded by the query rather than filtered
	// after fetching: in a busy box a window of recent activity is *all* fan
	// nudges, so client-side filtering would leave the page looking empty.
	import HistoryPanel from '$lib/components/HistoryPanel.svelte';

	interface Props {
		environmentId: string;
		isAdmin?: boolean;
	}
	let { environmentId, isAdmin = false }: Props = $props();
</script>

<HistoryPanel {environmentId} canEdit={isAdmin} empty="Nothing recorded for this environment yet." />
