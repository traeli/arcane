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
	{/snippet}
</ResourcePageLayout>
