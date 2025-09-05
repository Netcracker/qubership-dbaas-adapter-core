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
	"sync"
	"time"

	entity "github.com/Netcracker/qubership-dbaas-adapter-core/pkg/dao"
	"github.com/Netcracker/qubership-dbaas-adapter-core/pkg/dbaas"
	"github.com/Netcracker/qubership-dbaas-adapter-core/pkg/helper"
	"go.uber.org/zap"
)

type PhysicalDatabaseRegistrationService struct {
	dbName                 string
	logger                 *zap.Logger
	phydbid                string
	adapterAddress         string
	basicAdapterAuth       entity.BasicAuth
	labels                 map[string]string
	client                 *dbaas.Client
	Health                 entity.PhysicalDatabaseRegistrationHealth
	registrationFixedDelay int
	registrationRetryTime  int
	registrationRetryDelay int
	administrationService  CoreAdministrationServiceIface

	// mutex is used to synchronize concurrent registrations.
	mutex       sync.Mutex
	executor    *helper.BackgroundExecutor
	loopContext context.Context
	status      entity.Status
}

func NewPhysicalRegistrationService(
	dbName string,
	logger *zap.Logger,
	phydbid string,
	adapterAddress string,
	basicAdapterAuth entity.BasicAuth,
	labels map[string]string,
	dbaasClient *dbaas.Client,
	registrationFixedDelay int,
	registrationRetryTime int,
	registrationRetryDelay int,
	administrationService CoreAdministrationServiceIface,
	context context.Context,
) *PhysicalDatabaseRegistrationService {

	return &PhysicalDatabaseRegistrationService{
		dbName:                 dbName,
		logger:                 logger,
		phydbid:                phydbid,
		adapterAddress:         adapterAddress,
		basicAdapterAuth:       basicAdapterAuth,
		labels:                 labels,
		client:                 dbaasClient,
		Health:                 entity.PhysicalDatabaseRegistrationHealth{Status: "UNKNOWN"},
		registrationFixedDelay: registrationFixedDelay,
		registrationRetryTime:  registrationRetryTime,
		registrationRetryDelay: registrationRetryDelay,
		executor:               helper.NewBackgroundExecutor(),
		administrationService:  administrationService,
		loopContext:            context,
		status:                 entity.StatusRunning,
	}
}

func (srv *PhysicalDatabaseRegistrationService) StartRegister() {
	if srv.administrationService.GetVersion() == "v1" {
		go srv.registerPeriodically(srv.Register)
	} else {
		go srv.registerPeriodically(srv.registerWithRoles)
	}
}

func (srv *PhysicalDatabaseRegistrationService) registerPeriodically(regFunc func()) {
	for {
		select {
		case <-srv.loopContext.Done():
			srv.logger.Info("Periodical registration is finished")
			return
		default:
			regFunc()
			nextTime := time.Now().Truncate(time.Millisecond).Add(time.Duration(srv.registrationFixedDelay) * time.Millisecond)
			time.Sleep(time.Until(nextTime))
		}
	}
}

// DEPRECATED - v1
// Register performs one physical database registration attempt and sets the corresponding health status
// depending on the result.
func (srv *PhysicalDatabaseRegistrationService) Register() {
	defer func() {
		if r := recover(); r != nil {
			srv.logger.Warn(fmt.Sprintf("Recovered from physical database registration panic, set health WARNING: %+v", r))
			srv.Health = entity.PhysicalDatabaseRegistrationHealth{Status: "WARNING"}
		} else {
			srv.logger.Info("Successfully registered physical database, set health OK")
			srv.Health = entity.PhysicalDatabaseRegistrationHealth{Status: "OK"}
		}
	}()
	defer srv.mutex.Unlock()

	srv.mutex.Lock()
	srv.sendRegisterRequest()
}

// RegisterWithRetry performs attempts to register physical database in DBaaS during the retryTimeSec.
// If one attempt fails, next attempt is being performed after the retryDelaySec seconds.
// Health status is being updated after the each registration attempt.
func (srv *PhysicalDatabaseRegistrationService) RegisterWithRetry() {
	defer func() {
		if r := recover(); r != nil {
			srv.logger.Warn(fmt.Sprintf("Recovered from force physical database registration panic, set health PROBLEM: %+v", r))
			srv.Health = entity.PhysicalDatabaseRegistrationHealth{Status: "PROBLEM"}
		}
	}()
	defer srv.mutex.Unlock()

	srv.mutex.Lock()

	nextTime := time.Now().Truncate(time.Millisecond)
	lastTime := nextTime.Add(time.Duration(srv.registrationRetryTime) * time.Millisecond)

	for !srv.registerAndReturnResult() {
		nextTime = nextTime.Add(time.Duration(srv.registrationRetryDelay) * time.Millisecond)
		if nextTime.Before(lastTime) || nextTime.Equal(lastTime) {
			time.Sleep(time.Until(nextTime))
		} else {
			srv.logger.Warn("Force physical db registration has failed.")
			return
		}
	}
	srv.logger.Info("Force physical db registration finished successfully.")
}

// sendRegisterRequest sends HTTP request to register physical database in DBaaS.
// Causes panic in case of the registration fail.
func (srv *PhysicalDatabaseRegistrationService) sendRegisterRequest() entity.PhysicalDatabaseRegistrationResponse {
	body := entity.PhysicalDatabaseRegistrationRequest{
		AdapterAddress:       srv.adapterAddress,
		HttpBasicCredentials: srv.basicAdapterAuth,
		Labels:               srv.labels,
	}
	srv.modifyReqParams(&body)

	response, err := srv.client.PhysicalDatabaseRegistration(srv.dbName, srv.phydbid, body)
	if srv.administrationService.GetVersion() == "v1" {
		return entity.PhysicalDatabaseRegistrationResponse{}
	}

	if err != nil {
		panic(err)
	}
	srv.logger.Debug(fmt.Sprintf("Successful physical database registration"))
	return *response
}

// registerAndReturnResult send the physical database registration request and updates health status depending on the
// registration result. Function returns true in case of successful registration, false otherwise.
func (srv *PhysicalDatabaseRegistrationService) registerAndReturnResult() bool {
	defer func() bool {
		if r := recover(); r != nil {
			srv.logger.Warn(fmt.Sprintf("Recovered from force physical database registration panic, set health PROBLEM: %+v", r))
			srv.Health = entity.PhysicalDatabaseRegistrationHealth{Status: "PROBLEM"}
			return false
		} else {
			srv.logger.Info("Successfully registered physical database, set health OK")
			srv.Health = entity.PhysicalDatabaseRegistrationHealth{Status: "OK"}
			return true
		}
	}()
	srv.sendRegisterRequest()
	return true
}

type PhysicalDatabase struct {
	Labels map[string]string `json:"labels,omitempty"`
	Id     string            `json:"id"`
}

func (srv *PhysicalDatabaseRegistrationService) GetPhysicalDatabase() *PhysicalDatabase {
	if srv.phydbid == "" {
		return nil
	}
	return &PhysicalDatabase{
		Labels: srv.labels,
		Id:     srv.phydbid,
	}
}

func (srv *PhysicalDatabaseRegistrationService) ForceRegistration() {
	if srv.administrationService.GetVersion() != "v1" {
		srv.logger.Warn(fmt.Sprintf("Force registration not supported for API %s", srv.administrationService.GetVersion()))
		return
	}
	srv.executor.Submit(srv.RegisterWithRetry)
}
