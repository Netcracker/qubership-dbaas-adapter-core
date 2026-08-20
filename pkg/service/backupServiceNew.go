package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	dto "github.com/Netcracker/qubership-dbaas-adapter-core/pkg/dao"
	"github.com/Netcracker/qubership-dbaas-adapter-core/pkg/utils"
	"go.uber.org/zap"
)

const (
	backupAPIv1 = "api/v1"
)

// sendBackupV2Request builds, authenticates, and dispatches a V2 backup daemon request.
// SetBasicAuth is called unconditionally, matching the V1 SendBackupRequest behaviour.
func (d DefaultBackupAdministrationImpl) sendBackupV2Request(ctx context.Context, method, rawURL string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, rawURL, body)
	if err != nil {
		return nil, err
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	req.SetBasicAuth(d.backupApiUser, d.backupApiPass)
	return d.client.Do(req)
}

// CollectBackupV2 creates a new backup with the specified parameters
func (d DefaultBackupAdministrationImpl) CollectBackupV2(ctx context.Context, storageName, blobPath string, databaseNames []string) (*dto.BackupResponse, bool) {
	logger := utils.AddLoggerContext(d.logger, ctx)

	request := dto.BackupRequestV2{
		StorageName: storageName,
		BlobPath:    blobPath,
		Databases:   databaseNames,
	}
	requestBytes, err := json.Marshal(request)
	if err != nil {
		utils.PanicError(err, logger.Error, "Failed to marshal backup request")
	}

	res, err := d.sendBackupV2Request(ctx, http.MethodPost,
		fmt.Sprintf("%s/%s/backup", d.backupAddress, backupAPIv1),
		bytes.NewReader(requestBytes))
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
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusAccepted {
		utils.PanicError(fmt.Errorf("failed to create backup: %s", string(body)), logger.Error, "Failed to create backup")
	}

	databases := make([]dto.LogicalDatabaseBackup, 0, len(databaseNames))
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

// TrackBackupV2 retrieves details about a specific backup operation
func (d DefaultBackupAdministrationImpl) TrackBackupV2(ctx context.Context, backupId, blobPath string) (*dto.BackupResponse, bool) {
	logger := utils.AddLoggerContext(d.logger, ctx)

	u, _ := url.Parse(fmt.Sprintf("%s/%s/backup/%s", d.backupAddress, backupAPIv1, url.PathEscape(backupId)))
	q := u.Query()
	q.Set("blobPath", blobPath)
	u.RawQuery = q.Encode()
	res, err := d.sendBackupV2Request(ctx, http.MethodGet, u.String(), nil)
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

	backupResponse := &dto.BackupResponse{
		BlobPath: blobPath,
	}
	err = json.Unmarshal(body, backupResponse)
	if err != nil {
		utils.PanicError(err, logger.Error, "Failed to unmarshal backup response")
	}

	return backupResponse, true
}

func (d DefaultBackupAdministrationImpl) EvictBackupV2(ctx context.Context, backupId, blobPath string) bool {
	logger := utils.AddLoggerContext(d.logger, ctx)

	u, _ := url.Parse(fmt.Sprintf("%s/%s/backup/%s", d.backupAddress, backupAPIv1, url.PathEscape(backupId)))
	q := u.Query()
	q.Set("blobPath", blobPath)
	u.RawQuery = q.Encode()
	res, err := d.sendBackupV2Request(ctx, http.MethodDelete, u.String(), nil)
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

// RestoreBackupV2 creates a new restore operation from a backup
func (d DefaultBackupAdministrationImpl) RestoreBackupV2(ctx context.Context, backupId string, restoreRequest dto.CreateRestoreRequest, dryRun bool) (*dto.RestoreResponse, bool) {
	logger := utils.AddLoggerContext(d.logger, ctx)

	databases := make([]dto.DaemonRestoreMapping, 0, len(restoreRequest.Databases))
	databasesResp := make([]dto.LogicalDatabaseRestore, 0, len(databases))

	for _, database := range restoreRequest.Databases {
		dbInfo := convertRestoreRequestToDbInfo(database)
		newDbName, err := d.generateNewDBName(dbInfo, false)
		if err != nil {
			utils.PanicError(err, logger.Error, "Failed to generate new db name")
		}
		// Databases list for backup daemon request
		databases = append(databases, dto.DaemonRestoreMapping{
			PreviousDatabaseName: database.DatabaseName,
			DatabaseName:         newDbName,
		})
		// Databases list for response
		databasesResp = append(databasesResp, dto.LogicalDatabaseRestore{
			DatabaseName:         newDbName,
			PreviousDatabaseName: &database.DatabaseName,
			Status:               dto.NotStartedStatus,
		})
	}

	request := dto.RestoreRequestV2{
		StorageName: restoreRequest.StorageName,
		BlobPath:    restoreRequest.BlobPath,
		Databases:   databases,
		DryRun:      dryRun,
	}

	requestBytes, err := json.Marshal(request)
	if err != nil {
		utils.PanicError(err, logger.Error, "Failed to marshal backup request")
	}

	res, err := d.sendBackupV2Request(ctx, http.MethodPost,
		fmt.Sprintf("%s/%s/restore/%s", d.backupAddress, backupAPIv1, backupId),
		bytes.NewReader(requestBytes))
	if err != nil {
		utils.PanicError(err, logger.Error, "Failed to create restore")
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
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusAccepted {
		utils.PanicError(fmt.Errorf("failed to create restore: %s", string(body)), logger.Error, "Failed to create restore")
	}

	restoreResponse := &dto.RestoreResponse{
		Status:      dto.NotStartedStatus,
		StorageName: restoreRequest.StorageName,
		BlobPath:    restoreRequest.BlobPath,
		Databases:   databasesResp,
	}
	err = json.Unmarshal(body, restoreResponse)
	if err != nil {
		utils.PanicError(err, logger.Error, "Failed to unmarshal restore response")
	}

	return restoreResponse, true
}

// TrackRestoreV2 retrieves details about a specific restore operation
func (d DefaultBackupAdministrationImpl) TrackRestoreV2(ctx context.Context, restoreId, blobPath string) (*dto.RestoreResponse, bool) {
	logger := utils.AddLoggerContext(d.logger, ctx)

	u, _ := url.Parse(fmt.Sprintf("%s/%s/restore/%s", d.backupAddress, backupAPIv1, url.PathEscape(restoreId)))
	q := u.Query()
	q.Set("blobPath", blobPath)
	u.RawQuery = q.Encode()
	res, err := d.sendBackupV2Request(ctx, http.MethodGet, u.String(), nil)
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

	restoreResponse := &dto.RestoreResponse{
		BlobPath: blobPath,
	}
	err = json.Unmarshal(body, restoreResponse)
	if err != nil {
		utils.PanicError(err, logger.Error, "Failed to unmarshal backup response")
	}

	return restoreResponse, true
}

// EvictRestoreV2 deletes a restore operation
func (d DefaultBackupAdministrationImpl) EvictRestoreV2(ctx context.Context, restoreId, blobPath string) bool {
	logger := utils.AddLoggerContext(d.logger, ctx)

	u, _ := url.Parse(fmt.Sprintf("%s/%s/restore/%s", d.backupAddress, backupAPIv1, url.PathEscape(restoreId)))
	q := u.Query()
	q.Set("blobPath", blobPath)
	u.RawQuery = q.Encode()
	res, err := d.sendBackupV2Request(ctx, http.MethodDelete, u.String(), nil)
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
