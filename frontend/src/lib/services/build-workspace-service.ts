import BaseAPIService from './api-service';
import { environmentStore } from '$lib/stores/environment.store.svelte';
import type { FileEntry, FileContentResponse } from '$lib/types/shared';
import { downloadBlob, filenameFromPath } from '$lib/utils/browser-download';

export type WorkspaceGitUpdate = {
	updated: boolean;
	branch: string;
	beforeCommit: string;
	afterCommit: string;
};

export type WorkspaceSourceInfo = {
	isGitRepository: boolean;
	revision?: string;
	suggestedTag?: string;
};

class BuildWorkspaceService extends BaseAPIService {
	async listDirectory(path: string = '/'): Promise<FileEntry[]> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const res = await this.api.get(`/environments/${envId}/builds/browse`, {
			params: { path }
		});
		return res.data.data;
	}

	async getFileContent(path: string): Promise<FileContentResponse> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const res = await this.api.get(`/environments/${envId}/builds/browse/content`, {
			params: { path }
		});
		return res.data.data;
	}

	async downloadFile(path: string): Promise<void> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const res = await this.api.get(`/environments/${envId}/builds/browse/download`, {
			params: { path },
			responseType: 'blob'
		});

		downloadBlob(res.data, filenameFromPath(path));
	}

	async uploadFile(path: string, file: File): Promise<void> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const formData = new FormData();
		formData.append('file', file);
		return this.handleResponse(
			this.api.post(`/environments/${envId}/builds/browse/upload`, formData, {
				params: { path }
			})
		);
	}

	async createDirectory(path: string): Promise<void> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(
			this.api.post(`/environments/${envId}/builds/browse/mkdir`, null, {
				params: { path }
			})
		);
	}

	async updateGitWorkspace(contextDir: string): Promise<WorkspaceGitUpdate> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(
			this.api.post(`/environments/${envId}/builds/git-update`, {
				contextDir
			})
		);
	}

	async getWorkspaceSourceInfo(contextDir: string): Promise<WorkspaceSourceInfo> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(
			this.api.get(`/environments/${envId}/builds/source-info`, {
				params: { contextDir }
			})
		);
	}

	async deleteFile(path: string): Promise<void> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(
			this.api.delete(`/environments/${envId}/builds/browse`, {
				params: { path }
			})
		);
	}
}

export const buildWorkspaceService = new BuildWorkspaceService();
