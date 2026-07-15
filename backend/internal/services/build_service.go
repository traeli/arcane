package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	ref "github.com/distribution/reference"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	buildgit "github.com/getarcaneapp/arcane/backend/v2/pkg/gitutil"
	utilsregistry "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/registryauth"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	imagetypes "github.com/getarcaneapp/arcane/types/v2/image"
	buildapi "go.getarcane.app/builds/api"
	contextsource "go.getarcane.app/builds/pkg/utils/contextsource"
	buildtypes "go.getarcane.app/builds/types"
	"gorm.io/gorm"
)

type BuildService struct {
	db              *database.DB
	settings        *SettingsService
	dockerService   *DockerClientService
	registryService *ContainerRegistryService
	gitRepository   *GitRepositoryService
	eventService    *EventService
	builder         buildtypes.Builder
	gitProbeFn      func(context.Context, string, buildgit.AuthConfig) error
	gitCloneFn      func(context.Context, string, string, buildgit.AuthConfig) (string, error)
	gitCleanupFn    func(string) error
	workspaceMu     sync.Mutex
	busyWorkspaces  map[string]struct{}
}

type SourceUpdateMode string

const (
	SourceUpdateNone       SourceUpdateMode = "none"
	SourceUpdateGitPull    SourceUpdateMode = "git-pull"
	SourceUpdateQuickBuild SourceUpdateMode = "quick-build"
)

type SourceBuildOptions struct {
	Mode           SourceUpdateMode
	RegistryID     string
	RepositoryName string
}

type WorkspaceGitUpdate struct {
	Updated      bool   `json:"updated"`
	Branch       string `json:"branch"`
	BeforeCommit string `json:"beforeCommit"`
	AfterCommit  string `json:"afterCommit"`
}

type WorkspaceSourceInfo struct {
	IsGitRepository bool   `json:"isGitRepository"`
	Revision        string `json:"revision,omitempty"`
	SuggestedTag    string `json:"suggestedTag,omitempty"`
}

const buildHistoryOutputLimitBytes = 2 * 1024 * 1024

func NewBuildService(
	db *database.DB,
	settings *SettingsService,
	dockerService *DockerClientService,
	registryService *ContainerRegistryService,
	gitRepository *GitRepositoryService,
	eventService *EventService,
) *BuildService {
	svc := &BuildService{
		db:              db,
		settings:        settings,
		dockerService:   dockerService,
		registryService: registryService,
		gitRepository:   gitRepository,
		eventService:    eventService,
		busyWorkspaces:  make(map[string]struct{}),
	}
	var registryAuthProvider buildtypes.RegistryAuthProvider
	if registryService != nil {
		registryAuthProvider = registryService
	}

	// ContainerRegistryService already implements buildtypes.RegistryAuthProvider,
	// so the builder consumes it directly instead of through forwarding methods on BuildService.
	svc.builder = buildapi.NewService(buildapi.Config{
		SettingsProvider:     svc,
		DockerClientProvider: dockerService,
		RegistryAuthProvider: registryAuthProvider,
	})

	return svc
}

func (s *BuildService) BuildSettings() buildtypes.BuildSettings {
	if s.settings == nil {
		return buildtypes.BuildSettings{}
	}
	settings := s.settings.GetSettingsConfig()
	return buildtypes.BuildSettings{
		DepotProjectId:   settings.DepotProjectId.Value,
		DepotToken:       settings.DepotToken.Value,
		BuildProvider:    settings.BuildProvider.Value,
		BuildTimeoutSecs: settings.BuildTimeout.AsInt(),
	}
}

func (s *BuildService) BuildImage(ctx context.Context, environmentID string, req buildtypes.BuildRequest, progressWriter io.Writer, serviceName string, user *models.User) (*buildtypes.BuildResult, error) {
	return s.buildImageInternal(ctx, environmentID, req, SourceBuildOptions{}, progressWriter, serviceName, user)
}

func (s *BuildService) BuildImageWithSourceUpdate(ctx context.Context, environmentID string, req buildtypes.BuildRequest, options SourceBuildOptions, progressWriter io.Writer, serviceName string, user *models.User) (*buildtypes.BuildResult, error) {
	return s.buildImageInternal(ctx, environmentID, req, options, progressWriter, serviceName, user)
}

func (s *BuildService) buildImageInternal(ctx context.Context, environmentID string, req buildtypes.BuildRequest, sourceOptions SourceBuildOptions, progressWriter io.Writer, serviceName string, user *models.User) (*buildtypes.BuildResult, error) {
	if s.builder == nil {
		return nil, errors.New("build service not available")
	}

	logCapture := buildapi.NewLogCapture(buildHistoryOutputLimitBytes)
	writer := io.Writer(logCapture)
	if progressWriter != nil {
		writer = io.MultiWriter(progressWriter, logCapture)
	}

	buildRecordID := ""
	if s.db != nil && strings.TrimSpace(environmentID) != "" {
		if record, err := s.createBuildRecord(ctx, environmentID, req, sourceOptions.Mode, user); err != nil {
			slog.WarnContext(ctx, "failed to create build history record", "error", err)
		} else {
			buildRecordID = record.ID
		}
	}

	startedAt := time.Now()
	cleanupResolvedContext := func() error { return nil }
	var (
		result *buildtypes.BuildResult
		err    error
	)

	resolvedReq := req
	var sourceMetadata *buildgit.WorktreeUpdate
	var unlockWorkspace func()
	if sourceOptions.Mode == SourceUpdateGitPull || sourceOptions.Mode == SourceUpdateQuickBuild {
		resolvedReq, sourceMetadata, unlockWorkspace, err = s.prepareWorkspaceSourceBuildInternal(ctx, req, sourceOptions, writer, serviceName)
	} else if sourceOptions.Mode == SourceUpdateNone || sourceOptions.Mode == "" {
		unlockWorkspace, err = s.lockOrdinaryLocalBuildInternal(ctx, req)
	} else {
		err = fmt.Errorf("unsupported source update mode %q", sourceOptions.Mode)
	}
	if unlockWorkspace != nil {
		defer unlockWorkspace()
	}

	if err != nil {
		// The history entry remains useful for source validation/update failures.
	} else if nextReq, cleanupFn, resolveErr := s.resolveBuildRequestInternal(ctx, resolvedReq, writer, serviceName); resolveErr != nil {
		err = resolveErr
	} else {
		cleanupResolvedContext = cleanupFn
		resolvedReq = nextReq
		if buildRecordID != "" && (sourceOptions.Mode == SourceUpdateGitPull || sourceOptions.Mode == SourceUpdateQuickBuild) {
			if updateErr := s.updateBuildSourceInternal(ctx, buildRecordID, resolvedReq, sourceOptions.Mode, sourceMetadata); updateErr != nil {
				err = updateErr
			} else {
				result, err = s.builder.BuildImage(ctx, resolvedReq, writer, serviceName)
			}
		} else {
			result, err = s.builder.BuildImage(ctx, resolvedReq, writer, serviceName)
		}
	}

	completedAt := time.Now()
	if cleanupErr := cleanupResolvedContext(); cleanupErr != nil {
		slog.WarnContext(ctx, "failed to cleanup temporary git build context", "error", cleanupErr)
	}

	if s.db != nil && buildRecordID != "" {
		output := logCapture.String()
		var outputPtr *string
		if output != "" {
			outputPtr = &output
		}

		provider := s.effectiveBuildProviderInternal(req.Provider)
		var digest *string
		if result != nil {
			if result.Provider != "" {
				provider = result.Provider
			}
			if result.Digest != "" {
				digest = &result.Digest
			}
		}

		status := models.ImageBuildStatusSuccess
		var errMsg *string
		if err != nil {
			status = models.ImageBuildStatusFailed
			errMsg = new(err.Error())
		}

		if updateErr := s.completeBuildRecord(ctx, buildRecordID, status, outputPtr, logCapture.Truncated(), errMsg, digest, provider, completedAt, new(completedAt.Sub(startedAt).Milliseconds())); updateErr != nil {
			slog.WarnContext(ctx, "failed to update build history record", "error", updateErr)
		}
	}

	if err != nil {
		s.logBuildFailureEventInternal(ctx, environmentID, req, serviceName, buildRecordID, err, user)
	}

	return result, err
}

func (s *BuildService) logBuildFailureEventInternal(ctx context.Context, environmentID string, req buildtypes.BuildRequest, serviceName, buildRecordID string, err error, user *models.User) {
	if s.eventService == nil || err == nil {
		return
	}

	resourceName := firstNonEmptyStringInternal(req.Tags...)
	if resourceName == "" {
		resourceName = strings.TrimSpace(serviceName)
	}
	if resourceName == "" {
		resourceName = sanitizeBuildContextForEventInternal(req.ContextDir)
	}

	userID := ""
	username := ""
	if user != nil {
		userID = user.ID
		username = user.Username
	}

	metadata := models.JSON{
		"action":     "build",
		"provider":   s.effectiveBuildProviderInternal(req.Provider),
		"contextDir": sanitizeBuildContextForEventInternal(req.ContextDir),
		"dockerfile": req.Dockerfile,
		"tags":       append([]string(nil), req.Tags...),
	}
	if service := strings.TrimSpace(serviceName); service != "" {
		metadata["serviceName"] = service
	}
	if buildRecordID != "" {
		metadata["buildRecordId"] = buildRecordID
	}

	s.eventService.LogErrorEvent(ctx, models.EventTypeImageError, "image", "", resourceName, userID, username, environmentID, err, metadata)
}

func sanitizeBuildContextForEventInternal(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	base, fragment, hasFragment := strings.Cut(trimmed, "#")
	parsed, err := url.Parse(base)
	if err != nil {
		if strings.Contains(base, "@") {
			return "[unparseable URL]"
		}
		return trimmed
	}
	if parsed.User == nil {
		return trimmed
	}

	parsed.User = url.User("redacted")
	sanitized := parsed.String()
	if hasFragment {
		sanitized += "#" + fragment
	}
	return sanitized
}

func (s *BuildService) effectiveBuildProviderInternal(provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider != "" {
		return provider
	}
	if s.settings != nil {
		provider = strings.ToLower(strings.TrimSpace(s.settings.GetSettingsConfig().BuildProvider.Value))
	}
	if provider == "" {
		return "local"
	}
	return provider
}

func firstNonEmptyStringInternal(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func (s *BuildService) prepareWorkspaceSourceBuildInternal(ctx context.Context, req buildtypes.BuildRequest, options SourceBuildOptions, progressWriter io.Writer, serviceName string) (buildtypes.BuildRequest, *buildgit.WorktreeUpdate, func(), error) {
	noop := func() {}
	if options.Mode != SourceUpdateGitPull && options.Mode != SourceUpdateQuickBuild {
		return req, nil, noop, fmt.Errorf("unsupported source update mode %q", options.Mode)
	}
	if s.effectiveBuildProviderInternal(req.Provider) != "local" {
		return req, nil, noop, errors.New("workspace source builds are only supported by the local build provider")
	}
	if s.settings == nil || s.registryService == nil {
		return req, nil, noop, errors.New("workspace source build dependencies are not available")
	}

	contextPath, err := s.resolveWorkspaceDirectoryInternal(ctx, req.ContextDir, options.Mode == SourceUpdateQuickBuild)
	if err != nil {
		return req, nil, noop, err
	}

	if !s.tryLockWorkspaceInternal(contextPath) {
		return req, nil, noop, errors.New("this source directory is already being built")
	}
	unlock := func() { s.unlockWorkspaceInternal(contextPath) }

	repositoryName := strings.TrimSpace(options.RepositoryName)
	if options.Mode == SourceUpdateQuickBuild {
		repositoryName = strings.ToLower(filepath.Base(contextPath))
	}
	if _, err := ref.ParseNormalizedNamed(repositoryName + ":latest"); err != nil || strings.Contains(repositoryName, "/") {
		unlock()
		return req, nil, noop, errors.New("build directory name cannot be used as an image repository name")
	}

	var registryPrefix string
	if req.Push {
		registryID := strings.TrimSpace(options.RegistryID)
		if registryID == "" {
			unlock()
			return req, nil, noop, errors.New("an enabled target registry is required when push is enabled")
		}
		registry, registryErr := s.registryService.GetRegistryByID(ctx, registryID)
		if registryErr != nil {
			unlock()
			return req, nil, noop, fmt.Errorf("failed to load target registry: %w", registryErr)
		}
		if !registry.Enabled {
			unlock()
			return req, nil, noop, errors.New("target registry is disabled")
		}
		if options.Mode == SourceUpdateGitPull && !slices.Contains([]string(registry.RepositoryNames), repositoryName) {
			unlock()
			return req, nil, noop, errors.New("target repository is not configured for this registry")
		}
		registryPrefix = utilsregistry.NormalizeRegistryURL(registry.URL)
	}
	if options.Mode == SourceUpdateQuickBuild {
		dockerfilePath := filepath.Join(contextPath, "Dockerfile")
		info, statErr := os.Stat(dockerfilePath)
		if statErr != nil || !info.Mode().IsRegular() {
			unlock()
			return req, nil, noop, errors.New("selected build directory does not contain a Dockerfile")
		}
		if s.gitRepository == nil || s.gitRepository.gitClient == nil {
			unlock()
			return req, nil, noop, errors.New("local Git metadata reader is not available")
		}
		tag := strings.TrimSpace(firstNonEmptyStringInternal(req.Tags...))
		isRepository, repositoryErr := s.gitRepository.gitClient.IsRepository(ctx, contextPath)
		if repositoryErr != nil {
			unlock()
			return req, nil, noop, repositoryErr
		}
		var source *buildgit.WorktreeUpdate
		if isRepository {
			writeBuildProgressStatusInternal(progressWriter, serviceName, "reading local Git commit")
			revision, revisionErr := s.gitRepository.gitClient.GetCurrentCommit(ctx, contextPath)
			if revisionErr != nil {
				unlock()
				return req, nil, noop, fmt.Errorf("failed to read local Git commit: %w", revisionErr)
			}
			if len(revision) < 12 {
				unlock()
				return req, nil, noop, errors.New("local Git commit is invalid")
			}
			tag = strings.ToLower(revision[:12])
			source = &buildgit.WorktreeUpdate{Revision: revision}
		} else if tag == "" {
			unlock()
			return req, nil, noop, errors.New("an image tag is required when the selected build directory is not a Git repository")
		}
		imageReference := repositoryName + ":" + tag
		if req.Push {
			imageReference = registryPrefix + "/" + imageReference
		}
		if _, err := ref.ParseNormalizedNamed(imageReference); err != nil {
			unlock()
			return req, nil, noop, fmt.Errorf("generated image reference is invalid: %w", err)
		}
		req.ContextDir = filepath.Dir(contextPath)
		req.Dockerfile = filepath.ToSlash(filepath.Join(filepath.Base(contextPath), "Dockerfile"))
		req.Provider = "local"
		req.Tags = []string{imageReference}
		return req, source, unlock, nil
	}

	if s.gitRepository == nil || s.gitRepository.gitClient == nil {
		unlock()
		return req, nil, noop, errors.New("git source build dependencies are not available")
	}

	writeBuildProgressStatusInternal(progressWriter, serviceName, "checking Git workspace")
	isRepository, err := s.gitRepository.gitClient.IsRepository(ctx, contextPath)
	if err != nil {
		unlock()
		return req, nil, noop, err
	}
	if !isRepository {
		writeBuildProgressStatusInternal(progressWriter, serviceName, "no Git repository found; continuing without source update")
		imageReference := registryPrefix + "/" + repositoryName + ":latest"
		if _, err := ref.ParseNormalizedNamed(imageReference); err != nil {
			unlock()
			return req, nil, noop, fmt.Errorf("generated image reference is invalid: %w", err)
		}
		req.ContextDir = contextPath
		req.Tags = []string{imageReference}
		req.Push = true
		req.Load = false
		return req, nil, unlock, nil
	}
	authConfig, authErr := s.resolveWorkspaceGitAuthInternal(ctx, contextPath)
	if authErr != nil {
		unlock()
		return req, nil, noop, authErr
	}
	update, err := s.gitRepository.gitClient.UpdateWorktreeFastForward(ctx, contextPath, authConfig)
	if err != nil {
		unlock()
		return req, nil, noop, err
	}
	if len(update.Revision) < 12 {
		unlock()
		return req, nil, noop, errors.New("updated Git revision is invalid")
	}
	status := "source is up to date"
	if update.Updated {
		status = "source updated"
	}
	writeBuildProgressStatusInternal(progressWriter, serviceName, status)
	writeBuildProgressStatusInternal(progressWriter, serviceName, "resolved revision "+update.Revision[:12])

	tag := "sha-" + strings.ToLower(update.Revision[:12])
	imageReference := repositoryName + ":" + tag
	if req.Push {
		imageReference = registryPrefix + "/" + imageReference
	}
	if _, err := ref.ParseNormalizedNamed(imageReference); err != nil {
		unlock()
		return req, nil, noop, fmt.Errorf("generated image reference is invalid: %w", err)
	}
	req.ContextDir = contextPath
	req.Tags = []string{imageReference}
	return req, update, unlock, nil
}

func (s *BuildService) InspectWorkspaceSource(ctx context.Context, contextDir string) (*WorkspaceSourceInfo, error) {
	if s.gitRepository == nil || s.gitRepository.gitClient == nil {
		return nil, errors.New("local Git metadata reader is not available")
	}
	contextPath, err := s.resolveWorkspaceDirectoryInternal(ctx, contextDir, true)
	if err != nil {
		return nil, err
	}
	isRepository, err := s.gitRepository.gitClient.IsRepository(ctx, contextPath)
	if err != nil || !isRepository {
		return &WorkspaceSourceInfo{IsGitRepository: false}, err
	}
	revision, err := s.gitRepository.gitClient.GetCurrentCommit(ctx, contextPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read local Git commit: %w", err)
	}
	if len(revision) < 12 {
		return nil, errors.New("local Git commit is invalid")
	}
	return &WorkspaceSourceInfo{IsGitRepository: true, Revision: revision, SuggestedTag: strings.ToLower(revision[:12])}, nil
}

func (s *BuildService) UpdateWorkspaceGit(ctx context.Context, contextDir string) (*WorkspaceGitUpdate, error) {
	if s.gitRepository == nil || s.gitRepository.gitClient == nil {
		return nil, errors.New("git repository service is not available")
	}
	contextPath, err := s.resolveWorkspaceDirectoryInternal(ctx, contextDir, true)
	if err != nil {
		return nil, err
	}
	if !s.tryLockWorkspaceInternal(contextPath) {
		return nil, errors.New("this source directory is already being updated or built")
	}
	defer s.unlockWorkspaceInternal(contextPath)

	isRepository, err := s.gitRepository.gitClient.IsRepository(ctx, contextPath)
	if err != nil {
		return nil, err
	}
	if !isRepository {
		return nil, errors.New("selected build directory is not a Git repository")
	}
	before, err := s.gitRepository.gitClient.GetCurrentCommit(ctx, contextPath)
	if err != nil {
		return nil, err
	}
	authConfig, err := s.resolveWorkspaceGitAuthInternal(ctx, contextPath)
	if err != nil {
		return nil, err
	}
	update, err := s.gitRepository.gitClient.UpdateWorktreeFastForward(ctx, contextPath, authConfig)
	if err != nil {
		return nil, err
	}
	return &WorkspaceGitUpdate{
		Updated:      update.Updated,
		Branch:       update.Branch,
		BeforeCommit: before,
		AfterCommit:  update.Revision,
	}, nil
}

func (s *BuildService) resolveWorkspaceGitAuthInternal(ctx context.Context, contextPath string) (buildgit.AuthConfig, error) {
	remoteURL, err := s.gitRepository.gitClient.GetWorktreeRemoteURL(ctx, contextPath)
	if err != nil {
		return buildgit.AuthConfig{}, err
	}
	authConfig, _, err := s.resolveGitBuildAuthInternal(ctx, remoteURL)
	return authConfig, err
}

func (s *BuildService) resolveWorkspaceDirectoryInternal(ctx context.Context, contextDir string, requireDirectChild bool) (string, error) {
	if s.settings == nil {
		return "", errors.New("settings service is not available")
	}
	root, err := filepath.Abs(s.settings.GetStringSetting(ctx, "buildsDirectory", "/builds"))
	if err != nil {
		return "", fmt.Errorf("failed to resolve builds directory: %w", err)
	}
	root, err = filepath.EvalSymlinks(filepath.Clean(root))
	if err != nil {
		return "", fmt.Errorf("failed to resolve builds directory: %w", err)
	}
	resolved, err := filepath.Abs(filepath.Clean(contextDir))
	if err != nil {
		return "", fmt.Errorf("failed to resolve build context: %w", err)
	}
	resolved, err = filepath.EvalSymlinks(resolved)
	if err != nil {
		return "", fmt.Errorf("failed to resolve build context: %w", err)
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("build context must be a source directory inside the configured builds directory")
	}
	if requireDirectChild && (filepath.Dir(rel) != "." || filepath.Base(rel) == ".") {
		return "", errors.New("build context must be a direct child of the configured builds directory")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", errors.New("build context is not a directory")
	}
	return resolved, nil
}

func (s *BuildService) tryLockWorkspaceInternal(path string) bool {
	s.workspaceMu.Lock()
	defer s.workspaceMu.Unlock()
	if _, exists := s.busyWorkspaces[path]; exists {
		return false
	}
	s.busyWorkspaces[path] = struct{}{}
	return true
}

func (s *BuildService) lockOrdinaryLocalBuildInternal(ctx context.Context, req buildtypes.BuildRequest) (func(), error) {
	noop := func() {}
	if s.effectiveBuildProviderInternal(req.Provider) != "local" || s.settings == nil || contextsource.IsPotentialRemoteBuildContextSource(req.ContextDir) {
		return noop, nil
	}
	root, err := filepath.Abs(s.settings.GetStringSetting(ctx, "buildsDirectory", "/builds"))
	if err != nil {
		return noop, nil
	}
	root, err = filepath.EvalSymlinks(filepath.Clean(root))
	if err != nil {
		return noop, nil
	}
	path, err := filepath.Abs(filepath.Clean(req.ContextDir))
	if err != nil {
		return noop, nil
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return noop, nil
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return noop, nil
	}
	lockPath := path
	for current := path; current != root && current != filepath.Dir(current); current = filepath.Dir(current) {
		if info, statErr := os.Stat(filepath.Join(current, ".git")); statErr == nil && (info.IsDir() || info.Mode().IsRegular()) {
			lockPath = current
			break
		}
	}
	if !s.tryLockWorkspaceInternal(lockPath) {
		return noop, errors.New("this source directory is already being built")
	}
	return func() { s.unlockWorkspaceInternal(lockPath) }, nil
}

func (s *BuildService) unlockWorkspaceInternal(path string) {
	s.workspaceMu.Lock()
	delete(s.busyWorkspaces, path)
	s.workspaceMu.Unlock()
}

func (s *BuildService) updateBuildSourceInternal(ctx context.Context, buildID string, req buildtypes.BuildRequest, mode SourceUpdateMode, source *buildgit.WorktreeUpdate) error {
	updates := map[string]any{
		"tags":               models.StringSlice(req.Tags),
		"context_dir":        req.ContextDir,
		"dockerfile":         req.Dockerfile,
		"provider":           req.Provider,
		"push":               req.Push,
		"load":               req.Load,
		"source_update_mode": string(mode),
	}
	if source != nil {
		updates["source_revision"] = source.Revision
		updates["source_branch"] = source.Branch
		updates["source_repository"] = sanitizeBuildContextForEventInternal(source.RemoteURL)
	}
	return s.db.WithContext(ctx).Model(&models.ImageBuild{}).Where("id = ?", buildID).Updates(updates).Error
}

func (s *BuildService) resolveBuildRequestInternal(
	ctx context.Context,
	req buildtypes.BuildRequest,
	progressWriter io.Writer,
	serviceName string,
) (buildtypes.BuildRequest, func() error, error) {
	source, ok, err := contextsource.ParseGitBuildContextSource(req.ContextDir)
	if err != nil {
		return buildtypes.BuildRequest{}, func() error { return nil }, err
	}
	if !ok || source == nil {
		if contextsource.IsPotentialRemoteBuildContextSource(req.ContextDir) {
			return buildtypes.BuildRequest{}, func() error { return nil }, fmt.Errorf("unsupported remote build context source %q: only git repository URLs are supported", req.ContextDir)
		}
		return req, func() error { return nil }, nil
	}

	writeBuildProgressStatusInternal(progressWriter, serviceName, "resolving remote git context "+source.RepositoryURL)

	authConfig, matchedRepository, err := s.resolveGitBuildAuthInternal(ctx, source.RepositoryURL)
	if err != nil {
		return buildtypes.BuildRequest{}, func() error { return nil }, err
	}
	if matchedRepository {
		writeBuildProgressStatusInternal(progressWriter, serviceName, "using saved git credentials for "+source.RepositoryURL)
	}
	if contextsource.RequiresGitRemoteProbe(source.RepositoryURL) {
		writeBuildProgressStatusInternal(progressWriter, serviceName, "verifying remote git repository "+source.RepositoryURL)
		if err := s.probeGitContextInternal(ctx, source.RepositoryURL, authConfig); err != nil {
			return buildtypes.BuildRequest{}, func() error { return nil }, fmt.Errorf("failed to verify remote git repository %q: %w", source.RepositoryURL, err)
		}
	}

	repoPath, err := s.cloneGitContextInternal(ctx, source.RepositoryURL, source.Ref, authConfig)
	if err != nil {
		return buildtypes.BuildRequest{}, func() error { return nil }, err
	}

	contextDir := repoPath
	if source.Subdir != "" {
		if err := buildgit.ValidatePath(repoPath, filepath.FromSlash(source.Subdir)); err != nil {
			_ = s.cleanupGitContextInternal(repoPath)
			return buildtypes.BuildRequest{}, func() error { return nil }, fmt.Errorf("invalid git build context subdir: %w", err)
		}
		contextDir = filepath.Join(repoPath, filepath.FromSlash(source.Subdir))
	}

	info, err := os.Stat(contextDir)
	if err != nil {
		_ = s.cleanupGitContextInternal(repoPath)
		return buildtypes.BuildRequest{}, func() error { return nil }, fmt.Errorf("failed to stat resolved git build context: %w", err)
	}
	if !info.IsDir() {
		_ = s.cleanupGitContextInternal(repoPath)
		return buildtypes.BuildRequest{}, func() error { return nil }, errors.New("resolved git build context is not a directory")
	}

	writeBuildProgressStatusInternal(progressWriter, serviceName, "using remote build context "+source.Raw)

	resolvedReq := req
	resolvedReq.ContextDir = contextDir

	return resolvedReq, func() error { return s.cleanupGitContextInternal(repoPath) }, nil
}

func (s *BuildService) resolveGitBuildAuthInternal(ctx context.Context, rawURL string) (buildgit.AuthConfig, bool, error) {
	if s.gitRepository == nil {
		return buildgit.AuthConfig{}, false, nil
	}

	repository, err := s.gitRepository.FindEnabledRepositoryByURL(ctx, rawURL)
	if err != nil {
		return buildgit.AuthConfig{}, false, fmt.Errorf("failed to resolve git repository credentials: %w", err)
	}
	if repository == nil {
		return buildgit.AuthConfig{}, false, nil
	}

	authConfig, err := s.gitRepository.GetAuthConfig(ctx, repository)
	if err != nil {
		return buildgit.AuthConfig{}, true, fmt.Errorf("failed to load git repository credentials: %w", err)
	}

	return authConfig, true, nil
}

func (s *BuildService) probeGitContextInternal(ctx context.Context, repositoryURL string, authConfig buildgit.AuthConfig) error {
	if s.gitProbeFn != nil {
		return s.gitProbeFn(ctx, repositoryURL, authConfig)
	}

	if s.gitRepository != nil && s.gitRepository.gitClient != nil {
		return s.gitRepository.gitClient.ProbeRemote(ctx, repositoryURL, authConfig)
	}

	return errors.New("git repository service not available")
}

func (s *BuildService) cloneGitContextInternal(ctx context.Context, repositoryURL, ref string, authConfig buildgit.AuthConfig) (string, error) {
	if s.gitCloneFn != nil {
		return s.gitCloneFn(ctx, repositoryURL, ref, authConfig)
	}

	if s.gitRepository != nil && s.gitRepository.gitClient != nil {
		return s.gitRepository.gitClient.Clone(ctx, repositoryURL, ref, authConfig)
	}

	return "", errors.New("git repository service not available")
}

func (s *BuildService) cleanupGitContextInternal(repoPath string) error {
	if repoPath == "" {
		return nil
	}
	if s.gitCleanupFn != nil {
		return s.gitCleanupFn(repoPath)
	}
	if s.gitRepository != nil && s.gitRepository.gitClient != nil {
		return s.gitRepository.gitClient.Cleanup(repoPath)
	}
	return errors.New("git repository service not available")
}

func writeBuildProgressStatusInternal(progressWriter io.Writer, serviceName, status string) {
	if progressWriter == nil || strings.TrimSpace(status) == "" {
		return
	}

	if err := json.NewEncoder(progressWriter).Encode(buildtypes.ProgressEvent{
		Type:    "build",
		Service: serviceName,
		Status:  status,
	}); err != nil {
		slog.Debug("failed to write build progress status", "error", err)
	}
}

func (s *BuildService) ListImageBuildsByEnvironmentPaginated(ctx context.Context, environmentID string, params pagination.QueryParams) ([]imagetypes.BuildRecord, pagination.Response, error) {
	if s.db == nil {
		return nil, pagination.Response{}, errors.New("build history not available")
	}

	var builds []models.ImageBuild
	q := s.db.WithContext(ctx).Model(&models.ImageBuild{}).Where("environment_id = ?", environmentID)

	if term := strings.TrimSpace(params.Search); term != "" {
		searchPattern := "%" + term + "%"
		q = q.Where(
			"context_dir LIKE ? OR COALESCE(dockerfile, '') LIKE ? OR COALESCE(username, '') LIKE ? OR COALESCE(provider, '') LIKE ? OR COALESCE(error_message, '') LIKE ?",
			searchPattern, searchPattern, searchPattern, searchPattern, searchPattern,
		)
	}

	q = pagination.ApplyFilter(q, "status", params.Filters["status"])
	q = pagination.ApplyFilter(q, "provider", params.Filters["provider"])

	if params.Sort == "" {
		params.Sort = "createdAt"
	}

	paginationResp, err := pagination.PaginateAndSortDB(params, q, &builds)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to paginate builds: %w", err)
	}

	records := make([]imagetypes.BuildRecord, 0, len(builds))
	for _, build := range builds {
		records = append(records, buildToRecord(build, false))
	}

	return records, paginationResp, nil
}

func (s *BuildService) GetImageBuildByID(ctx context.Context, environmentID, buildID string) (*imagetypes.BuildRecord, error) {
	if s.db == nil {
		return nil, errors.New("build history not available")
	}

	var build models.ImageBuild
	if err := s.db.WithContext(ctx).First(&build, "id = ? AND environment_id = ?", buildID, environmentID).Error; err != nil {
		return nil, err
	}

	return new(buildToRecord(build, true)), nil
}

func (s *BuildService) createBuildRecord(ctx context.Context, environmentID string, req buildtypes.BuildRequest, sourceMode SourceUpdateMode, user *models.User) (*models.ImageBuild, error) {
	buildArgs := mapToJSON(req.BuildArgs)
	labels := mapToJSON(req.Labels)
	ulimits := mapToJSON(req.Ulimits)

	var userID *string
	var username *string
	if user != nil {
		userID = &user.ID
		username = &user.Username
	}

	record := &models.ImageBuild{
		EnvironmentID:    environmentID,
		UserID:           userID,
		Username:         username,
		Status:           models.ImageBuildStatusRunning,
		Provider:         req.Provider,
		ContextDir:       req.ContextDir,
		Dockerfile:       req.Dockerfile,
		Target:           req.Target,
		Tags:             models.StringSlice(req.Tags),
		Platforms:        models.StringSlice(req.Platforms),
		BuildArgs:        buildArgs,
		Labels:           labels,
		CacheFrom:        models.StringSlice(req.CacheFrom),
		CacheTo:          models.StringSlice(req.CacheTo),
		NoCache:          req.NoCache,
		Pull:             req.Pull,
		BuildNetwork:     req.Network,
		Isolation:        req.Isolation,
		ShmSize:          req.ShmSize,
		Ulimits:          ulimits,
		Entitlements:     models.StringSlice(req.Entitlements),
		Privileged:       req.Privileged,
		ExtraHosts:       models.StringSlice(req.ExtraHosts),
		Push:             req.Push,
		Load:             req.Load,
		SourceUpdateMode: string(sourceMode),
		BaseModel: models.BaseModel{
			CreatedAt: time.Now(),
		},
	}

	if err := s.db.WithContext(ctx).Create(record).Error; err != nil {
		return nil, fmt.Errorf("failed to create build record: %w", err)
	}

	return record, nil
}

func (s *BuildService) completeBuildRecord(
	ctx context.Context,
	buildID string,
	status models.ImageBuildStatus,
	output *string,
	outputTruncated bool,
	errMsg *string,
	digest *string,
	provider string,
	completedAt time.Time,
	durationMs *int64,
) error {
	if s.db == nil {
		return nil
	}

	updates := map[string]any{
		"status":           status,
		"completed_at":     completedAt,
		"duration_ms":      durationMs,
		"output":           output,
		"output_truncated": outputTruncated,
		"error_message":    errMsg,
		"digest":           digest,
		"provider":         provider,
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&models.ImageBuild{}).Where("id = ?", buildID).Updates(updates)
		if result.Error != nil {
			return fmt.Errorf("failed to update build record: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return errors.New("build record not found")
		}
		return nil
	})
}

func buildToRecord(build models.ImageBuild, includeOutput bool) imagetypes.BuildRecord {
	buildArgs := jsonToStringMap(build.BuildArgs)
	labels := jsonToStringMap(build.Labels)
	ulimits := jsonToStringMap(build.Ulimits)

	var output *string
	if includeOutput {
		output = build.Output
	}

	return imagetypes.BuildRecord{
		ID:               build.ID,
		EnvironmentID:    build.EnvironmentID,
		UserID:           build.UserID,
		Username:         build.Username,
		Status:           string(build.Status),
		Provider:         build.Provider,
		ContextDir:       build.ContextDir,
		Dockerfile:       build.Dockerfile,
		Target:           build.Target,
		Tags:             []string(build.Tags),
		Platforms:        []string(build.Platforms),
		BuildArgs:        buildArgs,
		Labels:           labels,
		CacheFrom:        []string(build.CacheFrom),
		CacheTo:          []string(build.CacheTo),
		NoCache:          build.NoCache,
		Pull:             build.Pull,
		Network:          build.BuildNetwork,
		Isolation:        build.Isolation,
		ShmSize:          build.ShmSize,
		Ulimits:          ulimits,
		Entitlements:     []string(build.Entitlements),
		Privileged:       build.Privileged,
		ExtraHosts:       []string(build.ExtraHosts),
		Push:             build.Push,
		Load:             build.Load,
		SourceUpdateMode: build.SourceUpdateMode,
		SourceRevision:   build.SourceRevision,
		SourceBranch:     build.SourceBranch,
		SourceRepository: build.SourceRepository,
		Digest:           build.Digest,
		ErrorMessage:     build.ErrorMessage,
		Output:           output,
		OutputTruncated:  build.OutputTruncated,
		CompletedAt:      build.CompletedAt,
		DurationMs:       build.DurationMs,
		CreatedAt:        build.CreatedAt,
	}
}

func mapToJSON(input map[string]string) models.JSON {
	if len(input) == 0 {
		return nil
	}

	out := models.JSON{}
	for key, value := range input {
		out[key] = value
	}

	if len(out) == 0 {
		return nil
	}

	return out
}

func jsonToStringMap(input models.JSON) map[string]string {
	out := map[string]string{}
	for key, value := range input {
		out[key] = fmt.Sprint(value)
	}

	if len(out) == 0 {
		return nil
	}

	return out
}
