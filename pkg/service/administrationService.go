// Copyright 2024-2025 NetCracker Technology Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package service

import (
	"context"
	"fmt"
	"strings"

	dto "github.com/Netcracker/qubership-dbaas-adapter-core/pkg/dao"
	"github.com/Netcracker/qubership-dbaas-adapter-core/pkg/utils"
	"go.uber.org/zap"
)

const (
	vaultRole           = "vaultRole"
	dbResourceKind      = "database"
	userResourceKind    = "user"
	vaultPasswordPrefix = "vault:"
)

type DbAdministration interface {
	// CreateDatabase may return 400 "Provided namePrefix does not meet the requirements"
	CreateDatabase(ctx context.Context, requestOnCreateDb dto.DbCreateRequest) (string, *dto.LogicalDatabaseDescribed, error)
	DescribeDatabases(ctx context.Context, logicalDatabases []string, showResources bool, showConnections bool) map[string]dto.LogicalDatabaseDescribed
	GetDatabases(ctx context.Context) []string
	DropResources(ctx context.Context, resources []dto.DbResource) []dto.DbResource
	GetMetadata(ctx context.Context, logicalDatabase string) map[string]any
	UpdateMetadata(ctx context.Context, newMetadata map[string]any, logicalDatabases string)
	GetDefaultCreateRequest() dto.DbCreateRequest
	GetDefaultUserCreateRequest() dto.UserCreateRequest
	// PreStart function runs some routine before server starts to listen for requests
	PreStart()
	CreateUser(ctx context.Context, userName string, requestOnCreateUser dto.UserCreateRequest) (*dto.CreatedUser, error)
	GetDBPrefix() string
	GetDBPrefixDelimiter() string
	MigrateToVault(ctx context.Context, dbName, userName string) error
	GetVersion() dto.ApiVersion
	GetSupportedRoles() []string
	GetFeatures() map[string]bool
	CreateRoles(ctx context.Context, roles []dto.AdditionalRole) ([]dto.Success, *dto.Failure)
}

type CoreAdministrationServiceIface interface {
	CreateDatabase(ctx context.Context, requestOnCreateDb dto.DbCreateRequest) (any, error)
	DropResources(ctx context.Context, resources []dto.DbResource) (*[]dto.DbResource, bool)
	GetDatabases(ctx context.Context) []string
	UpdateMetadata(ctx context.Context, newMetadata map[string]any, serviceName string)
	DescribeDatabases(ctx context.Context, logicalDatabases []string, isShowResources bool, isShowConnections bool) map[string]any
	GetDefaultCreateRequest() dto.DbCreateRequest
	GetDefaultUserCreateRequest() dto.UserCreateRequest
	PreStart()
	CreateUser(ctx context.Context, userName string, requestOnCreateUser dto.UserCreateRequest) (*dto.CreatedUser, error)
	MigrateToVault(ctx context.Context, dbName, userName string) (string, error)
	GetVersion() dto.ApiVersion
	CreateRoles(ctx context.Context, roles []dto.AdditionalRole) ([]dto.Success, *dto.Failure)
	GetSupportedRoles() []string
	GetFeatures() map[string]bool
	GetROHost() string
}

type CoreAdministrationService struct {
	namespace      string
	port           int
	dbAdm          DbAdministration
	logger         *zap.Logger
	isVaultEnabled bool
	vaultClient    *utils.VaultClient
	roHost         string
}

func NewCoreAdministrationService(
	namespace string,
	port int,
	dbAdm DbAdministration,
	logger *zap.Logger,
	isVaultEnabled bool,
	vaultClient *utils.VaultClient,
	roHost string) CoreAdministrationServiceIface {
	return &CoreAdministrationService{
		namespace:      namespace,
		port:           port,
		dbAdm:          dbAdm,
		logger:         logger,
		isVaultEnabled: isVaultEnabled,
		vaultClient:    vaultClient,
		roHost:         roHost,
	}
}

func (adminService *CoreAdministrationService) CreateDatabase(ctx context.Context, requestOnCreateDb dto.DbCreateRequest) (any, error) {
	//metadata creation should be inside as well
	logicalDatabaseName, dbDescribed, createErr := adminService.dbAdm.CreateDatabase(ctx, requestOnCreateDb)
	if createErr != nil {
		return nil, createErr
	}
	if adminService.isVaultEnabled {
		if err := adminService.createVaultRolesForDatabase(ctx, logicalDatabaseName, dbDescribed, requestOnCreateDb); err != nil {
			adminService.dbAdm.DropResources(ctx, dbDescribed.Resources)
			return nil, err
		}
	}
	logger := utils.AddLoggerContext(adminService.logger, ctx)

	logger.Info(fmt.Sprintf("Logical database with name %s has resources %+v", logicalDatabaseName, dbDescribed.Resources))

	return adminService.prepareDbCreateResponse(logicalDatabaseName, dbDescribed), nil
}

func (adminService *CoreAdministrationService) DropResources(ctx context.Context, resources []dto.DbResource) (*[]dto.DbResource, bool) {
	logger := utils.AddLoggerContext(adminService.logger, ctx)
	var dropResourcesResponse []dto.DbResource
	isFailed := false
	if adminService.isVaultEnabled {
		adminService.performVaultRolesDelete(ctx, resources)
	}
	dropResourcesResponse = adminService.dbAdm.DropResources(ctx, resources)

	for _, resource := range dropResourcesResponse {
		if resource.Status == dto.DELETE_FAILED {
			errMsg := "without error message"
			if resource.ErrorMessage != "" {
				errMsg = resource.ErrorMessage
			}
			logger.Warn(fmt.Sprintf("Error during deleting resource %s with name \"%s\", %v", resource.Kind, resource.Name, errMsg))
			isFailed = true
		} else {
			if resource.Status == "" {
				resource.Status = dto.DELETED
			}
			logger.Info(fmt.Sprintf("The resource %s:%s was deleted successfully.", resource.Kind, resource.Name))
		}
	}
	return &dropResourcesResponse, isFailed
}

func (adminService *CoreAdministrationService) GetDatabases(ctx context.Context) []string {
	logger := utils.AddLoggerContext(adminService.logger, ctx)
	dbs := adminService.dbAdm.GetDatabases(ctx)
	logger.Debug(fmt.Sprintf("Found %v databases", len(dbs)))
	return dbs
}

func (adminService *CoreAdministrationService) UpdateMetadata(ctx context.Context, newMetadata map[string]any, serviceName string) {
	logger := utils.AddLoggerContext(adminService.logger, ctx)
	adminService.dbAdm.UpdateMetadata(ctx, newMetadata, serviceName)
	logger.Debug(fmt.Sprintf("Metadata for %v keyspace is updated", serviceName))
}

func (adminService *CoreAdministrationService) DescribeDatabases(ctx context.Context, logicalDatabases []string, isShowResources bool, isShowConnections bool) map[string]any {
	logger := utils.AddLoggerContext(adminService.logger, ctx)
	dbsList := logicalDatabases
	if len(dbsList) == 0 {
		logger.Debug("Databases list is empty, collecting full databases list...")
		dbsList = adminService.dbAdm.GetDatabases(ctx)
	}
	logger.Info(fmt.Sprintf("The next databases %+v will be described, isShowResources = %t, isShowConnections = %t", logicalDatabases, isShowResources, isShowConnections))
	describedLogicalDbs := adminService.dbAdm.DescribeDatabases(ctx, dbsList, isShowResources, isShowConnections)
	return adminService.prepareDbDescriptions(describedLogicalDbs)
}

func (adminService *CoreAdministrationService) GetDefaultCreateRequest() dto.DbCreateRequest {
	return adminService.dbAdm.GetDefaultCreateRequest()
}

func (adminService *CoreAdministrationService) GetDefaultUserCreateRequest() dto.UserCreateRequest {
	return adminService.dbAdm.GetDefaultUserCreateRequest()
}

func (adminService *CoreAdministrationService) PreStart() {
	adminService.logger.Debug(fmt.Sprintf("PreStart function is started"))
	adminService.dbAdm.PreStart()
	adminService.logger.Debug(fmt.Sprintf("PreStart function is finished"))
}

func (adminService *CoreAdministrationService) CreateUser(ctx context.Context, userName string, requestOnCreateUser dto.UserCreateRequest) (*dto.CreatedUser, error) {
	createdUser, err := adminService.dbAdm.CreateUser(ctx, userName, requestOnCreateUser)
	if err == nil {
		password := createdUser.ConnectionProperties["password"].(string)
		if adminService.isVaultEnabled && strings.HasPrefix(password, vaultPasswordPrefix) {
			dbRole := password[len(vaultPasswordPrefix):]
			err := adminService.vaultClient.ForceRefreshCredsFor(dbRole)
			if err != nil {
				return nil, err
			}
		}
	}
	return createdUser, err
}

func (adminService *CoreAdministrationService) MigrateToVault(ctx context.Context, dbName, userName string) (string, error) { //TODO handle panic
	err := adminService.dbAdm.MigrateToVault(ctx, dbName, userName)
	if err != nil {
		return "", err
	}
	metadata := adminService.dbAdm.GetMetadata(ctx, dbName)
	return adminService.createVaultRole(ctx, metadata, dbName, userName)
}

func (adminService *CoreAdministrationService) GetVersion() dto.ApiVersion {
	return adminService.dbAdm.GetVersion()
}

func (adminService *CoreAdministrationService) createVaultRole(ctx context.Context, metadata map[string]any, dbName, userName string) (string, error) {
	err := validateSettingMetadata(metadata)
	if err != nil {
		return "", err
	}
	classifier := metadata["classifier"].(map[string]any)
	namespace := classifier["namespace"].(string)
	microserviceName := metadata["microserviceName"].(string)
	cloudPublicHost := utils.GetEnv("CLOUD_PUBLIC_HOST", "")
	roleName, err := adminService.vaultClient.CreateVaultRole(cloudPublicHost, namespace, microserviceName, userName)
	if err != nil {
		return "", err
	}
	metadata = appendVaultRoleToMetadata(metadata, roleName)
	adminService.dbAdm.UpdateMetadata(ctx, metadata, dbName)
	return roleName, nil
}

func (adminService *CoreAdministrationService) performVaultRolesDelete(ctx context.Context, resources []dto.DbResource) {
	logger := utils.AddLoggerContext(adminService.logger, ctx)
	for _, resource := range resources {
		if resource.Kind == dbResourceKind {
			metadata := adminService.dbAdm.GetMetadata(ctx, resource.Name)
			if metadata != nil {
				if vaultRoleName, ok := metadata[vaultRole].(string); ok {
					_ = adminService.vaultClient.DeleteVaultRole(vaultRoleName)
				} else if vaultRoleNames, ok := metadata[vaultRole].([]any); ok {
					for _, vaultRoleName := range vaultRoleNames {
						_ = adminService.vaultClient.DeleteVaultRole(vaultRoleName.(string))
					}
				} else {
					logger.Debug(fmt.Sprintf("vaultRole can't be found in metadata for %s", resource.Name))
				}
			} else {
				logger.Debug(fmt.Sprintf("can't get metadata for %s", resource.Name))
			}
		}
	}
}

func (adminService *CoreAdministrationService) prepareDbDescriptions(dbDescribed map[string]dto.LogicalDatabaseDescribed) map[string]any {
	result := make(map[string]any, 0)
	if adminService.dbAdm.GetVersion() == "v1" {
		for key, description := range dbDescribed {
			oldDescription := dto.LogicalDatabaseDescribedSingle{
				ConnectionProperties: description.ConnectionProperties[0],
				Resources:            description.Resources,
			}
			result[key] = oldDescription
		}
	} else {
		for key, description := range dbDescribed {
			result[key] = description
		}
	}

	return result
}

func (adminService *CoreAdministrationService) prepareDbCreateResponse(logicalDatabaseName string, dbDescribed *dto.LogicalDatabaseDescribed) any {
	if adminService.dbAdm.GetVersion() == "v1" {
		return dto.DbCreateResponse{Name: logicalDatabaseName, ConnectionProperties: dbDescribed.ConnectionProperties[0], Resources: dbDescribed.Resources}
	}
	return dto.DbCreateResponseMultiUser{Name: logicalDatabaseName, ConnectionProperties: dbDescribed.ConnectionProperties, Resources: dbDescribed.Resources}
}

func (adminService *CoreAdministrationService) createVaultRolesForDatabase(ctx context.Context, logicalDatabaseName string, dbDescribed *dto.LogicalDatabaseDescribed, requestOnCreateDb dto.DbCreateRequest) error {
	i := 0
	for _, resource := range dbDescribed.Resources {
		if resource.Kind == userResourceKind {
			//mongo returns username as admin:actual-user-name
			userName := strings.TrimPrefix(resource.Name, "admin:")
			vaultRoleName, err := adminService.createVaultRole(ctx, requestOnCreateDb.Metadata, logicalDatabaseName, userName)
			if err != nil {
				return err
			}
			password := utils.VaultPasswordPrefix + vaultRoleName
			dbDescribed.ConnectionProperties[i]["password"] = password
			i++
		}
	}
	return nil
}

func (adminService *CoreAdministrationService) GetSupportedRoles() []string {
	return adminService.dbAdm.GetSupportedRoles()
}

func (adminService *CoreAdministrationService) GetFeatures() map[string]bool {
	return adminService.dbAdm.GetFeatures()
}

func (adminService *CoreAdministrationService) GetROHost() string {
	return adminService.roHost
}

func (adminService *CoreAdministrationService) CreateRoles(ctx context.Context, additionalRoles []dto.AdditionalRole) (resultSuccess []dto.Success, failure *dto.Failure) {
	success, failure := adminService.dbAdm.CreateRoles(ctx, additionalRoles)
	needToDrop := false

	for _, successForMigrate := range success {
		userResources := make([]dto.DbResource, 0)
		for _, resource := range successForMigrate.Resources {
			if resource.Kind == userResourceKind {
				userResources = append(userResources, resource)
			}
		}
		if failure != nil && failure.Id == successForMigrate.Id {
			needToDrop = true
		}

		if !needToDrop && adminService.isVaultEnabled {
			for i, resource := range userResources {
				vaultRoleName, err := adminService.MigrateToVault(ctx, successForMigrate.DbName, resource.Name)
				if err != nil {
					if failure == nil {
						failure = &dto.Failure{
							Id:      successForMigrate.Id,
							Message: fmt.Sprintf("cannot migrate role %s for database %s to vault", vaultRoleName, successForMigrate.DbName),
						}
					} else {
						failure.Id = successForMigrate.Id
					}
					needToDrop = true
					break
				} else {
					password := utils.VaultPasswordPrefix + vaultRoleName
					successForMigrate.ConnectionProperties[i]["password"] = password
				}
			}
		}

		if needToDrop {
			adminService.DropResources(ctx, userResources)
			continue
		} else {
			resultSuccess = append(resultSuccess, successForMigrate)
		}
	}
	return resultSuccess, failure
}

func appendVaultRoleToMetadata(metadata map[string]any, roleName string) map[string]any {
	if role, ok := metadata[vaultRole].(string); ok {
		roles := make([]any, 0)
		roles = append(roles, role, roleName)
		metadata[vaultRole] = roles
	} else if roles, ok := metadata[vaultRole].([]any); ok {
		roles = append(roles, roleName)
		metadata[vaultRole] = roles
	} else {
		metadata[vaultRole] = roleName
	}
	return metadata
}

func validateSettingMetadata(metadata map[string]any) error {
	classifierData := metadata["classifier"]
	if classifierData == nil {
		return fmt.Errorf("request contains not valid 'classifier' parameter in metadata")
	}

	classifier := metadata["classifier"].(map[string]any)
	if classifier["namespace"] == nil {
		return fmt.Errorf("request contains not valid 'namespace' parameter in classifier")
	}

	if metadata["microserviceName"] == nil {
		return fmt.Errorf("request contains not valid 'microserviceName' parameter in metadata")
	}

	return nil
}
