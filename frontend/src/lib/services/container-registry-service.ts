import BaseAPIService from './api-service';
import type {
	ContainerRegistryCreateDto,
	ContainerRegistryPullUsageResponse,
	ContainerRegistryUpdateDto
} from '$lib/types/docker';
import type { ContainerRegistry, ContainerRegistryEnvironmentStatus, ContainerRegistryPullTestResult } from '$lib/types/docker';
import type { Paginated, SearchPaginationSortRequest } from '$lib/types/shared';
import { transformPaginationParams } from '$lib/utils/tables';

class ContainerRegistryService extends BaseAPIService {
	async getRegistries(options?: SearchPaginationSortRequest): Promise<Paginated<ContainerRegistry>> {
		const params = transformPaginationParams(options);
		const res = await this.api.get('/container-registries', { params });
		return res.data;
	}

	async getRegistry(id: string): Promise<ContainerRegistry> {
		return this.handleResponse(this.api.get(`/container-registries/${id}`));
	}

	async getPullUsage(): Promise<ContainerRegistryPullUsageResponse> {
		return this.handleResponse(this.api.get('/container-registries/pull-usage'));
	}

	async createRegistry(registry: ContainerRegistryCreateDto): Promise<ContainerRegistry> {
		return this.handleResponse(this.api.post(`/container-registries`, registry));
	}

	async updateRegistry(id: string, registry: ContainerRegistryUpdateDto): Promise<ContainerRegistry> {
		return this.handleResponse(this.api.put(`/container-registries/${id}`, registry));
	}

	async deleteRegistry(id: string): Promise<void> {
		return this.handleResponse(this.api.delete(`/container-registries/${id}`));
	}

	async testRegistry(id: string): Promise<any> {
		return this.handleResponse(this.api.post(`/container-registries/${id}/test`));
	}

	async getEnvironmentStatuses(environmentId: string): Promise<ContainerRegistryEnvironmentStatus[]> {
		return this.handleResponse(this.api.get(`/container-registries/environments/${environmentId}/status`));
	}

	async syncEnvironment(environmentId: string): Promise<void> {
		return this.handleResponse(this.api.post(`/container-registries/environments/${environmentId}/sync`));
	}

	async testEnvironmentPull(
		id: string,
		environmentId: string,
		repository: string,
		tag: string
	): Promise<ContainerRegistryPullTestResult> {
		return this.handleResponse(
			this.api.post(`/container-registries/${id}/environments/${environmentId}/test-pull`, {
				repository,
				tag
			})
		);
	}

	async getRepositories(id: string): Promise<string[]> {
		const result = await this.handleResponse<{ repositories: string[] }>(
			this.api.get(`/container-registries/${id}/repositories`)
		);
		return result.repositories;
	}

	async getTags(id: string, repository: string): Promise<string[]> {
		const result = await this.handleResponse<{ tags: string[] }>(
			this.api.get(`/container-registries/${id}/tags`, { params: { repository } })
		);
		return result.tags;
	}
}

export const containerRegistryService = new ContainerRegistryService();
