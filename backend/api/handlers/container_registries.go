package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/mapper"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
	"go.getarcane.app/sys/crypto"
)

// ContainerRegistryHandler handles container registry management endpoints.
type ContainerRegistryHandler struct {
	registryService    *services.ContainerRegistryService
	environmentService *services.EnvironmentService
}

// ============================================================================
// Input/Output Types
// ============================================================================

// ContainerRegistryPaginatedResponse is the paginated response for container registries.
type ContainerRegistryPaginatedResponse struct {
	Success    bool                                  `json:"success"`
	Data       []containerregistry.ContainerRegistry `json:"data"`
	Pagination base.PaginationResponse               `json:"pagination"`
}

type ListContainerRegistriesInput struct {
	Search string `query:"search" doc:"Search query"`
	Sort   string `query:"sort" doc:"Column to sort by"`
	Order  string `query:"order" default:"asc" doc:"Sort direction"`
	Start  int    `query:"start" default:"0" doc:"Start index"`
	Limit  int    `query:"limit" default:"20" doc:"Items per page"`
}

type ListContainerRegistriesOutput struct {
	Body ContainerRegistryPaginatedResponse
}

type CreateContainerRegistryInput struct {
	Body models.CreateContainerRegistryRequest
}

type CreateContainerRegistryOutput struct {
	Body base.ApiResponse[containerregistry.ContainerRegistry]
}

type GetContainerRegistryInput struct {
	ID string `path:"id" doc:"Registry ID"`
}

type GetContainerRegistryOutput struct {
	Body base.ApiResponse[containerregistry.ContainerRegistry]
}

type UpdateContainerRegistryInput struct {
	ID   string `path:"id" doc:"Registry ID"`
	Body models.UpdateContainerRegistryRequest
}

type UpdateContainerRegistryOutput struct {
	Body base.ApiResponse[containerregistry.ContainerRegistry]
}

type DeleteContainerRegistryInput struct {
	ID string `path:"id" doc:"Registry ID"`
}

type DeleteContainerRegistryOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type TestContainerRegistryInput struct {
	ID string `path:"id" doc:"Registry ID"`
}

type TestContainerRegistryOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type GetContainerRegistryPullUsageOutput struct {
	Body base.ApiResponse[containerregistry.PullUsageResponse]
}

type SyncContainerRegistriesInput struct {
	Body containerregistry.SyncRequest
}

type SyncContainerRegistriesOutput struct {
	Body base.ApiResponse[containerregistry.RegistrySyncResult]
}

type RegistryEnvironmentStatusInput struct {
	EnvironmentID string `path:"environmentId"`
}
type RegistryEnvironmentStatusOutput struct {
	Body base.ApiResponse[[]containerregistry.EnvironmentStatus]
}
type TestRegistryPullInput struct {
	ID   string `path:"id"`
	Body containerregistry.PullTestRequest
}
type TestRegistryPullOutput struct {
	Body base.ApiResponse[containerregistry.PullTestResult]
}
type TestRemoteRegistryPullInput struct {
	EnvironmentID string `path:"environmentId"`
	ID            string `path:"id"`
	Body          containerregistry.PullTestRequest
}
type TestRemoteRegistryPullOutput struct {
	Body base.ApiResponse[containerregistry.PullTestResult]
}
type RegistryCatalogInput struct {
	ID string `path:"id"`
}
type RegistryTagsInput struct {
	ID         string `path:"id"`
	Repository string `query:"repository"`
}
type RegistryRepositoriesOutput struct {
	Body base.ApiResponse[containerregistry.RepositoryCatalog]
}
type RegistryTagsOutput struct {
	Body base.ApiResponse[containerregistry.TagCatalog]
}

// ============================================================================
// Registration
// ============================================================================

// RegisterContainerRegistries registers all container registry endpoints.
func RegisterContainerRegistries(api huma.API, registryService *services.ContainerRegistryService, environmentService *services.EnvironmentService) {
	h := &ContainerRegistryHandler{
		registryService:    registryService,
		environmentService: environmentService,
	}

	huma.Register(api, huma.Operation{
		OperationID: "listContainerRegistryRepositories", Method: http.MethodGet,
		Path: "/container-registries/{id}/repositories", Summary: "List ECR repositories",
		Tags: []string{"Container Registries"}, Security: defaultOperationSecurityInternal(),
		Middlewares: humamw.RequirePermission(api, authz.PermRegistriesRead),
	}, h.ListRepositories)

	huma.Register(api, huma.Operation{
		OperationID: "listContainerRegistryTags", Method: http.MethodGet,
		Path: "/container-registries/{id}/tags", Summary: "List ECR image tags",
		Tags: []string{"Container Registries"}, Security: defaultOperationSecurityInternal(),
		Middlewares: humamw.RequirePermission(api, authz.PermRegistriesRead),
	}, h.ListTags)

	huma.Register(api, huma.Operation{
		OperationID: "getContainerRegistryEnvironmentStatuses", Method: http.MethodGet,
		Path: "/container-registries/environments/{environmentId}/status", Summary: "Get registry synchronization status for an environment",
		Tags: []string{"Container Registries"}, Security: defaultOperationSecurityInternal(),
		Middlewares: humamw.RequirePermission(api, authz.PermRegistriesRead),
	}, h.GetEnvironmentStatuses)

	huma.Register(api, huma.Operation{
		OperationID: "syncContainerRegistriesToEnvironment", Method: http.MethodPost,
		Path: "/container-registries/environments/{environmentId}/sync", Summary: "Sync registries to one environment",
		Tags: []string{"Container Registries"}, Security: defaultOperationSecurityInternal(),
		Middlewares: humamw.RequirePermission(api, authz.PermRegistriesUpdate),
	}, h.SyncEnvironment)

	huma.Register(api, huma.Operation{
		OperationID: "testContainerRegistryPullOnEnvironment", Method: http.MethodPost,
		Path: "/container-registries/{id}/environments/{environmentId}/test-pull", Summary: "Test pull access on one environment",
		Tags: []string{"Container Registries"}, Security: defaultOperationSecurityInternal(),
		Middlewares: humamw.RequirePermission(api, authz.PermRegistriesTest),
	}, h.TestRemotePull)

	huma.Register(api, huma.Operation{
		OperationID: "testLocalContainerRegistryPull", Method: http.MethodPost,
		Path: "/container-registries/{id}/test-pull", Summary: "Test local pull access for an image manifest",
		Tags: []string{"Container Registries"}, Security: defaultOperationSecurityInternal(),
		Middlewares: humamw.RequirePermission(api, authz.PermRegistriesTest),
	}, h.TestLocalPull)

	huma.Register(api, huma.Operation{
		OperationID: "listContainerRegistries",
		Method:      "GET",
		Path:        "/container-registries",
		Summary:     "List container registries",
		Description: "Get a paginated list of container registries",
		Tags:        []string{"Container Registries"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermRegistriesList),
	}, h.ListRegistries)

	huma.Register(api, huma.Operation{
		OperationID: "createContainerRegistry",
		Method:      "POST",
		Path:        "/container-registries",
		Summary:     "Create a container registry",
		Description: "Create a new container registry",
		Tags:        []string{"Container Registries"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermRegistriesCreate),
	}, h.CreateRegistry)

	huma.Register(api, huma.Operation{
		OperationID: "syncContainerRegistries",
		Method:      "POST",
		Path:        "/container-registries/sync",
		Summary:     "Sync container registries",
		Description: "Sync container registries from a remote source",
		Tags:        []string{"Container Registries"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermRegistriesUpdate),
	}, h.SyncRegistries)

	huma.Register(api, huma.Operation{
		OperationID: "getContainerRegistryPullUsage",
		Method:      "GET",
		Path:        "/container-registries/pull-usage",
		Summary:     "Get container registry pull usage",
		Description: "Get configured registry pull usage and rate limit visibility",
		Tags:        []string{"Container Registries"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermRegistriesRead),
	}, h.GetPullUsage)

	huma.Register(api, huma.Operation{
		OperationID: "getContainerRegistry",
		Method:      "GET",
		Path:        "/container-registries/{id}",
		Summary:     "Get a container registry",
		Description: "Get a container registry by ID",
		Tags:        []string{"Container Registries"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermRegistriesRead),
	}, h.GetRegistry)

	huma.Register(api, huma.Operation{
		OperationID: "updateContainerRegistry",
		Method:      "PUT",
		Path:        "/container-registries/{id}",
		Summary:     "Update a container registry",
		Description: "Update an existing container registry",
		Tags:        []string{"Container Registries"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermRegistriesUpdate),
	}, h.UpdateRegistry)

	huma.Register(api, huma.Operation{
		OperationID: "deleteContainerRegistry",
		Method:      "DELETE",
		Path:        "/container-registries/{id}",
		Summary:     "Delete a container registry",
		Description: "Delete a container registry by ID",
		Tags:        []string{"Container Registries"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermRegistriesDelete),
	}, h.DeleteRegistry)

	huma.Register(api, huma.Operation{
		OperationID: "testContainerRegistry",
		Method:      "POST",
		Path:        "/container-registries/{id}/test",
		Summary:     "Test a container registry",
		Description: "Test connectivity and authentication to a container registry",
		Tags:        []string{"Container Registries"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermRegistriesTest),
	}, h.TestRegistry)
}

// ============================================================================
// Handler Methods
// ============================================================================

// ListRegistries returns a paginated list of container registries.
func (h *ContainerRegistryHandler) ListRegistries(ctx context.Context, input *ListContainerRegistriesInput) (*ListContainerRegistriesOutput, error) {
	if h.registryService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	params := buildPaginationParamsInternal(input.Start, input.Limit, input.Sort, input.Order, input.Search)

	registries, paginationResp, err := h.registryService.GetRegistriesPaginated(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.RegistryListError{Err: err}).Error())
	}

	return &ListContainerRegistriesOutput{
		Body: ContainerRegistryPaginatedResponse{
			Success:    true,
			Data:       registries,
			Pagination: toPaginationResponseInternal(paginationResp),
		},
	}, nil
}

// GetPullUsage returns pull usage visibility for configured registries.
func (h *ContainerRegistryHandler) GetPullUsage(ctx context.Context, input *struct{}) (*GetContainerRegistryPullUsageOutput, error) {
	if h.registryService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	usage, err := h.registryService.GetRegistryPullUsage(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.RegistryRetrievalError{Err: err}).Error())
	}

	return &GetContainerRegistryPullUsageOutput{
		Body: base.ApiResponse[containerregistry.PullUsageResponse]{
			Success: true,
			Data:    usage,
		},
	}, nil
}

// CreateRegistry creates a new container registry.
func (h *ContainerRegistryHandler) CreateRegistry(ctx context.Context, input *CreateContainerRegistryInput) (*CreateContainerRegistryOutput, error) {
	if h.registryService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	reg, err := h.registryService.CreateRegistry(ctx, input.Body)
	if err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.RegistryCreationError{Err: err}).Error())
	}

	h.triggerRemoteRegistrySync(ctx, "registry creation")

	out, mapErr := mapper.MapOne[*models.ContainerRegistry, containerregistry.ContainerRegistry](reg)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError((&common.RegistryMappingError{Err: mapErr}).Error())
	}

	return &CreateContainerRegistryOutput{
		Body: base.ApiResponse[containerregistry.ContainerRegistry]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// GetRegistry returns a container registry by ID.
func (h *ContainerRegistryHandler) GetRegistry(ctx context.Context, input *GetContainerRegistryInput) (*GetContainerRegistryOutput, error) {
	if h.registryService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	reg, err := h.registryService.GetRegistryByID(ctx, input.ID)
	if err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.RegistryRetrievalError{Err: err}).Error())
	}

	out, mapErr := mapper.MapOne[*models.ContainerRegistry, containerregistry.ContainerRegistry](reg)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError((&common.RegistryMappingError{Err: mapErr}).Error())
	}

	return &GetContainerRegistryOutput{
		Body: base.ApiResponse[containerregistry.ContainerRegistry]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// UpdateRegistry updates a container registry.
func (h *ContainerRegistryHandler) UpdateRegistry(ctx context.Context, input *UpdateContainerRegistryInput) (*UpdateContainerRegistryOutput, error) {
	if h.registryService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	reg, err := h.registryService.UpdateRegistry(ctx, input.ID, input.Body)
	if err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.RegistryUpdateError{Err: err}).Error())
	}

	h.triggerRemoteRegistrySync(ctx, "registry update")

	out, mapErr := mapper.MapOne[*models.ContainerRegistry, containerregistry.ContainerRegistry](reg)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError((&common.RegistryMappingError{Err: mapErr}).Error())
	}

	return &UpdateContainerRegistryOutput{
		Body: base.ApiResponse[containerregistry.ContainerRegistry]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// DeleteRegistry deletes a container registry.
func (h *ContainerRegistryHandler) DeleteRegistry(ctx context.Context, input *DeleteContainerRegistryInput) (*DeleteContainerRegistryOutput, error) {
	if h.registryService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := h.registryService.DeleteRegistry(ctx, input.ID); err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.RegistryDeletionError{Err: err}).Error())
	}

	h.triggerRemoteRegistrySync(ctx, "registry deletion")

	return &DeleteContainerRegistryOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Container registry deleted successfully",
			},
		},
	}, nil
}

// TestRegistry tests connectivity to a container registry.
func (h *ContainerRegistryHandler) TestRegistry(ctx context.Context, input *TestContainerRegistryInput) (*TestContainerRegistryOutput, error) {
	if h.registryService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	reg, err := h.registryService.GetRegistryByID(ctx, input.ID)
	if err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.RegistryRetrievalError{Err: err}).Error())
	}

	// ECR registries use a different auth flow: generate a temporary token via AWS API.
	if reg.RegistryType == "ecr" {
		if err := h.registryService.TestECRRegistry(ctx, reg); err != nil {
			return nil, huma.Error400BadRequest((&common.RegistryTestError{Err: err}).Error())
		}
		return &TestContainerRegistryOutput{
			Body: base.ApiResponse[base.MessageResponse]{
				Success: true,
				Data: base.MessageResponse{
					Message: "ECR authentication succeeded",
				},
			},
		}, nil
	}

	decryptedToken, err := crypto.Decrypt(reg.Token)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.TokenDecryptionError{Err: err}).Error())
	}

	if err := h.registryService.TestRegistry(ctx, reg.URL, reg.Username, decryptedToken); err != nil {
		return nil, huma.Error400BadRequest((&common.RegistryTestError{Err: err}).Error())
	}

	msg := "Authentication succeeded"
	if strings.TrimSpace(reg.Username) == "" && strings.TrimSpace(decryptedToken) == "" {
		msg = "Registry saved (no credentials to test)"
	}

	return &TestContainerRegistryOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: msg,
			},
		},
	}, nil
}

// SyncRegistries syncs container registries from a remote source.
func (h *ContainerRegistryHandler) SyncRegistries(ctx context.Context, input *SyncContainerRegistriesInput) (*SyncContainerRegistriesOutput, error) {
	if h.registryService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := h.registryService.SyncRegistries(ctx, input.Body.Registries); err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.RegistrySyncError{Err: err}).Error())
	}

	return &SyncContainerRegistriesOutput{
		Body: base.ApiResponse[containerregistry.RegistrySyncResult]{
			Success: true,
			Data:    containerregistry.RegistrySyncResult{Message: "Registries synced successfully", AppliedVersion: input.Body.Version},
		},
	}, nil
}

func (h *ContainerRegistryHandler) GetEnvironmentStatuses(ctx context.Context, input *RegistryEnvironmentStatusInput) (*RegistryEnvironmentStatusOutput, error) {
	rows, err := h.environmentService.GetRegistryEnvironmentStatuses(ctx, input.EnvironmentID)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	out := make([]containerregistry.EnvironmentStatus, 0, len(rows))
	for i := range rows {
		out = append(out, containerregistry.EnvironmentStatus{
			RegistryID: rows[i].RegistryID, EnvironmentID: rows[i].EnvironmentID, DesiredVersion: rows[i].DesiredVersion,
			AppliedVersion: rows[i].AppliedVersion, SyncStatus: rows[i].SyncStatus, LastSyncAt: rows[i].LastSyncAt,
			LastSyncError: rows[i].LastSyncError, PullTestStatus: rows[i].PullTestStatus,
			LastPullTestAt: rows[i].LastPullTestAt, LastPullTestError: rows[i].LastPullTestError,
		})
	}
	return &RegistryEnvironmentStatusOutput{Body: base.ApiResponse[[]containerregistry.EnvironmentStatus]{Success: true, Data: out}}, nil
}

func (h *ContainerRegistryHandler) SyncEnvironment(ctx context.Context, input *RegistryEnvironmentStatusInput) (*SyncContainerRegistriesOutput, error) {
	if err := h.environmentService.SyncRegistriesToEnvironment(ctx, input.EnvironmentID); err != nil {
		return nil, huma.Error502BadGateway(err.Error())
	}
	return &SyncContainerRegistriesOutput{Body: base.ApiResponse[containerregistry.RegistrySyncResult]{Success: true, Data: containerregistry.RegistrySyncResult{Message: "Registries synced successfully"}}}, nil
}

func (h *ContainerRegistryHandler) TestLocalPull(ctx context.Context, input *TestRegistryPullInput) (*TestRegistryPullOutput, error) {
	result, err := h.registryService.TestPullAccess(ctx, input.ID, input.Body.Repository, input.Body.Tag)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	return &TestRegistryPullOutput{Body: base.ApiResponse[containerregistry.PullTestResult]{Success: true, Data: result}}, nil
}

func (h *ContainerRegistryHandler) TestRemotePull(ctx context.Context, input *TestRemoteRegistryPullInput) (*TestRemoteRegistryPullOutput, error) {
	result, err := h.environmentService.TestRegistryPullAccess(ctx, input.EnvironmentID, input.ID, input.Body)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	return &TestRemoteRegistryPullOutput{Body: base.ApiResponse[containerregistry.PullTestResult]{Success: true, Data: result}}, nil
}

func (h *ContainerRegistryHandler) ListRepositories(ctx context.Context, input *RegistryCatalogInput) (*RegistryRepositoriesOutput, error) {
	values, err := h.registryService.ListECRRepositories(ctx, input.ID)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	return &RegistryRepositoriesOutput{Body: base.ApiResponse[containerregistry.RepositoryCatalog]{Success: true, Data: containerregistry.RepositoryCatalog{Repositories: values}}}, nil
}

func (h *ContainerRegistryHandler) ListTags(ctx context.Context, input *RegistryTagsInput) (*RegistryTagsOutput, error) {
	values, err := h.registryService.ListECRTags(ctx, input.ID, strings.TrimSpace(input.Repository))
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	return &RegistryTagsOutput{Body: base.ApiResponse[containerregistry.TagCatalog]{Success: true, Data: containerregistry.TagCatalog{Repository: input.Repository, Tags: values}}}, nil
}

// ============================================================================
// Helper Methods
// ============================================================================

func (h *ContainerRegistryHandler) triggerRemoteRegistrySync(ctx context.Context, reason string) {
	if h.environmentService == nil {
		return
	}

	detachedCtx := context.WithoutCancel(ctx)

	go func(syncCtx context.Context, syncReason string) {
		if err := h.environmentService.SyncRegistriesToRemoteEnvironments(syncCtx); err != nil {
			slog.WarnContext(syncCtx, "Failed to fan out registry sync to remote environments", "reason", syncReason, "error", err.Error())
		}
	}(detachedCtx, reason)
}
