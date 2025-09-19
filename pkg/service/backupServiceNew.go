package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	dto "github.com/Netcracker/qubership-dbaas-adapter-core/pkg/dao"
	"github.com/Netcracker/qubership-dbaas-adapter-core/pkg/utils"
	"go.uber.org/zap"
)

const (
	backupAPIv1 = "api/v1"
)

// CollectBackupNew creates a new backup with the specified parameters
func (d DefaultBackupAdministrationImpl) CollectBackupNew(ctx context.Context, storageName, blobPath string, databaseNames []string) (*dto.BackupResponse, bool) {
	logger := utils.AddLoggerContext(d.logger, ctx)

	request := dto.BackupRequestNew{
		StorageName: storageName,
		BlobPath:    blobPath,
		Databases:   databaseNames,
	}
	requestBytes, err := json.Marshal(request)
	if err != nil {
		utils.PanicError(err, logger.Error, "Failed to marshal backup request")
	}

	res, err := http.Post(fmt.Sprintf("%s/%s/backup", d.backupAddress, backupAPIv1), "application/json", bytes.NewReader(requestBytes))
	if err != nil {
		utils.PanicError(err, logger.Error, "Failed to create backup")
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		utils.PanicError(err, logger.Error, "Failed to read backup response")
	}

	if res.StatusCode == http.StatusNotFound {
		logger.Warn("Backup daemon responded with status: not found")
		return nil, false
	}
	if res.StatusCode != http.StatusOK {
		utils.PanicError(fmt.Errorf("failed to create backup: %s", string(body)), logger.Error, "Failed to create backup")
	}

	databases := make([]dto.LogicalDatabaseBackup, len(databaseNames))
	for _, databaseName := range databaseNames {
		databases = append(databases, dto.LogicalDatabaseBackup{
			DatabaseName: databaseName,
			Status:       dto.NotStartedStatus,
		})
	}

	backupResponse := &dto.BackupResponse{
		Status:      dto.NotStartedStatus,
		StorageName: storageName,
		BlobPath:    blobPath,
		Databases:   databases,
	}
	err = json.Unmarshal(body, backupResponse)
	if err != nil {
		utils.PanicError(err, logger.Error, "Failed to unmarshal backup response")
	}

	logger.Info("Backup started",
		zap.String("backupId", backupResponse.BackupId),
		zap.String("storageName", storageName),
		zap.String("blobPath", blobPath),
		zap.Strings("databases", databaseNames))

	return backupResponse, true
}

// TrackBackupNew retrieves details about a specific backup operation
func (d DefaultBackupAdministrationImpl) TrackBackupNew(ctx context.Context, backupId string) (*dto.BackupResponse, bool) {
	logger := utils.AddLoggerContext(d.logger, ctx)

	res, err := http.Get(fmt.Sprintf("%s/%s/backup/%s", d.backupAddress, backupAPIv1, backupId))
	if err != nil {
		utils.PanicError(err, logger.Error, "Failed to get backup status")
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		utils.PanicError(err, logger.Error, "Failed to read backup response")
	}

	if res.StatusCode == http.StatusNotFound {
		logger.Warn("Backup daemon responded with status: not found")
		return nil, false
	}

	if res.StatusCode != http.StatusOK {
		utils.PanicError(fmt.Errorf("failed to get backup status: %s", string(body)), logger.Error, "Failed to get backup status")
	}

	backupResponse := &dto.BackupResponse{}
	err = json.Unmarshal(body, backupResponse)
	if err != nil {
		utils.PanicError(err, logger.Error, "Failed to unmarshal backup response")
	}

	return backupResponse, true
}

func (d DefaultBackupAdministrationImpl) EvictBackupNew(ctx context.Context, backupId string) bool {
	logger := utils.AddLoggerContext(d.logger, ctx)
	req, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/%s/backup/%s", d.backupAddress, backupAPIv1, backupId), nil)
	if err != nil {
		utils.PanicError(err, logger.Error, "Failed to create request")
	}

	res, err := d.client.Do(req)
	if err != nil {
		utils.PanicError(err, logger.Error, "Failed to evict backup")
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return false
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		utils.PanicError(err, logger.Error, "Failed to read backup response")
	}

	if res.StatusCode != http.StatusOK {
		utils.PanicError(fmt.Errorf("failed to evict backup: %s", string(body)), logger.Error, "Failed to evict backup")
	}

	return true
}

// RestoreBackupNew creates a new restore operation from a backup
func (d DefaultBackupAdministrationImpl) RestoreBackupNew(ctx context.Context, backupId string, restoreRequest dto.CreateRestoreRequest, dryRun bool) (*dto.RestoreResponse, bool) {
	logger := utils.AddLoggerContext(d.logger, ctx)

	databases := make([]dto.DaemonRestoreMapping, len(restoreRequest.Databases))

	for _, database := range restoreRequest.Databases {
		dbInfo := convertRestoreRequestToDbInfo(database)
		newDbName, err := d.generateNewDBName(dbInfo, false)
		if err != nil {
			utils.PanicError(err, logger.Error, "Failed to generate new db name")
		}
		databases = append(databases, dto.DaemonRestoreMapping{
			PreviousDatabaseName: database.DatabaseName,
			DatabaseName:         newDbName,
		})
	}

	request := dto.RestoreRequestNew{
		StorageName: restoreRequest.StorageName,
		BlobPath:    restoreRequest.BlobPath,
		Databases:   databases,
	}

	requestBytes, err := json.Marshal(request)
	if err != nil {
		utils.PanicError(err, logger.Error, "Failed to marshal backup request")
	}

	res, err := http.Post(fmt.Sprintf("%s/%s/restore/%s", d.backupAddress, backupAPIv1, backupId), "application/json", bytes.NewReader(requestBytes))
	if err != nil {
		utils.PanicError(err, logger.Error, "Failed to create backup")
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		utils.PanicError(err, logger.Error, "Failed to read backup response")
	}

	if res.StatusCode == http.StatusNotFound {
		logger.Warn("Backup daemon responded with status: not found")
		return nil, false
	}
	if res.StatusCode != http.StatusOK {
		utils.PanicError(fmt.Errorf("failed to create restore: %s", string(body)), logger.Error, "Failed to create restore")
	}

	restoreResponse := &dto.RestoreResponse{}
	err = json.Unmarshal(body, restoreResponse)
	if err != nil {
		utils.PanicError(err, logger.Error, "Failed to unmarshal restore response")
	}

	return restoreResponse, true
}

// TrackRestoreNew retrieves details about a specific restore operation
func (d DefaultBackupAdministrationImpl) TrackRestoreNew(ctx context.Context, restoreId string) (*dto.RestoreResponse, bool) {
	logger := utils.AddLoggerContext(d.logger, ctx)

	res, err := http.Get(fmt.Sprintf("%s/%s/restore/%s", d.backupAddress, backupAPIv1, restoreId))
	if err != nil {
		utils.PanicError(err, logger.Error, "Failed to get restore status")
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		utils.PanicError(err, logger.Error, "Failed to read restore response")
	}

	if res.StatusCode == http.StatusNotFound {
		logger.Warn("Backup daemon responded with status: not found")
		return nil, false
	}

	if res.StatusCode != http.StatusOK {
		utils.PanicError(fmt.Errorf("failed to get restore status: %s", string(body)), logger.Error, "Failed to get restore status")
	}

	restoreResponse := &dto.RestoreResponse{}
	err = json.Unmarshal(body, restoreResponse)
	if err != nil {
		utils.PanicError(err, logger.Error, "Failed to unmarshal backup response")
	}

	return restoreResponse, true
}

// EvictRestoreNew deletes a restore operation
func (d DefaultBackupAdministrationImpl) EvictRestoreNew(ctx context.Context, restoreId string) bool {
	logger := utils.AddLoggerContext(d.logger, ctx)
	req, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/%s/restore/%s", d.backupAddress, backupAPIv1, restoreId), nil)
	if err != nil {
		utils.PanicError(err, logger.Error, "Failed to create request")
	}

	res, err := d.client.Do(req)
	if err != nil {
		utils.PanicError(err, logger.Error, "Failed to evict restore")
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return false
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		utils.PanicError(err, logger.Error, "Failed to read restore response")
	}

	if res.StatusCode != http.StatusOK {
		utils.PanicError(fmt.Errorf("failed to evict restore: %s", string(body)), logger.Error, "Failed to evict restore")
	}

	return true
}

func convertRestoreRequestToDbInfo(database dto.RestoreMapping) dto.DbInfo {
	return dto.DbInfo{
		Name:         database.DatabaseName,
		Microservice: database.MicroserviceName,
		Namespace:    database.Namespace,
		Prefix:       database.Prefix,
	}
}
