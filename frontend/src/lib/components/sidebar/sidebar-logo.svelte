<script lang="ts">
	import { cn } from '$lib/utils';
	import { m } from '$lib/paraglide/messages';

	let { isCollapsed }: { isCollapsed: boolean } = $props();
</script>

<div
	class={cn(
		'border-border/30 flex w-full items-center border-b transition-all duration-300',
		isCollapsed ? 'h-14 justify-center px-1' : 'h-14 justify-start gap-3 px-2'
	)}
>
	<img
		src="/drh-logo.png"
		alt={m.brand_name()}
		class={cn(
			'shrink-0 rounded-lg object-cover shadow-sm ring-1 ring-white/10 transition-all duration-300',
			isCollapsed ? 'size-8' : 'size-9'
		)}
		width={isCollapsed ? '32' : '36'}
		height={isCollapsed ? '32' : '36'}
	/>
	{#if !isCollapsed}
		<span class="text-sidebar-foreground truncate text-lg font-semibold tracking-wide">{m.brand_name()}</span>
	{/if}
</div>
