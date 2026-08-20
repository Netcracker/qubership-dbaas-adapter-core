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

package dbaas_adapter_core

import (
	"context"
	"testing"

	"github.com/Netcracker/qubership-dbaas-adapter-core/pkg/dao"
	"github.com/Netcracker/qubership-dbaas-adapter-core/pkg/dbaas"
	"github.com/Netcracker/qubership-dbaas-adapter-core/pkg/service"
	"github.com/Netcracker/qubership-dbaas-adapter-core/pkg/utils"
	testing2 "github.com/Netcracker/qubership-dbaas-adapter-core/testing"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

type DbTestAdmin struct {
	logger  *zap.Logger
	version string
	dbs     map[string]struct {
		user string
		pass string
	}
}

func (d DbTestAdmin) CreateDatabase(ctx context.Context, requestOnCreateDb dao.DbCreateRequest) (string, *dao.LogicalDatabaseDescribed, error) {
	//Emulate strange conditions to raise exception
	if requestOnCreateDb.NamePrefix == nil || *requestOnCreateDb.NamePrefix != d.GetDBPrefix() {
		return "", nil, &utils.ExecutionError{Msg: "Prefix not valid"}
	}
	n := *requestOnCreateDb.NamePrefix + d.GetDBPrefixDelimiter() + requestOnCreateDb.DbName
	v := d.dbs[n]
	v.user = requestOnCreateDb.Username
	v.pass = requestOnCreateDb.Password
	d.dbs[n] = v
	return n, &dao.LogicalDatabaseDescribed{
		ConnectionProperties: []dao.ConnectionProperties{{
			n: v.user,
		}},
		Resources: []dao.DbResource{
			{
				Kind: "user",
				Name: v.user,
			},
			{
				Kind: "db",
				Name: n,
			},
		},
	}, nil
}

func (d DbTestAdmin) DescribeDatabases(ctx context.Context, logicalDatabases []string, resources bool, connections bool) map[string]dao.LogicalDatabaseDescribed {
	res := make(map[string]dao.LogicalDatabaseDescribed)
	for _, db := range logicalDatabases {
		v := d.dbs[db]
		res[db] = dao.LogicalDatabaseDescribed{
			ConnectionProperties: []dao.ConnectionProperties{{
				db: v.user,
			}},
			Resources: []dao.DbResource{
				{
					Kind: "user",
					Name: v.user,
				},
				{
					Kind: "db",
					Name: db,
				},
			},
		}
	}
	return res
}

func (d DbTestAdmin) GetDatabases(ctx context.Context) []string {
	var res []string
	for db, _ := range d.dbs {
		res = append(res, db)
	}
	return res
}

func (d DbTestAdmin) DropResources(ctx context.Context, resources []dao.DbResource) []dao.DbResource {
	var rsrs []dao.DbResource
	for _, r := range resources {
		r.Status = dao.DELETED
		rsrs = append(rsrs)
	}
	return rsrs
}

func (d DbTestAdmin) GetMetadata(ctx context.Context, logicalDatabase string) map[string]any {
	d.logger.Debug("Get metadata. Return empty")
	return map[string]any{}
}

func (d DbTestAdmin) UpdateMetadata(ctx context.Context, newMetadata map[string]any, logicalDatabases string) {
	d.logger.Debug("Updated Metadata void")
}

func (d DbTestAdmin) GetDefaultCreateRequest() dao.DbCreateRequest {
	prefix := d.GetDBPrefix()
	return dao.DbCreateRequest{
		Metadata:   nil,
		NamePrefix: &prefix,
		Password:   testing2.Simplstr(),
		DbName:     testing2.Simplstr(),
		Settings:   nil,
		Username:   testing2.Simplstr(),
	}
}

func (d DbTestAdmin) GetDefaultUserCreateRequest() dao.UserCreateRequest {
	return dao.UserCreateRequest{

		Password: testing2.Simplstr(),
	}
}

func (d DbTestAdmin) PreStart() {
	d.logger.Debug("Prestart void")
}

func (d DbTestAdmin) CreateUser(ctx context.Context, userName string, requestOnCreateUser dao.UserCreateRequest) (*dao.CreatedUser, error) {
	if userName == "" {
		userName = testing2.Simplstr()
	}
	return &dao.CreatedUser{
		ConnectionProperties: dao.ConnectionProperties{
			requestOnCreateUser.DbName: userName,
			"password":                 requestOnCreateUser.Password,
		},
		Resources: []dao.DbResource{
			{
				Kind: "user",
				Name: userName,
			},
			{
				Kind: "db",
				Name: requestOnCreateUser.DbName,
			},
		},
		Name: userName,
	}, nil
}

func (d DbTestAdmin) MigrateToVault(ctx context.Context, dbName, userName string) error {
	return nil
}

func (d DbTestAdmin) GetDBPrefix() string {
	return "testcheckpref"
}

func (d DbTestAdmin) GetDBPrefixDelimiter() string {
	return "-"
}

func (d DbTestAdmin) GetVersion() dao.ApiVersion {
	return dao.ApiVersion(d.version)
}

func (d DbTestAdmin) CreateDatabaseWithRoles(requestOnCreateDb dao.DbCreateRequest) (string, *dao.DbCreateResponseMultiUser, error) {
	return "", nil, nil
}

func (d DbTestAdmin) GetSupportedRoles() []string {
	return make([]string, 0)
}

func (d DbTestAdmin) GetFeatures() map[string]bool {
	return map[string]bool{}
}

func (d DbTestAdmin) GetROHost() string {
	return ""
}

func (d DbTestAdmin) CreateRoles(ctx context.Context, roles []dao.AdditionalRole) ([]dao.Success, *dao.Failure) {
	return []dao.Success{}, nil
}

var _ service.DbAdministration = &DbTestAdmin{}

func Test_FullFeaturedConfigV2(t *testing.T) {
	logger := utils.GetLogger(true)

	dbAdminV2 := DbTestAdmin{
		version: "v2",
		logger:  logger,
		dbs: make(map[string]struct {
			user string
			pass string
		}),
	}

	appCredentials := testing2.AppCredentials{
		AppName:           testing2.Simplstr(),
		AdapterApiUser:    testing2.Simplstr(),
		AdapterApiPass:    testing2.Simplstr(),
		BackupApiUser:     testing2.Simplstr(),
		BackupApiPass:     testing2.Simplstr(),
		AggregatorApiUser: testing2.Simplstr(),
		AggregatorApiPass: testing2.Simplstr(),
	}

	backupServer := testing2.GetTestHttpBackupServer(appCredentials.BackupApiUser, appCredentials.BackupApiPass)
	defer backupServer.Close()
	backupAddress := backupServer.URL

	logger.Debug("Created Backup Test Server Handlers...")

	aggregatorServer := testing2.GetTestHttpAggregatorServer(appCredentials.AggregatorApiUser, appCredentials.AggregatorApiPass, appCredentials.AppName, appCredentials.AppName, true)
	defer aggregatorServer.Close()
	aggAddress := aggregatorServer.URL

	dbaasClient, err := dbaas.NewDbaasClient(aggAddress, &dao.BasicAuth{appCredentials.AggregatorApiUser, appCredentials.AggregatorApiPass}, nil)
	if err != nil {
		assert.Fail(t, "Failed to create Dbaas Client", err)
	}

	cancelFunc, testApp, appErr, appCredentials := testing2.PrepateTestApp(dbaasClient, logger, dbAdminV2, appCredentials, backupAddress)
	if appErr != nil {
		logger.Error("Error during app initialization")
		if cancelFunc != nil {
			cancelFunc()
		}
		assert.Fail(t, "Failed initializing app", appErr)
	}

	testing2.UseFullFeaturedConfig(logger, t, testApp, string(dbAdminV2.GetVersion()),
		appCredentials.AppName, appCredentials.AdapterApiUser, appCredentials.AdapterApiPass, true, true,
		appCredentials.BackupApiUser, appCredentials.BackupApiPass)
}
