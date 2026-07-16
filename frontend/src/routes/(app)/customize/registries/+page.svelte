<script lang="ts">
	import * as Dialog from '$lib/components/ui/dialog/index.js';
	import { toast } from 'svelte-sonner';
	import type { ContainerRegistry, ContainerRegistryPullUsage } from '$lib/types/docker';
	import type { ContainerRegistryEnvironmentStatus } from '$lib/types/docker';
	import type { ContainerRegistryCreateDto, ContainerRegistryUpdateDto } from '$lib/types/docker';
	import ContainerRegistryFormSheet from '$lib/components/sheets/container-registry-sheet.svelte';
	import RegistryTable from './registry-table.svelte';
	import { handleApiResultWithCallbacks } from '$lib/utils/api';
	import { tryCatch } from '$lib/utils/api';
	import { m } from '$lib/paraglide/messages';
	import { containerRegistryService } from '$lib/services/container-registry-service';
	import { queryKeys } from '$lib/query/query-keys';
	import { untrack } from 'svelte';
	import { ResourcePageLayout, type ActionButton } from '$lib/layouts/index.js';
	import { createQuery } from '@tanstack/svelte-query';
	import { hasPermission } from '$lib/utils/auth';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { ArcaneButton } from '$lib/components/arcane-button';
	import * as Select from '$lib/components/ui/select';
	import { Spinner } from '$lib/components/ui/spinner';
	import { CopyButton } from '$lib/components/ui/copy-button';

	let { data } = $props();

	let registries = $state(untrack(() => data.registries));
	let selectedIds = $state<string[]>([]);
	let isRegistryDialogOpen = $state(false);
	let isInfoDialogOpen = $state(false);
	let registryToEdit = $state<ContainerRegistry | null>(null);
	let registryToPullTest = $state<ContainerRegistry | null>(null);
	let pullTestRepository = $state('');
	let pullTestTag = $state('');
	let isPullTestOpen = $state(false);
	let isPullTesting = $state(false);
	let pullTestRepositories = $state<string[]>([]);
	let pullTestTags = $state<string[]>([]);
	let isImageBrowserOpen = $state(false);
	let isImageBrowserLoading = $state(false);
	let browserRegistries = $state<ContainerRegistry[]>([]);
	let browserRegistryId = $state('');
	let browserRepositories = $state<string[]>([]);
	let browserRepository = $state('');
	let browserTags = $state<string[]>([]);
	let selectedEnvironmentId = $derived(environmentStore.selected?.id ?? '0');
	let requestOptions = $state(untrack(() => data.registryRequestOptions));
	const pullUsageQuery = createQuery(() => ({
		queryKey: queryKeys.containerRegistries.pullUsage(),
		queryFn: () => containerRegistryService.getPullUsage(),
		initialData: data.pullUsage ?? undefined
	}));
	const pullUsageByRegistry = $derived.by<Record<string, ContainerRegistryPullUsage>>(() => {
		const entries = pullUsageQuery.data?.registries ?? [];
		return Object.fromEntries(entries.map((usage) => [usage.registryId, usage]));
	});
	const environmentStatusQuery = createQuery(() => ({
		queryKey: ['container-registries', 'environment-status', selectedEnvironmentId],
		queryFn: () => containerRegistryService.getEnvironmentStatuses(selectedEnvironmentId),
		enabled: selectedEnvironmentId !== '0'
	}));
	const environmentStatuses = $derived.by<Record<string, ContainerRegistryEnvironmentStatus>>(() =>
		Object.fromEntries((environmentStatusQuery.data ?? []).map((status) => [status.registryId, status]))
	);

	let isLoading = $state({
		create: false,
		edit: false,
		refresh: false,
		sync: false
	});

	async function syncCurrentEnvironment() {
		if (selectedEnvironmentId === '0') return;
		isLoading.sync = true;
		try {
			await containerRegistryService.syncEnvironment(selectedEnvironmentId);
			await environmentStatusQuery.refetch();
			toast.success(m.registries_sync_success());
		} catch (error) {
			toast.error(error instanceof Error ? error.message : m.registries_sync_failed());
		} finally {
			isLoading.sync = false;
		}
	}

	async function openPullTest(registry: ContainerRegistry) {
		registryToPullTest = registry;
		pullTestRepository = registry.repositoryNames?.[0] ?? '';
		pullTestTag = '';
		pullTestRepositories = [];
		pullTestTags = [];
		isPullTestOpen = true;
		if (registry.registryType === 'ecr') {
			try {
				pullTestRepositories = await containerRegistryService.getRepositories(registry.id);
				if (!pullTestRepository) pullTestRepository = pullTestRepositories[0] ?? '';
				if (pullTestRepository) pullTestTags = await containerRegistryService.getTags(registry.id, pullTestRepository);
			} catch (error) {
				toast.error(error instanceof Error ? error.message : m.registries_catalog_load_failed());
			}
		}
	}

	async function refreshPullTestTags() {
		if (!registryToPullTest || !pullTestRepository.trim() || registryToPullTest.registryType !== 'ecr') return;
		try {
			pullTestTags = await containerRegistryService.getTags(registryToPullTest.id, pullTestRepository.trim());
		} catch (error) {
			toast.error(error instanceof Error ? error.message : m.registries_catalog_load_failed());
		}
	}

	async function testRemotePull() {
		if (!registryToPullTest || !pullTestRepository.trim() || !pullTestTag.trim()) return;
		isPullTesting = true;
		try {
			const result = await containerRegistryService.testEnvironmentPull(
				registryToPullTest.id,
				selectedEnvironmentId,
				pullTestRepository.trim(),
				pullTestTag.trim()
			);
			await environmentStatusQuery.refetch();
			toast.success(m.registries_pull_test_success({ image: result.imageReference }));
			isPullTestOpen = false;
		} catch (error) {
			toast.error(error instanceof Error ? error.message : m.registries_pull_test_failed());
		} finally {
			isPullTesting = false;
		}
	}

	function registryImageReference(registry: ContainerRegistry, repository: string, tag: string) {
		const host =
			registry.url
				.trim()
				.replace(/^https?:\/\//, '')
				.replace(/\/$/, '') || 'docker.io';
		return `${host}/${repository}:${tag}`;
	}

	async function loadBrowserTags(registryId: string, repository: string) {
		browserTags = [];
		if (!registryId || !repository) return;
		isImageBrowserLoading = true;
		try {
			browserTags = await containerRegistryService.getTags(registryId, repository);
		} catch (error) {
			toast.error(error instanceof Error ? error.message : m.registries_catalog_load_failed());
		} finally {
			isImageBrowserLoading = false;
		}
	}

	async function loadBrowserRepositories(registryId: string) {
		browserRegistryId = registryId;
		browserRepositories = [];
		browserRepository = '';
		browserTags = [];
		if (!registryId) return;
		isImageBrowserLoading = true;
		try {
			browserRepositories = await containerRegistryService.getRepositories(registryId);
			browserRepository = browserRepositories[0] ?? '';
			if (browserRepository) await loadBrowserTags(registryId, browserRepository);
		} catch (error) {
			toast.error(error instanceof Error ? error.message : m.registries_catalog_load_failed());
		} finally {
			isImageBrowserLoading = false;
		}
	}

	async function openImageBrowser() {
		isImageBrowserOpen = true;
		isImageBrowserLoading = true;
		try {
			const result = await containerRegistryService.getRegistries({
				pagination: { page: 1, limit: 100 },
				sort: { column: 'url', direction: 'asc' },
				filters: { enabled: true }
			});
			browserRegistries = result.data;
			await loadBrowserRepositories(browserRegistries[0]?.id ?? '');
		} catch (error) {
			toast.error(error instanceof Error ? error.message : m.registries_catalog_load_failed());
		} finally {
			isImageBrowserLoading = false;
		}
	}

	async function refreshRegistries() {
		isLoading.refresh = true;
		handleApiResultWithCallbacks({
			result: await tryCatch(containerRegistryService.getRegistries(requestOptions)),
			message: m.common_refresh_failed({ resource: m.registries_title() }),
			setLoadingState: (value) => (isLoading.refresh = value),
			onSuccess: async (newRegistries) => {
				registries = newRegistries;
				await pullUsageQuery.refetch();
				toast.success(m.registries_refreshed());
			}
		});
	}

	function openCreateRegistryDialog() {
		registryToEdit = null;
		isRegistryDialogOpen = true;
	}

	function openEditRegistryDialog(registry: ContainerRegistry) {
		registryToEdit = registry;
		isRegistryDialogOpen = true;
	}

	async function handleRegistryDialogSubmit(detail: {
		registry: ContainerRegistryCreateDto | ContainerRegistryUpdateDto;
		isEditMode: boolean;
	}) {
		const { registry, isEditMode } = detail;
		const loadingKey = isEditMode ? 'edit' : 'create';
		isLoading[loadingKey] = true;

		try {
			if (isEditMode && registryToEdit?.id) {
				await containerRegistryService.updateRegistry(registryToEdit.id, registry as ContainerRegistryUpdateDto);
				toast.success(m.common_update_success({ resource: m.resource_registry() }));
			} else {
				await containerRegistryService.createRegistry(registry as ContainerRegistryCreateDto);
				toast.success(m.common_create_success({ resource: m.resource_registry() }));
			}

			[registries] = await Promise.all([containerRegistryService.getRegistries(requestOptions), pullUsageQuery.refetch()]);
			isRegistryDialogOpen = false;
		} catch (error) {
			console.error('Error saving registry:', error);
			toast.error(error instanceof Error ? error.message : m.registries_save_failed());
		} finally {
			isLoading[loadingKey] = false;
		}
	}

	const canCreateRegistry = $derived(hasPermission('registries:create'));

	const actionButtons: ActionButton[] = $derived.by(() => {
		const buttons: ActionButton[] = [
			{
				id: 'info',
				action: 'inspect',
				label: m.registries_info_title(),
				onclick: () => (isInfoDialogOpen = true)
			}
		];
		if (canCreateRegistry) {
			buttons.push({
				id: 'create',
				action: 'create',
				label: m.common_add_button({ resource: m.resource_registry_cap() }),
				onclick: openCreateRegistryDialog
			});
		}
		buttons.push({
			id: 'browse-images',
			action: 'inspect',
			label: m.registries_browse_images(),
			onclick: openImageBrowser
		});
		if (selectedEnvironmentId !== '0' && hasPermission('registries:update')) {
			buttons.push({
				id: 'sync-environment',
				action: 'restart',
				label: m.registries_sync_current_environment(),
				onclick: syncCurrentEnvironment,
				loading: isLoading.sync,
				disabled: isLoading.sync
			});
		}
		buttons.push({
			id: 'refresh',
			action: 'restart',
			label: m.common_refresh(),
			onclick: refreshRegistries,
			loading: isLoading.refresh,
			disabled: isLoading.refresh
		});
		return buttons;
	});
</script>

<ResourcePageLayout title={m.registries_title()} subtitle={m.registries_subtitle()} {actionButtons}>
	{#snippet mainContent()}
		<RegistryTable
			bind:registries
			bind:selectedIds
			bind:requestOptions
			{pullUsageByRegistry}
			{environmentStatuses}
			{selectedEnvironmentId}
			onEditRegistry={openEditRegistryDialog}
			onTestRemotePull={openPullTest}
		/>
	{/snippet}

	{#snippet additionalContent()}
		<ContainerRegistryFormSheet
			bind:open={isRegistryDialogOpen}
			bind:registryToEdit
			onSubmit={handleRegistryDialogSubmit}
			isLoading={isLoading.create || isLoading.edit}
		/>

		<Dialog.Root bind:open={isInfoDialogOpen}>
			<Dialog.Content class="max-w-2xl">
				<Dialog.Header>
					<Dialog.Title>{m.registries_info_title()}</Dialog.Title>
					<Dialog.Description>{m.registries_info_description()}</Dialog.Description>
				</Dialog.Header>
				<div class="grid grid-cols-1 gap-6 py-4 md:grid-cols-2">
					<div class="space-y-3">
						<h4 class="text-sm font-medium">{m.registries_popular_public_title()}</h4>
						<div class="space-y-2 text-sm">
							<div class="flex justify-between">
								<span class="text-muted-foreground">{m.registry_docker_hub()}</span>
								<code class="bg-muted rounded px-2 py-1 text-xs">{m.registry_docker_hub_url()}</code>
							</div>
							<div class="flex justify-between">
								<span class="text-muted-foreground">{m.registry_github_container_registry()}</span>
								<code class="bg-muted rounded px-2 py-1 text-xs">{m.registry_github_url()}</code>
							</div>
							<div class="flex justify-between">
								<span class="text-muted-foreground">{m.registry_google_container_registry()}</span>
								<code class="bg-muted rounded px-2 py-1 text-xs">{m.registry_google_url()}</code>
							</div>
							<div class="flex justify-between">
								<span class="text-muted-foreground">{m.registry_quay_io()}</span>
								<code class="bg-muted rounded px-2 py-1 text-xs">{m.registry_quay_url()}</code>
							</div>
						</div>
					</div>
					<div class="space-y-3">
						<h4 class="text-sm font-medium">{m.registries_auth_notes_title()}</h4>
						<div class="text-muted-foreground space-y-1 text-sm">
							<p>• {m.registries_auth_notes_bullet_docker_hub()}</p>
							<p>• {m.registries_auth_notes_bullet_github()}</p>
							<p>• {m.registries_auth_notes_bullet_anonymous()}</p>
							<p>• {m.registries_auth_notes_bullet_encrypted()}</p>
						</div>
					</div>
				</div>
			</Dialog.Content>
		</Dialog.Root>

		<Dialog.Root bind:open={isPullTestOpen}>
			<Dialog.Content class="max-w-lg">
				<Dialog.Header>
					<Dialog.Title>{m.registries_pull_test_title()}</Dialog.Title>
					<Dialog.Description>{m.registries_pull_test_description()}</Dialog.Description>
				</Dialog.Header>
				<div class="space-y-4 py-4">
					<div class="space-y-2">
						<Label for="pull-test-repository">{m.registries_pull_test_repository()}</Label><Input
							id="pull-test-repository"
							list="registry-repositories"
							bind:value={pullTestRepository}
							onblur={refreshPullTestTags}
						/>
						<datalist id="registry-repositories">
							{#each pullTestRepositories as repository}<option value={repository}></option>{/each}
						</datalist>
					</div>
					<div class="space-y-2">
						<Label for="pull-test-tag">{m.registries_pull_test_tag()}</Label><Input
							id="pull-test-tag"
							list="registry-tags"
							bind:value={pullTestTag}
						/>
						<datalist id="registry-tags">
							{#each pullTestTags as tag}<option value={tag}></option>{/each}
						</datalist>
					</div>
				</div>
				<Dialog.Footer>
					<ArcaneButton action="cancel" onclick={() => (isPullTestOpen = false)} disabled={isPullTesting}
						>{m.common_cancel()}</ArcaneButton
					>
					<ArcaneButton
						action="pull"
						onclick={testRemotePull}
						loading={isPullTesting}
						disabled={!pullTestRepository.trim() || !pullTestTag.trim()}>{m.registries_pull_test_submit()}</ArcaneButton
					>
				</Dialog.Footer>
			</Dialog.Content>
		</Dialog.Root>

		<Dialog.Root bind:open={isImageBrowserOpen}>
			<Dialog.Content class="max-w-3xl">
				<Dialog.Header>
					<Dialog.Title>{m.registries_browse_images()}</Dialog.Title>
					<Dialog.Description>{m.registries_browse_images_description()}</Dialog.Description>
				</Dialog.Header>
				<div class="grid gap-4 py-4 sm:grid-cols-2">
					<div class="space-y-2">
						<Label for="image-browser-registry">{m.registries_browse_registry()}</Label>
						<Select.Root
							type="single"
							value={browserRegistryId}
							onValueChange={(value) => value && loadBrowserRepositories(value)}
						>
							<Select.Trigger id="image-browser-registry" class="w-full">
								<span class="truncate">
									{browserRegistries.find((registry) => registry.id === browserRegistryId)?.url ?? m.common_select_placeholder()}
								</span>
							</Select.Trigger>
							<Select.Content style="width: var(--bits-select-anchor-width);">
								{#each browserRegistries as registry (registry.id)}
									<Select.Item value={registry.id}>{registry.url || 'docker.io'}</Select.Item>
								{/each}
							</Select.Content>
						</Select.Root>
					</div>
					<div class="space-y-2">
						<Label for="image-browser-repository">{m.registries_browse_repository()}</Label>
						<Select.Root
							type="single"
							value={browserRepository}
							disabled={!browserRegistryId || browserRepositories.length === 0}
							onValueChange={(value) => {
								if (value) {
									browserRepository = value;
									void loadBrowserTags(browserRegistryId, value);
								}
							}}
						>
							<Select.Trigger id="image-browser-repository" class="w-full">
								<span class="truncate">{browserRepository || m.common_select_placeholder()}</span>
							</Select.Trigger>
							<Select.Content style="width: var(--bits-select-anchor-width);">
								{#each browserRepositories as repository (repository)}
									<Select.Item value={repository}>{repository}</Select.Item>
								{/each}
							</Select.Content>
						</Select.Root>
					</div>
				</div>

				<div class="max-h-96 overflow-y-auto rounded-md border">
					{#if isImageBrowserLoading}
						<div class="flex items-center justify-center gap-2 py-12 text-sm">
							<Spinner class="size-4" />
							<span>{m.common_loading()}</span>
						</div>
					{:else if browserRegistries.length === 0}
						<p class="text-muted-foreground p-6 text-center text-sm">{m.registries_browse_no_registries()}</p>
					{:else if browserRepositories.length === 0}
						<p class="text-muted-foreground p-6 text-center text-sm">{m.registries_browse_no_repositories()}</p>
					{:else if browserTags.length === 0}
						<p class="text-muted-foreground p-6 text-center text-sm">{m.registries_browse_no_images()}</p>
					{:else}
						{#each browserTags as tag (tag)}
							{@const registry = browserRegistries.find((item) => item.id === browserRegistryId)}
							{#if registry}
								{@const imageReference = registryImageReference(registry, browserRepository, tag)}
								<div class="flex items-center gap-3 border-b px-4 py-3 last:border-b-0">
									<code class="min-w-0 flex-1 truncate text-xs" title={imageReference}>{imageReference}</code>
									<CopyButton text={imageReference} variant="outline" tabindex={0} />
								</div>
							{/if}
						{/each}
					{/if}
				</div>
			</Dialog.Content>
		</Dialog.Root>
	{/snippet}
</ResourcePageLayout>
