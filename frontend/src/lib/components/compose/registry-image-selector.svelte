<script lang="ts">
	import SelectWithLabel from '$lib/components/form/select-with-label.svelte';
	import { m } from '$lib/paraglide/messages';
	import { queryKeys } from '$lib/query/query-keys';
	import { containerRegistryService } from '$lib/services/container-registry-service';
	import { createQuery } from '@tanstack/svelte-query';

	let {
		selectedImage = $bindable(''),
		disabled = false,
		idPrefix = 'registry-image',
		onValueChange
	}: {
		selectedImage: string;
		disabled?: boolean;
		idPrefix?: string;
		onValueChange?: (value: string) => void;
	} = $props();

	const requestOptions = {
		pagination: { page: 1, limit: 100 },
		sort: { column: 'createdAt', direction: 'desc' as const }
	};
	const registriesQuery = createQuery(() => ({
		queryKey: queryKeys.containerRegistries.list(requestOptions),
		queryFn: () => containerRegistryService.getRegistries(requestOptions),
		enabled: !disabled
	}));
	let selectedRegistryId = $state('');
	let selectedRepository = $state('');
	let selectedTag = $state('');

	const enabledRegistries = $derived((registriesQuery.data?.data ?? []).filter((registry) => registry.enabled));
	const selectedRegistry = $derived(enabledRegistries.find((registry) => registry.id === selectedRegistryId));
	const registryOptions = $derived(
		enabledRegistries.map((registry) => ({
			label: registry.description?.trim() || registry.url,
			value: registry.id,
			description: registry.url
		}))
	);
	const repositoriesQuery = createQuery(() => ({
		queryKey: ['container-registries', selectedRegistryId, 'repositories'],
		queryFn: () => containerRegistryService.getRepositories(selectedRegistryId),
		enabled: !disabled && selectedRegistryId !== ''
	}));
	const tagsQuery = createQuery(() => ({
		queryKey: ['container-registries', selectedRegistryId, 'repositories', selectedRepository, 'tags'],
		queryFn: () => containerRegistryService.getTags(selectedRegistryId, selectedRepository),
		enabled: !disabled && selectedRegistryId !== '' && selectedRepository !== ''
	}));
	const repositoryOptions = $derived(
		(repositoriesQuery.data ?? []).map((repository) => ({ label: repository, value: repository }))
	);
	const tagOptions = $derived((tagsQuery.data ?? []).map((tag) => ({ label: tag, value: tag })));

	function normalizedRegistryPrefix(value: string): string {
		return value
			.trim()
			.toLowerCase()
			.replace(/^https?:\/\//, '')
			.replace(/\/+$/, '');
	}

	function handleRegistryChange(value: string) {
		selectedRegistryId = value;
		selectedRepository = '';
		selectedTag = '';
		selectedImage = '';
	}

	function handleRepositoryChange(value: string) {
		selectedRepository = value;
		selectedTag = '';
		selectedImage = '';
	}

	function handleTagChange(value: string) {
		selectedTag = value;
		selectedImage =
			selectedRegistry && selectedRepository && value
				? `${normalizedRegistryPrefix(selectedRegistry.url)}/${selectedRepository}:${value}`
				: '';
		onValueChange?.(selectedImage);
	}
</script>

<div class="grid gap-3 md:grid-cols-3">
	<SelectWithLabel
		id={`${idPrefix}-registry`}
		label={m.compose_image_registry_label()}
		placeholder={registriesQuery.isPending ? m.common_loading() : m.compose_image_registry_placeholder()}
		options={registryOptions}
		value={selectedRegistryId}
		onValueChange={handleRegistryChange}
		triggerClass="w-full"
		{disabled}
	/>
	<SelectWithLabel
		id={`${idPrefix}-repository`}
		label={m.compose_image_repository_label()}
		placeholder={repositoriesQuery.isPending ? m.common_loading() : m.compose_image_repository_placeholder()}
		options={repositoryOptions}
		value={selectedRepository}
		onValueChange={handleRepositoryChange}
		triggerClass="w-full"
		disabled={disabled || !selectedRegistryId}
	/>
	<SelectWithLabel
		id={`${idPrefix}-tag`}
		label={m.compose_image_tag_label()}
		placeholder={tagsQuery.isPending ? m.common_loading() : m.compose_image_tag_placeholder()}
		options={tagOptions}
		value={selectedTag}
		onValueChange={handleTagChange}
		triggerClass="w-full"
		disabled={disabled || !selectedRepository}
	/>
</div>
