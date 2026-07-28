<script lang="ts">
	import ActivityLog from '$lib/components/ActivityLog.svelte';
</script>

<div class="space-y-5">
	<div>
		<h1 class="text-2xl font-semibold">Log</h1>
		<p class="mt-1 text-sm text-rig-400">
			Everything Grow Core recorded, across every environment and grow — controls, warnings and
			system notices. For a readable story of one grow, plant or environment, see its History tab.
		</p>
	</div>
	<ActivityLog limit={25} showEnvironment showFilters />
</div>
