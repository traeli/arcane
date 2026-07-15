package models

import (
	"time"
)

type ContainerRegistry struct {
	BaseModel

	URL                        string      `json:"url" sortable:"true"`
	Username                   string      `json:"username" sortable:"true"`
	Token                      string      `json:"token"`
	Description                *string     `json:"description,omitempty" sortable:"true"`
	Insecure                   bool        `json:"insecure" sortable:"true"`
	Enabled                    bool        `json:"enabled" sortable:"true"`
	RegistryType               string      `json:"registryType" sortable:"true"`
	RepositoryNames            StringSlice `json:"repositoryNames" gorm:"column:repository_names;type:text;not null;default:'[]'"`
	AWSAccessKeyID             string      `json:"awsAccessKeyId"`
	AWSSecretAccessKey         string      `json:"awsSecretAccessKey"`
	AWSRegion                  string      `json:"awsRegion"`
	ConsumerAWSAccessKeyID     string      `json:"consumerAwsAccessKeyId" gorm:"column:consumer_aws_access_key_id"`
	ConsumerAWSSecretAccessKey string      `json:"consumerAwsSecretAccessKey" gorm:"column:consumer_aws_secret_access_key"`
	ConsumerAWSRegion          string      `json:"consumerAwsRegion" gorm:"column:consumer_aws_region"`
	ECRToken                   string      `json:"ecrToken"`
	ECRTokenGeneratedAt        *time.Time  `json:"ecrTokenGeneratedAt"`
	CreatedAt                  time.Time   `json:"createdAt" sortable:"true"`
	UpdatedAt                  time.Time   `json:"updatedAt" sortable:"true"`
}

func (ContainerRegistry) TableName() string {
	return "container_registries"
}

type CreateContainerRegistryRequest struct {
	URL                        string   `json:"url" binding:"required"`
	Username                   string   `json:"username"`
	Token                      string   `json:"token"`
	Description                *string  `json:"description"`
	Insecure                   *bool    `json:"insecure"`
	Enabled                    *bool    `json:"enabled"`
	RegistryType               string   `json:"registryType"`
	RepositoryNames            []string `json:"repositoryNames"`
	AWSAccessKeyID             string   `json:"awsAccessKeyId"`
	AWSSecretAccessKey         string   `json:"awsSecretAccessKey"`
	AWSRegion                  string   `json:"awsRegion"`
	ConsumerAWSAccessKeyID     string   `json:"consumerAwsAccessKeyId"`
	ConsumerAWSSecretAccessKey string   `json:"consumerAwsSecretAccessKey"`
	ConsumerAWSRegion          string   `json:"consumerAwsRegion"`
}

type UpdateContainerRegistryRequest struct {
	URL                        *string   `json:"url"`
	Username                   *string   `json:"username"`
	Token                      *string   `json:"token"`
	Description                *string   `json:"description"`
	Insecure                   *bool     `json:"insecure"`
	Enabled                    *bool     `json:"enabled"`
	RegistryType               *string   `json:"registryType"`
	RepositoryNames            *[]string `json:"repositoryNames"`
	AWSAccessKeyID             *string   `json:"awsAccessKeyId"`
	AWSSecretAccessKey         *string   `json:"awsSecretAccessKey"`
	AWSRegion                  *string   `json:"awsRegion"`
	ConsumerAWSAccessKeyID     *string   `json:"consumerAwsAccessKeyId"`
	ConsumerAWSSecretAccessKey *string   `json:"consumerAwsSecretAccessKey"`
	ConsumerAWSRegion          *string   `json:"consumerAwsRegion"`
}

type ContainerRegistryEnvironmentStatus struct {
	BaseModel
	RegistryID        string     `json:"registryId" gorm:"column:registry_id;not null;uniqueIndex:idx_registry_environment_status"`
	EnvironmentID     string     `json:"environmentId" gorm:"column:environment_id;not null;uniqueIndex:idx_registry_environment_status"`
	DesiredVersion    string     `json:"desiredVersion" gorm:"column:desired_version;not null;default:''"`
	AppliedVersion    string     `json:"appliedVersion" gorm:"column:applied_version;not null;default:''"`
	SyncStatus        string     `json:"syncStatus" gorm:"column:sync_status;not null;default:'pending'"`
	LastSyncAt        *time.Time `json:"lastSyncAt,omitempty" gorm:"column:last_sync_at"`
	LastSyncError     string     `json:"lastSyncError,omitempty" gorm:"column:last_sync_error;not null;default:''"`
	PullTestStatus    string     `json:"pullTestStatus" gorm:"column:pull_test_status;not null;default:'unknown'"`
	LastPullTestAt    *time.Time `json:"lastPullTestAt,omitempty" gorm:"column:last_pull_test_at"`
	LastPullTestError string     `json:"lastPullTestError,omitempty" gorm:"column:last_pull_test_error;not null;default:''"`
}

func (ContainerRegistryEnvironmentStatus) TableName() string {
	return "container_registry_environment_statuses"
}
