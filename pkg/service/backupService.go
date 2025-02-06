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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/docker/distribution/uuid"

	dto "github.com/Netcracker/qubership-dbaas-adapter-core/pkg/dao"
	"github.com/Netcracker/qubership-dbaas-adapter-core/pkg/utils"
	"go.uber.org/zap"
)

type BackupAdministrationService interface {
	CollectBackup(ctx context.Context, logicalDatabases []string, keepFromRequest string, allowEviction bool) dto.DatabaseAdapterBaseTrack
	TrackBackup(ctx context.Context, trackId string) (dto.DatabaseAdapterBaseTrack, bool)
	// RestoreBackup May return 501 "Cannot restore backup without explicitly specified list of databases in it"
	RestoreBackup(ctx context.Context, backupId string, logicalDatabases []dto.DbInfo, regenerateNames, oldNameFormat bool) (*dto.DatabaseAdapterRestoreTrack, error)
	TrackRestore(ctx context.Context, trackId string) (dto.DatabaseAdapterRestoreTrack, bool)
	EvictBackup(ctx context.Context, backupId string) (string, bool)
}

type DefaultBackupAdministrationImpl struct {
	logger         *zap.Logger
	backupAddress  string
	backupApiUser  string
	backupApiPass  string
	fullRestore    bool
	client         utils.HttpClient
	dbMaxLength    int
	specialSymbols []string
}

func DefaultBackupAdministrationService(
	logger *zap.Logger,
	backupAddress string,
	backupApiUser string,
	backupApiPass string,
	fullRestore bool,
	client utils.HttpClient, dbMaxLength int, specialSymbols []string) BackupAdministrationService {
	if client == nil {
		client = &http.Client{}
	}
	return DefaultBackupAdministrationImpl{
		logger,
		backupAddress,
		backupApiUser,
		backupApiPass,
		fullRestore,
		client,
		dbMaxLength,
		specialSymbols,
	}
}

func (d DefaultBackupAdministrationImpl) SendBackupRequest(ctx context.Context, method, uri string, bodyStruct interface{}) *http.Response {
	logger := utils.AddLoggerContext(d.logger, ctx)
	var req *http.Request
	var err error
	if method == http.MethodPost {
		codedBody, errm := json.Marshal(bodyStruct)
		utils.PanicError(errm, logger.Error, "Failed to marshal request body to send to backup")
		req, err = http.NewRequest(method, d.backupAddress+uri, bytes.NewReader(codedBody))
	} else {
		req, err = http.NewRequest(method, d.backupAddress+uri, nil)
	}
	utils.PanicError(err, logger.Error, "Failed to prepare request to send to backup")
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	req.SetBasicAuth(d.backupApiUser, d.backupApiPass)
	res, errs := d.client.Do(req)
	utils.PanicError(errs, logger.Error, "Failed to send request to backup")
	logger.Info(fmt.Sprintf("Received response with status: %s", res.Status))

	return res
}

func (d DefaultBackupAdministrationImpl) ReadResponseBody(ctx context.Context, response *http.Response) (int, []byte) {
	logger := utils.AddLoggerContext(d.logger, ctx)
	if response.StatusCode >= 200 && response.StatusCode <= 299 {
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		utils.PanicError(err, logger.Error, "Failed reading response from backup agent")
		return response.StatusCode, body
	} else if response.StatusCode == http.StatusNotFound {
		return response.StatusCode, nil
	} else {
		panic(fmt.Sprintf("Failed: Backup daemon responded with status: %v", response.StatusCode))
	}
}

func substr(s string, start, end int) string {
	counter, startIdx := 0, 0
	for i := range s {
		if counter == start {
			startIdx = i
		}
		if counter == end {
			return s[startIdx:i]
		}
		counter++
	}
	return s[startIdx:]
}

func (d DefaultBackupAdministrationImpl) CollectBackup(ctx context.Context, logicalDatabases []string, keepFromRequest string, allowEviction bool) dto.DatabaseAdapterBaseTrack {
	request := dto.BackupRequest{
		Args:          logicalDatabases,
		AllowEviction: strconv.FormatBool(allowEviction),
	}
	if keepFromRequest != "" {
		request.Keep = keepFromRequest
	}
	_, body := d.ReadResponseBody(ctx, d.SendBackupRequest(ctx, http.MethodPost, "/backup", request))
	return dto.GetDatabaseAdapterBackupActionTrack(dto.ProceedingTrackStatus, string(body))
}

func (d DefaultBackupAdministrationImpl) TrackBackup(ctx context.Context, trackId string) (dto.DatabaseAdapterBaseTrack, bool) {
	logger := utils.AddLoggerContext(d.logger, ctx)
	statusCode, body := d.ReadResponseBody(ctx, d.SendBackupRequest(ctx, http.MethodGet, "/jobstatus/"+trackId, nil))
	if statusCode == http.StatusNotFound {
		return dto.DatabaseAdapterBaseTrack{}, false
	}
	var response dto.BackupTask
	err := json.Unmarshal(body, &response)
	utils.PanicError(err, logger.Error, "Failed parsing backup daemon response")
	return dto.GetDatabaseAdapterBackupActionTrackByTask(response), true
}

func (d DefaultBackupAdministrationImpl) RegenerateDbName(dbName string) string {
	if len(dbName) > 63 || 63-len(dbName+"_clone_") <= 0 {
		return uuid.Generate().String()
	} else if 63-len(dbName+"_clone_")-7 >= 0 {
		return dbName + "_clone_" + utils.Substr(uuid.Generate().String(), 0, 7)
	} else {
		return dbName + "_clone_" + utils.Substr(uuid.Generate().String(), 0, 63-len(dbName+"_clone_"))
	}
}

func (d DefaultBackupAdministrationImpl) RestoreBackup(ctx context.Context, backupId string, logicalDatabases []dto.DbInfo, regenerateNames, oldNameFormat bool) (*dto.DatabaseAdapterRestoreTrack, error) {
	if !d.fullRestore && len(logicalDatabases) < 1 {
		return nil, &dto.BackupRestoresOnlySpecifiedDBsError{}
	}
	var changedDbNames map[string]string
	if regenerateNames {
		if d.fullRestore {
			panic("DBs name regeneration is not supported without specified DBs list")
		}
		changedDbNames = make(map[string]string)
		var err error
		for _, db := range logicalDatabases {
			var name string
			if oldNameFormat {
				name = d.RegenerateDbName(db.Name)
			} else if db.Prefix != nil {
				name = utils.RegenerateDbName(*db.Prefix, d.getMaxDbLength())
			} else {
				name, err = utils.PrepareDatabaseName(db.Namespace, db.Microservice, d.getMaxDbLength())
				if err != nil {
					panic(fmt.Sprintf("cannot generate new dbName for %v", db))
				}
			}
			for _, specialSymbol := range d.specialSymbols {
				name = strings.ReplaceAll(name, specialSymbol, "_")
			}
			changedDbNames[db.Name] = name
		}
	}
	request := dto.RestoreRequest{
		Vault:         backupId,
		Dbs:           getDbNames(logicalDatabases),
		ChangeDbNames: changedDbNames,
	}
	_, body := d.ReadResponseBody(ctx, d.SendBackupRequest(ctx, http.MethodPost, "/restore", request))
	track := dto.GetDatabaseAdapterRestoreActionTrack(dto.ProceedingTrackStatus, string(body), changedDbNames)
	return &track, nil
}

func (d DefaultBackupAdministrationImpl) TrackRestore(ctx context.Context, trackId string) (dto.DatabaseAdapterRestoreTrack, bool) {
	logger := utils.AddLoggerContext(d.logger, ctx)
	statusCode, body := d.ReadResponseBody(ctx, d.SendBackupRequest(ctx, http.MethodGet, "/jobstatus/"+trackId, nil))
	if statusCode == http.StatusNotFound {
		return dto.DatabaseAdapterRestoreTrack{}, false
	}
	var response dto.BackupTask
	err := json.Unmarshal(body, &response)
	utils.PanicError(err, logger.Error, "Failed parsing backup daemon response")
	return dto.GetDatabaseAdapterRestoreActionTrackByTask(response), true
}

func (d DefaultBackupAdministrationImpl) EvictBackup(ctx context.Context, backupId string) (string, bool) {
	statusCode, body := d.ReadResponseBody(ctx, d.SendBackupRequest(ctx, http.MethodPost, "/evict/"+backupId, nil))
	if statusCode == http.StatusNotFound {
		return "", false
	}
	return string(body), true
}

func (d DefaultBackupAdministrationImpl) getMaxDbLength() int {
	return d.dbMaxLength
}

var _ BackupAdministrationService = DefaultBackupAdministrationImpl{}

func getDbNames(dbInfo []dto.DbInfo) (result []string) {
	for _, db := range dbInfo {
		result = append(result, db.Name)
	}
	return
}
