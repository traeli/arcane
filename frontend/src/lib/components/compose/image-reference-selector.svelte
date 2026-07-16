<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import RegistryImageSelector from '$lib/components/compose/registry-image-selector.svelte';
	import SelectWithLabel from '$lib/components/form/select-with-label.svelte';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { m } from '$lib/paraglide/messages';
	import { queryKeys } from '$lib/query/query-keys';
	import { imageService } from '$lib/services/image-service';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { applyImageToComposeService, getComposeServiceNames, imageReferenceServiceName } from '$lib/utils/compose-flow';
	import { createQuery } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';

	let {
		composeContent = $bindable(''),
		disabled = false
	}: {
		composeContent: string;
		disabled?: boolean;
	} = $props();

	const environmentId = $derived(environmentStore.selected?.id || '0');
	const requestOptions = {
		pagination: { page: 1, limit: 100 },
		sort: { column: 'createdAt', direction: 'desc' as const }
	};

	const imagesQuery = createQuery(() => ({
		queryKey: queryKeys.images.list(environmentId, requestOptions),
		queryFn: () => imageService.getImagesForEnvironment(environmentId, requestOptions),
		enabled: !disabled
	}));

	const buildsQuery = createQuery(() => ({
		queryKey: queryKeys.images.buildsList(environmentId, requestOptions),
		queryFn: () => imageService.getImageBuilds(requestOptions),
		enabled: !disabled
	}));
	let imageSource = $state<'local' | 'registry'>('local');
	let selectedImage = $state('');
	let selectedService = $state('__new__');
	let newServiceName = $state('');

	const sourceOptions = $derived([
		{ label: m.compose_image_source_local(), value: 'local' },
		{ label: m.compose_image_source_registry(), value: 'registry' }
	]);
	const imageOptions = $derived.by(() => {
		const options: { label: string; value: string; description?: string; created: number }[] = [];
		const seen = new Set<string>();
		for (const build of buildsQuery.data?.data ?? []) {
			if (build.status !== 'success') continue;
			for (const tag of build.tags ?? []) {
				if (!tag || seen.has(tag)) continue;
				seen.add(tag);
				options.push({
					label: tag,
					value: tag,
					description: m.compose_image_source_recent_build(),
					created: new Date(build.createdAt).getTime()
				});
			}
		}
		for (const image of imagesQuery.data?.data ?? []) {
			for (const tag of image.repoTags ?? []) {
				if (!tag || tag.startsWith('<none>') || seen.has(tag)) continue;
				seen.add(tag);
				options.push({ label: tag, value: tag, description: m.compose_image_source_local(), created: image.created * 1000 });
			}
		}
		return options.sort((a, b) => b.created - a.created);
	});

	const serviceNames = $derived(getComposeServiceNames(composeContent));
	const serviceOptions = $derived([
		{ label: m.compose_image_create_service(), value: '__new__' },
		...serviceNames.map((name) => ({ label: name, value: name }))
	]);

	function handleImageChange(value: string) {
		selectedImage = value;
		if (selectedService === '__new__') newServiceName = imageReferenceServiceName(value);
	}

	function handleSourceChange(value: string) {
		imageSource = value === 'registry' ? 'registry' : 'local';
		selectedImage = '';
	}

	function applyImage() {
		const serviceName = selectedService === '__new__' ? newServiceName.trim() : selectedService;
		if (!selectedImage || !serviceName) {
			toast.error(m.compose_image_selection_required());
			return;
		}
		try {
			composeContent = applyImageToComposeService(composeContent, serviceName, selectedImage);
			selectedService = serviceName;
			toast.success(m.compose_image_applied({ service: serviceName }));
		} catch {
			toast.error(m.compose_image_invalid_yaml());
		}
	}
</script>

<div class="border-border bg-card/60 grid shrink-0 gap-3 rounded-lg border p-4 md:grid-cols-2 xl:grid-cols-6 xl:items-end">
	<div class="min-w-0">
		<SelectWithLabel
			id="compose-image-source"
			label={m.compose_image_source_label()}
			options={sourceOptions}
			value={imageSource}
			onValueChange={handleSourceChange}
			triggerClass="w-full"
			{disabled}
		/>
	</div>
	{#if imageSource === 'registry'}
		<div class="min-w-0 md:col-span-2 xl:col-span-3">
			<RegistryImageSelector bind:selectedImage {disabled} idPrefix="compose-image" onValueChange={handleImageChange} />
		</div>
	{:else}
		<div class="min-w-0 xl:col-span-3">
			<SelectWithLabel
				id="compose-image-reference"
				label={m.compose_image_selector_label()}
				description={m.compose_image_selector_description()}
				placeholder={imagesQuery.isPending || buildsQuery.isPending ? m.common_loading() : m.compose_image_selector_placeholder()}
				options={imageOptions}
				value={selectedImage}
				onValueChange={handleImageChange}
				triggerClass="w-full"
				{disabled}
			/>
		</div>
	{/if}
	<div class="min-w-0">
		<SelectWithLabel
			id="compose-image-service"
			label={m.compose_image_target_service()}
			options={serviceOptions}
			bind:value={selectedService}
			triggerClass="w-full"
			{disabled}
		/>
	</div>
	{#if selectedService === '__new__'}
		<div class="min-w-0 space-y-2">
			<Label for="compose-image-new-service">{m.compose_image_service_name()}</Label>
			<Input id="compose-image-new-service" bind:value={newServiceName} {disabled} />
		</div>
	{/if}
	<ArcaneButton
		action="base"
		tone="outline-primary"
		customLabel={m.compose_image_apply()}
		onclick={applyImage}
		disabled={disabled || !selectedImage || (selectedService === '__new__' && !newServiceName.trim())}
		class="shrink-0"
	/>
</div>
