<script lang="ts">
	import * as Collapsible from '$lib/components/ui/collapsible/index.js';
	import { ArrowDownIcon } from '$lib/icons';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import IfPermitted from '$lib/components/if-permitted.svelte';
	import FormInput from '$lib/components/form/form-input.svelte';
	import SelectWithLabel from '$lib/components/form/select-with-label.svelte';
	import { preventDefault } from '$lib/utils/settings';
	import { m } from '$lib/paraglide/messages';
	import type { BuildFormInputsStore } from './build-form.types';

	type RegistryOption = { label: string; value: string; description?: string };

	let {
		inputs,
		provider,
		quickBuildOpen = $bindable(false),
		showAdvanced = $bindable(false),
		isPushMode = false,
		showRegistrySelection = false,
		registryOptions = [],
		repositoryOptions = [],
		fullImageReference = '',
		registryLoadError = null,
		quickDirectory = $bindable(''),
		quickRegistryId = $bindable(''),
		quickTag = $bindable(''),
		quickIsGitRepository = false,
		quickSourceLoading = false,
		quickDirectoryOptions = [],
		quickDirectoriesLoading = false,
		quickImageReference = '',
		isGitUpdating = false,
		isBuilding = false,
		onGitUpdate,
		onQuickBuild,
		onSubmit
	}: {
		inputs: BuildFormInputsStore;
		provider: 'local' | 'depot';
		quickBuildOpen?: boolean;
		showAdvanced?: boolean;
		isPushMode?: boolean;
		showRegistrySelection?: boolean;
		registryOptions?: RegistryOption[];
		repositoryOptions?: RegistryOption[];
		fullImageReference?: string;
		registryLoadError?: { message?: string } | null;
		quickDirectory?: string;
		quickRegistryId?: string;
		quickTag?: string;
		quickIsGitRepository?: boolean;
		quickSourceLoading?: boolean;
		quickDirectoryOptions?: RegistryOption[];
		quickDirectoriesLoading?: boolean;
		quickImageReference?: string;
		isGitUpdating?: boolean;
		isBuilding?: boolean;
		onGitUpdate?: () => void;
		onQuickBuild?: () => void;
		onSubmit?: () => void;
	} = $props();

	const providerIsLocal = $derived(provider === 'local');
	const providerIsDepot = $derived(provider === 'depot');
</script>

<div class="p-8">
	<form onsubmit={preventDefault(() => onSubmit?.())} class="space-y-8">
		<Collapsible.Root bind:open={quickBuildOpen} class="border-border/70 bg-muted/30 rounded-xl border">
			<Collapsible.Trigger
				id="build-quick-toggle"
				class="hover:bg-accent/50 flex w-full items-center justify-between rounded-xl px-4 py-4 text-left transition-colors"
			>
				<span class="text-sm font-semibold">{m.build_quick_title()}</span>
				<ArrowDownIcon class={quickBuildOpen ? 'size-4 rotate-180 transition-transform' : 'size-4 transition-transform'} />
			</Collapsible.Trigger>
			<Collapsible.Content>
				<div class="border-border/60 space-y-4 border-t px-4 pt-4 pb-4">
					<p class="text-muted-foreground text-xs">{m.build_quick_description()}</p>

					<div class="grid gap-4 sm:grid-cols-2">
						<SelectWithLabel
							id="build-quick-directory"
							label={m.build_quick_directory_label()}
							placeholder={quickDirectoriesLoading ? m.common_loading() : m.build_quick_directory_placeholder()}
							options={quickDirectoryOptions}
							bind:value={quickDirectory}
							disabled={quickDirectoriesLoading || isBuilding || isGitUpdating}
						/>

						<SelectWithLabel
							id="build-quick-registry"
							label={m.build_push_registry_label()}
							placeholder={m.build_push_registry_placeholder()}
							options={registryOptions}
							bind:value={quickRegistryId}
							disabled={!$inputs.push.value || isBuilding || isGitUpdating}
						/>
					</div>

					{#if quickDirectory}
						<div>
							<Label for="build-quick-tag">{m.build_push_tag_label()}</Label>
							<p class="text-muted-foreground mt-1 text-xs">
								{quickIsGitRepository ? m.build_quick_tag_git_description() : m.build_quick_tag_manual_description()}
							</p>
							<Input
								id="build-quick-tag"
								class="mt-2"
								placeholder={quickSourceLoading ? m.common_loading() : m.build_push_tag_placeholder()}
								bind:value={quickTag}
								disabled={quickSourceLoading || quickIsGitRepository || isBuilding || isGitUpdating}
							/>
						</div>
					{/if}

					{#if quickDirectoryOptions.length === 0 && !quickDirectoriesLoading}
						<p class="text-muted-foreground text-xs">{m.build_quick_directory_empty()}</p>
					{/if}

					{#if quickImageReference}
						<div class="space-y-1">
							<span class="text-muted-foreground text-xs font-medium">{m.build_push_reference_label()}</span>
							<div class="bg-background/70 rounded-md px-3 py-2">
								<code class="text-xs break-all">{quickImageReference}</code>
							</div>
						</div>
					{/if}

					<div class="flex flex-wrap justify-end gap-2">
						<IfPermitted perm="build-workspaces:manage">
							<ArcaneButton
								id="build-git-update"
								action="base"
								type="button"
								tone="outline"
								size="sm"
								customLabel={m.build_git_update()}
								onclick={() => onGitUpdate?.()}
								loading={isGitUpdating}
								disabled={!quickDirectory || !quickIsGitRepository || quickSourceLoading || isBuilding || isGitUpdating}
							/>
						</IfPermitted>
						<IfPermitted perm="images:build">
							<ArcaneButton
								id="build-quick-action"
								action="start_all"
								type="button"
								size="sm"
								customLabel={m.build_quick_action()}
								onclick={() => onQuickBuild?.()}
								loading={isBuilding}
								disabled={!quickDirectory ||
									!quickTag ||
									quickSourceLoading ||
									($inputs.push.value && !quickRegistryId) ||
									isBuilding ||
									isGitUpdating}
							/>
						</IfPermitted>
					</div>
				</div>
			</Collapsible.Content>
		</Collapsible.Root>

		<div class="border-border/60 border-t pt-7">
			<Collapsible.Root bind:open={showAdvanced} class="border-border/70 rounded-xl border">
				<Collapsible.Trigger
					class="text-muted-foreground hover:text-foreground hover:bg-accent/50 flex w-full items-center justify-between rounded-xl px-4 py-3 text-xs transition-colors"
				>
					{m.tabs_advanced()}
					<ArrowDownIcon class={showAdvanced ? 'size-4 rotate-180 transition-transform' : 'size-4 transition-transform'} />
				</Collapsible.Trigger>
				<Collapsible.Content>
					<div class="border-border/60 grid gap-6 border-t p-4">
						<div class="grid gap-4">
							{#if showRegistrySelection}
								<SelectWithLabel
									id="build-push-registry"
									label={m.build_push_registry_label()}
									placeholder={m.build_push_registry_placeholder()}
									options={registryOptions}
									bind:value={$inputs.registryId.value}
									triggerClass="w-full"
								/>
								{#if registryLoadError}
									<p class="text-destructive text-xs">{m.build_push_registry_permission()}</p>
								{:else if registryOptions.length === 0}
									<p class="text-muted-foreground text-xs">{m.build_push_registry_none()}</p>
								{/if}

								<SelectWithLabel
									id="build-push-repository"
									label={m.build_push_repository_label()}
									placeholder={m.build_push_repository_placeholder()}
									options={repositoryOptions}
									bind:value={$inputs.repositoryName.value}
									triggerClass="w-full"
								/>
								{#if $inputs.registryId.value && repositoryOptions.length === 0 && !registryLoadError}
									<p class="text-muted-foreground text-xs">{m.build_push_repository_empty()}</p>
								{/if}
							{/if}

							{#if isPushMode}
								<FormInput
									label={m.build_push_tag_label()}
									type="text"
									placeholder={m.build_push_tag_placeholder()}
									bind:input={$inputs.pushTag}
								/>

								{#if fullImageReference}
									<div class="space-y-1">
										<span class="text-muted-foreground text-xs font-medium">
											{m.build_push_reference_label()}
										</span>
										<div class="bg-muted/50 rounded-md px-3 py-2">
											<code class="text-xs break-all">{fullImageReference}</code>
										</div>
									</div>
								{/if}
							{:else}
								<FormInput
									label={m.image_tags()}
									type="text"
									placeholder={m.image_tags_placeholder()}
									description={m.image_tags_description()}
									bind:input={$inputs.tags}
								/>
							{/if}
						</div>

						<div class="border-border/60 space-y-6 border-t pt-6">
							<!-- Advanced build options -->
							<div class="grid gap-4 sm:grid-cols-2">
								<FormInput
									label={m.dockerfile()}
									type="text"
									placeholder={m.dockerfile()}
									description={m.dockerfile_description()}
									bind:input={$inputs.dockerfile}
								/>

								<FormInput
									label={m.target_label()}
									type="text"
									placeholder={m.target_placeholder()}
									description={m.target_description()}
									bind:input={$inputs.target}
								/>
							</div>

							<FormInput
								label={m.platforms_label()}
								type="text"
								placeholder={m.platforms_placeholder()}
								description={m.platforms_description()}
								bind:input={$inputs.platforms}
								warningText={providerIsLocal ? m.build_provider_warning_single_platform_local() : undefined}
							/>

							<FormInput
								label={m.build_args()}
								type="textarea"
								rows={3}
								placeholder={m.build_args_placeholder()}
								description={m.build_args_description()}
								bind:input={$inputs.buildArgs}
							/>

							<FormInput
								label={m.common_labels()}
								type="textarea"
								rows={3}
								placeholder={m.build_labels_placeholder()}
								description={m.build_labels_description()}
								bind:input={$inputs.labels}
							/>

							<FormInput
								label={m.build_cache_from_label()}
								type="textarea"
								rows={2}
								placeholder={m.build_cache_from_placeholder()}
								description={m.build_cache_from_description()}
								bind:input={$inputs.cacheFrom}
							/>

							<FormInput
								label={m.build_cache_to_label()}
								type="textarea"
								rows={2}
								placeholder={m.build_cache_to_placeholder()}
								description={m.build_cache_to_description()}
								bind:input={$inputs.cacheTo}
								disabled={providerIsLocal}
								warningText={providerIsLocal ? m.build_provider_warning_unsupported_local() : undefined}
							/>

							<div class="grid gap-4 sm:grid-cols-2">
								<FormInput
									label={m.build_network_label()}
									type="text"
									placeholder={m.build_network_placeholder()}
									description={m.build_network_description()}
									bind:input={$inputs.network}
									disabled={providerIsDepot}
									warningText={providerIsDepot ? m.build_provider_warning_unsupported_depot() : undefined}
								/>

								<FormInput
									label={m.build_isolation_label()}
									type="text"
									placeholder={m.build_isolation_placeholder()}
									description={m.build_isolation_description()}
									bind:input={$inputs.isolation}
									disabled={providerIsDepot}
									warningText={providerIsDepot ? m.build_provider_warning_unsupported_depot() : undefined}
								/>
							</div>

							<div class="grid gap-4 sm:grid-cols-2">
								<FormInput
									label={m.build_shm_size_label()}
									type="text"
									placeholder={m.build_shm_size_placeholder()}
									description={m.build_shm_size_description()}
									bind:input={$inputs.shmSize}
									disabled={providerIsDepot}
									warningText={providerIsDepot ? m.build_provider_warning_unsupported_depot() : undefined}
								/>

								<FormInput
									label={m.build_entitlements_label()}
									type="textarea"
									rows={2}
									placeholder={m.build_entitlements_placeholder()}
									description={m.build_entitlements_description()}
									bind:input={$inputs.entitlements}
									disabled={providerIsLocal}
									warningText={providerIsLocal ? m.build_provider_warning_unsupported_local() : undefined}
								/>
							</div>

							<FormInput
								label={m.build_ulimits_label()}
								type="textarea"
								rows={2}
								placeholder={m.build_ulimits_placeholder()}
								description={m.build_ulimits_description()}
								bind:input={$inputs.ulimits}
								disabled={providerIsDepot}
								warningText={providerIsDepot ? m.build_provider_warning_unsupported_depot() : undefined}
							/>

							<FormInput
								label={m.build_extra_hosts_label()}
								type="textarea"
								rows={2}
								placeholder={m.build_extra_hosts_placeholder()}
								description={m.build_extra_hosts_description()}
								bind:input={$inputs.extraHosts}
								disabled={providerIsDepot}
								warningText={providerIsDepot ? m.build_provider_warning_unsupported_depot() : undefined}
							/>

							<div class="grid gap-4 sm:grid-cols-2">
								<FormInput
									label={m.build_privileged_label()}
									type="switch"
									description={m.build_privileged_description()}
									bind:input={$inputs.privileged}
									disabled={providerIsLocal}
									warningText={providerIsLocal ? m.build_provider_warning_unsupported_local() : undefined}
								/>

								<FormInput
									label={m.build_no_cache_label()}
									type="switch"
									description={m.build_no_cache_description()}
									bind:input={$inputs.noCache}
								/>
							</div>

							<FormInput
								label={m.build_pull_base_images_label()}
								type="switch"
								description={m.build_pull_base_images_description()}
								bind:input={$inputs.pull}
							/>
						</div>
					</div>
				</Collapsible.Content>
			</Collapsible.Root>
		</div>
	</form>
</div>
