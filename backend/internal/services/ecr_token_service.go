package services

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	utilsregistry "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/registryauth"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"go.getarcane.app/sys/crypto"
)

const ecrTokenTTL = 12 * time.Hour

var ecrRegistryHostPatternInternal = regexp.MustCompile(`^(\d{12})\.dkr\.ecr(?:-fips)?\.([a-z0-9-]+)\.amazonaws\.com(?:\.cn)?$`)

func isECRRegistryHostInternal(registryHost string) bool {
	host := utilsregistry.NormalizeRegistryForComparison(registryHost)
	return ecrRegistryHostPatternInternal.MatchString(host)
}

type ecrTokenResult struct {
	username string
	password string
}

// getLocalECRAuthHeaderInternal resolves ECR credentials from the standard AWS
// credential chain available to this process. It intentionally does not read or
// write Arcane's registry database, which lets headless agents authenticate with
// credentials supplied through environment variables, shared credential files,
// workload identity, or an instance role.
func getLocalECRAuthHeaderInternal(ctx context.Context, registryHost string) (string, bool, error) {
	host := utilsregistry.NormalizeRegistryForComparison(registryHost)
	matches := ecrRegistryHostPatternInternal.FindStringSubmatch(host)
	if len(matches) != 3 {
		return "", false, nil
	}

	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(matches[2]))
	if err != nil {
		return "", true, fmt.Errorf("load local AWS credentials for %s: %w", host, err)
	}

	result, err := ecr.NewFromConfig(cfg).GetAuthorizationToken(ctx, &ecr.GetAuthorizationTokenInput{
		RegistryIds: []string{matches[1]},
	})
	if err != nil {
		return "", true, fmt.Errorf("get ECR authorization token for %s: %w", host, err)
	}
	if len(result.AuthorizationData) == 0 || result.AuthorizationData[0].AuthorizationToken == nil {
		return "", true, fmt.Errorf("ECR returned empty authorization data for %s", host)
	}

	decoded, err := base64.StdEncoding.DecodeString(*result.AuthorizationData[0].AuthorizationToken)
	if err != nil {
		return "", true, fmt.Errorf("decode ECR authorization token for %s: %w", host, err)
	}
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || parts[1] == "" {
		return "", true, fmt.Errorf("unexpected ECR authorization token format for %s", host)
	}

	authHeader, err := utilsregistry.EncodeAuthHeader(parts[0], parts[1], host)
	if err != nil {
		return "", true, fmt.Errorf("encode ECR authorization for %s: %w", host, err)
	}
	return authHeader, true, nil
}

// GetOrRefreshECRToken returns a valid ECR auth token (username + password) for the given
// registry. If the cached token (stored encrypted in the DB) is still within its 12-hour
// validity window it is returned directly; otherwise a new token is obtained from the AWS
// ECR API, persisted back to the DB, and returned.
// Concurrent refreshes for the same registry are deduplicated via singleflight.
func (s *ContainerRegistryService) GetOrRefreshECRToken(ctx context.Context, reg *models.ContainerRegistry) (username, password string, err error) {
	// Fast path: return cached token if still valid.
	if reg.ECRTokenGeneratedAt != nil && time.Since(reg.ECRTokenGeneratedAt.UTC()) < ecrTokenTTL {
		if reg.ECRToken != "" {
			decrypted, decErr := crypto.Decrypt(reg.ECRToken)
			if decErr == nil && strings.TrimSpace(decrypted) != "" {
				return "AWS", decrypted, nil
			}
		}
	}

	// Slow path: deduplicate concurrent refreshes for the same registry.
	result, sErr, _ := s.ecrRefreshGroup.Do(reg.ID, func() (any, error) {
		// Detach from the request context so a cancelled caller doesn't
		// abort the shared refresh for all waiting goroutines.
		refreshCtx := context.WithoutCancel(ctx)
		return s.refreshECRTokenInternal(refreshCtx, reg)
	})
	if sErr != nil {
		return "", "", sErr
	}
	r, ok := result.(*ecrTokenResult)
	if !ok {
		return "", "", &common.ECRTokenResultTypeError{}
	}
	return r.username, r.password, nil
}

func (s *ContainerRegistryService) refreshECRTokenInternal(ctx context.Context, reg *models.ContainerRegistry) (*ecrTokenResult, error) {
	ecrClient, clientErr := s.ecrClientForRegistryInternal(ctx, reg)
	if clientErr != nil {
		return nil, clientErr
	}
	result, ecrErr := ecrClient.GetAuthorizationToken(ctx, &ecr.GetAuthorizationTokenInput{})
	if ecrErr != nil {
		return nil, fmt.Errorf("failed to get ECR authorization token for registry %s: %w", reg.URL, ecrErr)
	}
	if len(result.AuthorizationData) == 0 || result.AuthorizationData[0].AuthorizationToken == nil {
		return nil, fmt.Errorf("ECR returned empty authorization data for registry %s", reg.URL)
	}

	decoded, decodeErr := base64.StdEncoding.DecodeString(*result.AuthorizationData[0].AuthorizationToken)
	if decodeErr != nil {
		return nil, fmt.Errorf("failed to decode ECR token for registry %s: %w", reg.URL, decodeErr)
	}
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 || parts[1] == "" {
		return nil, fmt.Errorf("unexpected ECR token format for registry %s", reg.URL)
	}
	ecrPassword := parts[1]
	encryptedToken, encErr := crypto.Encrypt(ecrPassword)
	if encErr != nil {
		return nil, fmt.Errorf("failed to encrypt ECR token for registry %s: %w", reg.URL, encErr)
	}
	now := time.Now().UTC()
	reg.ECRToken = encryptedToken
	reg.ECRTokenGeneratedAt = &now
	if saveErr := s.db.WithContext(ctx).Model(reg).Updates(map[string]any{"ecr_token": encryptedToken, "ecr_token_generated_at": now}).Error; saveErr != nil {
		slog.WarnContext(ctx, "failed to persist ECR token to database", "registry", reg.URL, "error", saveErr)
	}
	return &ecrTokenResult{username: "AWS", password: ecrPassword}, nil
}

func (s *ContainerRegistryService) ecrClientForRegistryInternal(ctx context.Context, reg *models.ContainerRegistry) (*ecr.Client, error) {
	// Decrypt the stored AWS secret access key.
	secretKey, decErr := crypto.Decrypt(reg.AWSSecretAccessKey)
	if decErr != nil {
		return nil, fmt.Errorf("failed to decrypt AWS secret key for registry %s: %w", reg.URL, decErr)
	}
	secretKey = strings.TrimSpace(secretKey)
	if secretKey == "" {
		return nil, fmt.Errorf("AWS secret access key is empty for registry %s", reg.URL)
	}

	// Call AWS ECR GetAuthorizationToken.
	cfg, cfgErr := config.LoadDefaultConfig(ctx,
		config.WithRegion(reg.AWSRegion),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			reg.AWSAccessKeyID,
			secretKey,
			"",
		)),
	)
	if cfgErr != nil {
		return nil, fmt.Errorf("failed to load AWS config for registry %s: %w", reg.URL, cfgErr)
	}

	return ecr.NewFromConfig(cfg), nil
}

func (s *ContainerRegistryService) ListRegistryRepositories(ctx context.Context, registryID string) ([]string, error) {
	reg, err := s.GetRegistryByID(ctx, registryID)
	if err != nil {
		return nil, err
	}
	if reg.RegistryType != registryTypeECR {
		values := append([]string(nil), reg.RepositoryNames...)
		slices.Sort(values)
		return values, nil
	}
	client, err := s.ecrClientForRegistryInternal(ctx, reg)
	if err != nil {
		return nil, err
	}
	var values []string
	var token *string
	for {
		page, pageErr := client.DescribeRepositories(ctx, &ecr.DescribeRepositoriesInput{NextToken: token})
		if pageErr != nil {
			return nil, fmt.Errorf("describe ECR repositories: %w", pageErr)
		}
		for _, item := range page.Repositories {
			if item.RepositoryName != nil {
				values = append(values, *item.RepositoryName)
			}
		}
		token = page.NextToken
		if token == nil || *token == "" {
			slices.Sort(values)
			return values, nil
		}
	}
}

func (s *ContainerRegistryService) ListRegistryTags(ctx context.Context, registryID, repository string) ([]string, error) {
	reg, err := s.GetRegistryByID(ctx, registryID)
	if err != nil {
		return nil, err
	}
	repository = strings.TrimSpace(repository)
	if repository == "" {
		return nil, fmt.Errorf("repository is required")
	}
	if reg.RegistryType != registryTypeECR {
		registryHost := utilsregistry.NormalizeRegistryForComparison(reg.URL)
		repo, parseErr := name.NewRepository(registryHost+"/"+strings.Trim(repository, "/"), registryNameOptionsInternal(reg)...)
		if parseErr != nil {
			return nil, fmt.Errorf("parse registry repository: %w", parseErr)
		}
		remoteOptions := []remote.Option{remote.WithContext(ctx)}
		if strings.TrimSpace(reg.Username) != "" && reg.Token != "" {
			token, tokenErr := s.GetDecryptedToken(ctx, registryID)
			if tokenErr != nil {
				return nil, tokenErr
			}
			remoteOptions = append(remoteOptions, remote.WithAuth(authn.FromConfig(authn.AuthConfig{
				Username: strings.TrimSpace(reg.Username),
				Password: strings.TrimSpace(token),
			})))
		}
		values, listErr := remote.List(repo, remoteOptions...)
		if listErr != nil {
			return nil, fmt.Errorf("list registry tags: %w", listErr)
		}
		slices.Sort(values)
		return values, nil
	}
	client, err := s.ecrClientForRegistryInternal(ctx, reg)
	if err != nil {
		return nil, err
	}
	var values []string
	var token *string
	for {
		page, pageErr := client.DescribeImages(ctx, &ecr.DescribeImagesInput{RepositoryName: &repository, NextToken: token})
		if pageErr != nil {
			return nil, fmt.Errorf("describe ECR images: %w", pageErr)
		}
		for _, item := range page.ImageDetails {
			values = append(values, item.ImageTags...)
		}
		token = page.NextToken
		if token == nil || *token == "" {
			slices.Sort(values)
			return values, nil
		}
	}
}

func registryNameOptionsInternal(reg *models.ContainerRegistry) []name.Option {
	options := []name.Option{name.WeakValidation}
	if reg.Insecure {
		options = append(options, name.Insecure)
	}
	return options
}
