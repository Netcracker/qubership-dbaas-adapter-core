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

package testing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Netcracker/qubership-dbaas-adapter-core/pkg/dao"
	"github.com/Netcracker/qubership-dbaas-adapter-core/pkg/dbaas"
	fiber2 "github.com/Netcracker/qubership-dbaas-adapter-core/pkg/impl/fiber"
	"github.com/Netcracker/qubership-dbaas-adapter-core/pkg/service"
	"github.com/Netcracker/qubership-dbaas-adapter-core/pkg/utils"
	"github.com/docker/distribution/uuid"
	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func GetDefaultApp(
	dbaasClient *dbaas.Client,
	namespace string,
	appName string,
	apiUser string,
	apiPass string,
	aggregatorRegistrationLabels map[string]string,
	adapterAddress string,
	dbAdmin service.DbAdministration,
	backupAddress string,
	backupDaemonApiUser string,
	backupDaemonApiUPass string,
	backupFullRestore bool,
	httpClient utils.HttpClient,
	supports dao.SupportsBase,
	logger *zap.Logger,
	profiler bool,
	promServiceName string) (context.CancelFunc, *fiber.App, error) {
	appPath := "/" + appName

	administrationService := service.NewCoreAdministrationService(
		namespace,
		8080,
		dbAdmin,
		logger,
		false,
		nil,
		"",
	)
	return fiber2.GetFiberServer(func(app *fiber.App, ctx context.Context) error {
		fiber2.BuildFiberDBaaSAdapterHandlers(
			app,
			apiUser,
			apiPass,
			appPath,
			administrationService,
			service.NewPhysicalRegistrationService(
				appName,
				logger,
				appName,
				adapterAddress,
				dao.BasicAuth{
					Username: apiUser,
					Password: apiPass,
				},
				aggregatorRegistrationLabels,
				dbaasClient,
				150000,
				60000,
				5000,
				administrationService,
				ctx,
			),
			service.DefaultBackupAdministrationService(
				logger,
				backupAddress,
				backupDaemonApiUser,
				backupDaemonApiUPass,
				backupFullRestore,
				httpClient,
				64,
				nil),
			supports.ToMap(),
			logger,
			profiler,
			Simplstr())
		return nil
	})
}

func GetTestHttpBackupServer(user string, pass string) *httptest.Server {
	checkVault := "BACKUP0RREST0R1D0REV1CT"
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqUser, reqPass, parsed := r.BasicAuth()
		if !parsed || reqUser != user || reqPass != pass {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte("Not authorized"))
		}

		if r.Method == http.MethodPost {
			if r.Header.Get("Content-Type") != "application/json" {
				w.WriteHeader(http.StatusUnsupportedMediaType)
				w.Write([]byte("Content-Type is not valid"))
				return
			}
			// Backup and restore
			if strings.HasPrefix(r.URL.Path, "/backup") ||
				strings.HasPrefix(r.URL.Path, "/restore") ||
				strings.HasPrefix(r.URL.Path, "/evict") {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(checkVault))
				return
			} else {
				w.WriteHeader(http.StatusUnsupportedMediaType)
				w.Write([]byte("Unsupported"))
				return
			}
		} else if r.Method == http.MethodGet {
			// Track backup and restore
			if strings.HasPrefix(r.URL.Path, "/jobstatus/") {
				ss := strings.Split(r.URL.Path, "/")
				id := ss[len(ss)-1]
				if id == checkVault {
					w.WriteHeader(http.StatusOK)
					bt := dao.BackupTask{
						Vault:  id,
						Status: dao.SuccessfulBackupStatus,
						TaskId: id,
					}
					resp, _ := json.Marshal(bt)
					w.Write(resp)
					return
				} else {
					w.WriteHeader(http.StatusNotFound)
					w.Write([]byte("Not found"))
					return
				}
			} else {
				w.WriteHeader(http.StatusUnsupportedMediaType)
				w.Write([]byte("Unsupported"))
				return
			}
		}
	}))
}

func GetTestHttpAggregatorServer(user, pass, appName, regId string, additionalRoles bool) *httptest.Server {
	reg_url := fmt.Sprintf("/api/v3/dbaas/%s/physical_databases/%s", appName, regId)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqUser, reqPass, parsed := r.BasicAuth()

		if strings.HasPrefix(r.URL.Path, "/api-version") {
			w.WriteHeader(http.StatusOK)
			v3 := dao.DbaasAggregatorVersion{
				SupportedMajors: []int{3},
			}
			v3Json, err := json.Marshal(&v3)
			if err != nil {
				panic(err)
			}
			w.Write(v3Json)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/health") {
			w.WriteHeader(http.StatusOK)
			return
		}
		if !parsed || reqUser != user || reqPass != pass {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte("Not authorized"))
			return
		}
		if r.Header.Get("Content-Type") != "application/json" {
			w.WriteHeader(http.StatusUnsupportedMediaType)
			return
		} else if additionalRoles && r.URL.Path == reg_url {
			id := Simplstr()
			roleId := Simplstr()
			dbName := Simplstr()
			responseBody := dao.PhysicalDatabaseRegistrationResponse{
				Instruction: dao.Instruction{Id: id,
					AdditionalRoles: []dao.AdditionalRole{
						dao.AdditionalRole{Id: roleId,
							DbName: dbName,
							ConnectionProperties: []dao.ConnectionProperties{{
								"role":     "",
								"username": "",
							},
							}},
					}},
			}
			w.WriteHeader(http.StatusAccepted)
			jsonBody, _ := json.Marshal(responseBody)
			w.Write(jsonBody)
		} else if additionalRoles && strings.HasPrefix(r.URL.Path, "/additional-roles") {
			w.WriteHeader(http.StatusOK)
			return
		} else {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("{}"))
		}
	}))
}

func GetFailedRegistrationAggregatorServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api-version") {
			w.WriteHeader(http.StatusOK)
			v3 := dao.DbaasAggregatorVersion{
				SupportedMajors: []int{3},
			}
			v3Json, err := json.Marshal(&v3)
			if err != nil {
				panic(err)
			}
			w.Write(v3Json)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/health") {
			w.WriteHeader(http.StatusOK)
			return
		}

		w.WriteHeader(http.StatusInternalServerError)
	}))
}

func Simplstr() string {
	return utils.Substr(uuid.Generate().String(), 0, 7)
}

func UseFullFeaturedConfig(logger *zap.Logger, t *testing.T, app *fiber.App, version, appName,
	adapterApiUser, adapterApiPass string, testUsers bool, testBackup bool, backupApiUser, backupApiPass string) {

	defer app.Server().Shutdown()

	logger.Debug("Fiber App is initialized. Running requests to handlers...")

	defautRoute := fmt.Sprintf(dao.DefaultRouteFormat, dao.RootUrl, version)
	var resp *http.Response
	var respErr error
	appPath := defautRoute + "/" + appName

	// Handlers without auth
	resp, respErr = HandlerTest(logger, app,
		http.MethodGet,
		"/health",
		nil)
	assert.Equal(t, nil, respErr)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	resp, respErr = HandlerTest(logger, app,
		http.MethodGet,
		appPath+"/supports",
		nil)
	assert.Equal(t, nil, respErr)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	resp, respErr = HandlerTest(logger, app,
		http.MethodGet,
		defautRoute+"/physical_database/force_registration",
		nil)
	assert.Equal(t, nil, respErr)
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)

	// Check Auth Working
	resp, respErr = HandlerTest(logger, app,
		http.MethodPost,
		appPath+"/databases",
		nil,
		adapterApiUser,
		"wrongPass")
	assert.Equal(t, nil, respErr)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	// Basic Auth
	resp, respErr = HandlerTest(logger, app,
		http.MethodPost,
		appPath+"/databases",
		nil,
		adapterApiUser,
		adapterApiPass)
	assert.Equal(t, nil, respErr)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)
	createdDbBody, _ := io.ReadAll(resp.Body)
	var createdDb dao.DbCreateResponse
	json.Unmarshal(createdDbBody, &createdDb)
	resp.Body.Close()
	createdDbName := createdDb.Name
	createdResources := createdDb.Resources

	resp, respErr = HandlerTest(logger, app,
		http.MethodGet,
		appPath+"/databases",
		nil,
		adapterApiUser,
		adapterApiPass)
	assert.Equal(t, nil, respErr)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	dbsBody, _ := io.ReadAll(resp.Body)
	var dbsGetList []string
	json.Unmarshal(dbsBody, &dbsGetList)
	resp.Body.Close()
	assert.Contains(t, dbsGetList, createdDbName)

	resp, respErr = HandlerTest(logger, app,
		http.MethodPut,
		appPath+"/databases/"+createdDbName+"/metadata",
		map[string]any{"newMeta": "newMetaValue"},
		adapterApiUser,
		adapterApiPass)
	assert.Equal(t, nil, respErr)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	resp, respErr = HandlerTest(logger, app,
		http.MethodPost,
		appPath+"/describe/databases",
		[]string{createdDbName},
		adapterApiUser,
		adapterApiPass)
	assert.Equal(t, nil, respErr)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	describedDbsBody, _ := io.ReadAll(resp.Body)
	var describedDbs map[string]dao.LogicalDatabaseDescribed
	json.Unmarshal(describedDbsBody, &describedDbs)
	resp.Body.Close()
	assert.True(t, len(describedDbs) > 0)

	for _, resource := range createdResources {
		resp, respErr = HandlerTest(logger, app,
			http.MethodPost,
			appPath+"/resources/bulk-drop",
			[]dao.DbResource{
				{
					Kind: resource.Kind,
					Name: resource.Name,
				},
			},
			adapterApiUser,
			adapterApiPass)
		assert.Equal(t, nil, respErr)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	}

	resp, respErr = HandlerTest(logger, app,
		http.MethodGet,
		appPath+"/physical_database",
		nil,
		adapterApiUser,
		adapterApiPass)
	assert.Equal(t, nil, respErr)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	phyDbInfoBody, _ := io.ReadAll(resp.Body)
	var phyDbInfo service.PhysicalDatabase
	json.Unmarshal(phyDbInfoBody, &phyDbInfo)
	resp.Body.Close()
	assert.Equal(t, appName, phyDbInfo.Id)

	if testUsers {
		resp, respErr = HandlerTest(logger, app,
			http.MethodPut,
			appPath+"/users",
			nil,
			adapterApiUser,
			adapterApiPass)
		assert.Equal(t, nil, respErr)
		assert.Equal(t, http.StatusCreated, resp.StatusCode)
		createdUserWONameBody, _ := io.ReadAll(resp.Body)
		var createdUserWOName dao.CreatedUser
		json.Unmarshal(createdUserWONameBody, &createdUserWOName)
		resp.Body.Close()
		assert.NotEqual(t, "", createdUserWOName.Name)

		generatedUserName := Simplstr()
		resp, respErr = HandlerTest(logger, app,
			http.MethodPut,
			appPath+"/users/"+generatedUserName,
			nil,
			adapterApiUser,
			adapterApiPass)
		assert.Equal(t, nil, respErr)
		assert.Equal(t, http.StatusCreated, resp.StatusCode)
		createdUserWNameBody, _ := io.ReadAll(resp.Body)
		var createdUserWName dao.CreatedUser
		json.Unmarshal(createdUserWNameBody, &createdUserWName)
		resp.Body.Close()
		assert.Equal(t, generatedUserName, createdUserWName.Name)
	}

	// Backups
	if testBackup {
		backupPath := appPath + "/backups"
		resp, respErr = HandlerTest(logger, app,
			http.MethodPost,
			backupPath+"/collect",
			[]string{createdDbName},
			adapterApiUser,
			adapterApiPass)
		assert.Equal(t, nil, respErr)
		assert.Equal(t, http.StatusAccepted, resp.StatusCode)
		backupCollectBody, _ := io.ReadAll(resp.Body)
		var backupCollect dao.DatabaseAdapterBaseTrack
		json.Unmarshal(backupCollectBody, &backupCollect)
		resp.Body.Close()
		assert.NotEqual(t, "", backupCollect.TrackId)
		assert.Equal(t, dao.ProceedingTrackStatus, backupCollect.Status)

		resp, respErr = HandlerTest(logger, app,
			http.MethodGet,
			backupPath+"/track/backup/"+backupCollect.TrackId,
			nil,
			adapterApiUser,
			adapterApiPass)
		assert.Equal(t, nil, respErr)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		backupTrackBody, _ := io.ReadAll(resp.Body)
		var backupTrack dao.DatabaseAdapterBaseTrack
		json.Unmarshal(backupTrackBody, &backupTrack)
		resp.Body.Close()
		assert.Equal(t, dao.SuccessTrackStatus, backupTrack.Status)

		resp, respErr = HandlerTest(logger, app,
			http.MethodPost,
			backupPath+"/"+backupCollect.TrackId+"/restore",
			[]string{createdDbName},
			adapterApiUser,
			adapterApiPass)
		assert.Equal(t, nil, respErr)
		assert.Equal(t, http.StatusAccepted, resp.StatusCode)
		restoreBody, _ := io.ReadAll(resp.Body)
		var restore dao.DatabaseAdapterRestoreTrack
		json.Unmarshal(restoreBody, &restore)
		resp.Body.Close()
		assert.NotEqual(t, "", restore.TrackId)
		assert.Equal(t, dao.ProceedingTrackStatus, restore.Status)

		resp, respErr = HandlerTest(logger, app,
			http.MethodGet,
			backupPath+"/track/restore/"+restore.TrackId,
			nil,
			adapterApiUser,
			adapterApiPass)
		assert.Equal(t, nil, respErr)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		restoreTrackBody, _ := io.ReadAll(resp.Body)
		var restoreTrack dao.DatabaseAdapterBaseTrack
		json.Unmarshal(restoreTrackBody, &restoreTrack)
		resp.Body.Close()
		assert.Equal(t, dao.SuccessTrackStatus, backupTrack.Status)

		resp, respErr = HandlerTest(logger, app,
			http.MethodDelete,
			backupPath+"/"+backupCollect.TrackId,
			nil,
			adapterApiUser,
			adapterApiPass)
		assert.Equal(t, nil, respErr)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	}

}

func HandlerTest(logger *zap.Logger, app *fiber.App, method string, target string, bodyStruct any, creds ...string) (resp *http.Response, err error) {
	var req *http.Request
	if method == http.MethodPost || method == http.MethodPut {
		codedBody, errm := json.Marshal(bodyStruct)
		utils.PanicError(errm, logger.Error, "Failed to marshal test request body")
		req = httptest.NewRequest(method, target, bytes.NewReader(codedBody))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	if creds != nil && len(creds) >= 2 {
		req.SetBasicAuth(creds[0], creds[1])
	}
	return app.Test(req)
}

type AppCredentials struct {
	AppName           string
	AdapterApiUser    string
	AdapterApiPass    string
	BackupApiUser     string
	BackupApiPass     string
	AggregatorApiUser string
	AggregatorApiPass string
}

func PrepateTestApp(dbaasClient *dbaas.Client, logger *zap.Logger, dbAdmin service.DbAdministration,
	testApp AppCredentials, backupAddress string) (context.CancelFunc, *fiber.App, error, AppCredentials) {
	logger.Debug("Setting up test environment...")

	namespace := Simplstr()

	adapterAddress := Simplstr() + ".svc:8080"

	supports := dao.SupportsBase{
		Users:             true,
		Settings:          false,
		DescribeDatabases: true,
	}

	logger.Debug("Variables are generated...")

	aggRegLables := map[string]string{
		Simplstr(): Simplstr(),
	}

	logger.Debug("Created Aggregator Test Server Handlers...")

	c, a, e := GetDefaultApp(
		dbaasClient,
		namespace,
		testApp.AppName,
		testApp.AdapterApiUser,
		testApp.AdapterApiPass,
		aggRegLables,
		adapterAddress,
		dbAdmin,
		backupAddress,
		testApp.BackupApiUser,
		testApp.BackupApiPass,
		false,
		nil,
		supports,
		logger,
		false,
		Simplstr())

	return c, a, e, testApp
}
