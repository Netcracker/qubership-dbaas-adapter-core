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

package fiber

// To generate swagger spec run: swag init -g pkg/impl/fiber/handlers.go

import (
	"context"
	"fmt"
	"runtime/debug"
	"strconv"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"

	"sync"

	_ "github.com/Netcracker/qubership-dbaas-adapter-core/docs"
	dto "github.com/Netcracker/qubership-dbaas-adapter-core/pkg/dao"
	"github.com/Netcracker/qubership-dbaas-adapter-core/pkg/service"
	"github.com/ansrivas/fiberprometheus/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/basicauth"
	"github.com/gofiber/fiber/v2/middleware/pprof"
	"github.com/gofiber/fiber/v2/middleware/recover"
	utils "github.com/gofiber/fiber/v2/utils"
	"github.com/gofiber/swagger"
	"go.uber.org/zap"
)

func checkIfParamExistsOrDefault(c *fiber.Ctx, param string, emptyValue string, defaultValue string) string {
	if c.Request().URI().QueryArgs().Has(param) {
		// Check possibly empty value of query arg
		if len(c.Request().URI().QueryArgs().Peek(param)) == 0 {
			return emptyValue
		}
		return c.Query(param)
	}
	return defaultValue
}

type DbaasAdapterHandler struct {
	backupPath      string
	adminService    service.CoreAdministrationServiceIface
	physicalService *service.PhysicalDatabaseRegistrationService
	backupService   service.BackupAdministrationService
	logger          *zap.Logger
}

// GetDatabases godoc
// @Tags Common dbaas adapter operations
// @Summary Force physical database registration
// @Description Force this adapter to immediately register itself in dbaas-aggregator.
// @Description Adapter initiates background task that tries to register physical database in dbaas-aggregator,
// @Description and responds with status 202 before the background task finishes.
// @Param apiVersion path string true "API version of dbaas adapter" Enums(v1, v2) default(v2)
// @Success 202 {string} Token "if physical database registration process has been started successfully."
// @Router /physical_database/force_registration [get]
func (h *DbaasAdapterHandler) ForceRegistration(c *fiber.Ctx) error {
	h.physicalService.ForceRegistration()
	return c.SendStatus(fiber.StatusAccepted)
}

// GetDatabases godoc
// @Tags Database administration
// @Summary List of all databases
// @Description Returns list with names of databases
// @Produce  json
// @Success 200 {object} []string "Databases listed"
// @Failure 500 {string} Token "Error occurred while databases listing."
// @Param appName path string true "Application name" Enums(postgresql, arangodb, clickhouse, mongodb, cassandra) default(postgresql)
// @Param apiVersion path string true "API version of dbaas adapter" Enums(v1, v2) default(v2)
// @Router /{appName}/databases [get]
func (h *DbaasAdapterHandler) GetDatabases(c *fiber.Ctx) error {
	//Get databases list
	ctx := getRequestContext(c)
	return c.JSON(h.adminService.GetDatabases(ctx))
}

// CreateDatabase godoc
// @Tags Database administration
// @Summary Create database
// @Description Creates database with one user having readWrite role in it and returns connection parameters including credentials.
// @Description Also in created database provided metadata being inserted in _dbaas_metadata collection.
// @Accept   json
// @Produce  json
// @Param appName path string true "Application name" Enums(postgresql, arangodb, clickhouse, mongodb, cassandra) default(postgresql)
// @Param apiVersion path string true "API version of dbaas adapter" Enums(v1, v2) default(v2)
// @Param body body dto.DbCreateRequest true "Create DB body request"
// @Success 201 {object} dto.DbCreateResponseMultiUser
// @Failure 400 {string} Token "Provided parameters does not meet the requirements"
// @Router /{appName}/databases [post]
func (h *DbaasAdapterHandler) CreateDatabase(c *fiber.Ctx) error {
	//Create database
	requestDb := h.adminService.GetDefaultCreateRequest()
	if len(c.Body()) > 0 {
		parserErr := c.BodyParser(&requestDb)
		if parserErr != nil {
			return parserErr
		}
	}
	ctx := getRequestContext(c)
	response, createErr := h.adminService.CreateDatabase(ctx, requestDb)
	if createErr != nil {
		h.logger.Info(fmt.Sprintf("Coud not create database: %s", createErr))
		return c.Status(fiber.StatusBadRequest).SendString(createErr.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(&response)
}

// bulk-drop godoc
// @Tags Database administration
// @Summary Drop created resources
// @Description Can drop any previously created resources such as user or database
// @Accept   json
// @Produce  json
// @Param appName path string true "Application name" Enums(postgresql, arangodb, clickhouse, mongodb, cassandra) default(postgresql)
// @Param apiVersion path string true "API version of dbaas adapter" Enums(v1, v2) default(v2)
// @Param body body []dto.DbResource true "List of resources to drop"
// @Success 200 {object} []dto.DbResource "Drop successful"
// @Router /{appName}/resources/bulk-drop [post]
func (h *DbaasAdapterHandler) BulkDrop(c *fiber.Ctx) error {
	//Delete resources
	var resources []dto.DbResource
	parserErr := c.BodyParser(&resources)
	if parserErr != nil {
		return parserErr
	}
	ctx := getRequestContext(c)
	response, dropErr := h.adminService.DropResources(ctx, resources)
	if dropErr {
		c.Status(fiber.StatusInternalServerError)
	}
	return c.JSON(&response)
}

// DescribeDatabases godoc
// @Tags Database administration
// @Summary Describe databases
// @Description Returns info about requested databases, this is optional API
// @Accept   json
// @Produce  json
// @Param appName path string true "Application name" Enums(postgresql, arangodb, clickhouse, mongodb, cassandra) default(postgresql)
// @Param apiVersion path string true "API version of dbaas adapter" Enums(v1, v2) default(v2)
// @Param resources query bool false "If present - should return connection properties of each requested database"
// @Param connectionProperties query bool false "If present - should return resources of each requested database"
// @Param body body []string true "List of names of databases to describe"
// @Success 200 {object} map[string]dto.LogicalDatabaseDescribed
// @Failure 500 {string} Token "Error occurred while databases describe."
// @Router /{appName}/describe/databases [post]
func (h *DbaasAdapterHandler) DescribeDatabases(c *fiber.Ctx) error {
	var databases []string
	if len(c.Body()) > 0 {
		parserErr := c.BodyParser(&databases)
		if parserErr != nil {
			return parserErr
		}
	}
	ctx := getRequestContext(c)
	showResources, _ := strconv.ParseBool(checkIfParamExistsOrDefault(c, "resources", "true", "false"))
	showConnectionProperties, _ := strconv.ParseBool(checkIfParamExistsOrDefault(c, "connectionProperties", "true", "false"))
	response := h.adminService.DescribeDatabases(ctx, databases, showResources, showConnectionProperties)
	return c.JSON(&response)
}

// UpdateMetadata godoc
// @Tags Database administration
// @Summary Update database metadata
// @Description Changes metadata saved in databaase
// @Accept   json
// @Produce  json
// @Param appName path string true "Application name" Enums(postgresql, arangodb, clickhouse, mongodb, cassandra) default(postgresql)
// @Param apiVersion path string true "API version of dbaas adapter" Enums(v1, v2) default(v2)
// @Param dbName path string true "Databases to update"
// @Param body body map[string]interface{} true "New metadata"
// @Success 200 {string} Token "Update metadata was successful"
// @Failure 500 {string} Token "Error was occurred during update metadata."
// @Router /{appName}/databases/{dbName}/metadata [put]
func (h *DbaasAdapterHandler) UpdateMetadata(c *fiber.Ctx) error {
	// Update DB Meta
	var newMetadata map[string]interface{}
	parseErr := c.BodyParser(&newMetadata)
	if parseErr != nil {
		return parseErr
	}
	dbName := c.Params("dbName")
	ctx := getRequestContext(c)
	h.adminService.UpdateMetadata(ctx, newMetadata, dbName)
	return c.SendStatus(fiber.StatusOK)
}

func createUser(userName string, adminService service.CoreAdministrationServiceIface, c *fiber.Ctx) error {
	createUserRequest := adminService.GetDefaultUserCreateRequest()
	if len(c.Body()) > 0 {
		parserErr := c.BodyParser(&createUserRequest)
		if parserErr != nil {
			return parserErr
		}
	}
	ctx := getRequestContext(c)
	createdUser, createErr := adminService.CreateUser(ctx, userName, createUserRequest)
	if createErr != nil {
		return c.Status(fiber.StatusBadRequest).SendString(createErr.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(&createdUser)
}

// Create user godoc
// @Tags Database administration
// @Summary Create user
// @Description Creates new user for specified database and returns it with connection information,
// @Description or returns already created user if it exists. If database not specified will be used default database.
// @Accept   json
// @Produce  json
// @Param appName path string true "Application name" Enums(postgresql, arangodb, clickhouse, mongodb, cassandra) default(postgresql)
// @Param apiVersion path string true "API version of dbaas adapter" Enums(v1, v2) default(v2)
// @Param body body dto.UserCreateRequest true "Info about user to create"
// @Success 201 {object} dto.CreatedUser "User created"
// @Failure 500 {string} Token "Error occurred while user creation."
// @Router /{appName}/users [put]
func (h *DbaasAdapterHandler) CreateNewUser(c *fiber.Ctx) error {
	return createUser("", h.adminService, c)
}

// Create user godoc
// @Tags Database administration
// @Summary Create user
// @Description Creates new user for specified database and returns it with connection information,
// @Description or returns already created user if it exists. If database not specified will be used default database.
// @Accept   json
// @Produce  json
// @Param appName path string true "Application name" Enums(postgresql, arangodb, clickhouse, mongodb, cassandra) default(postgresql)
// @Param apiVersion path string true "API version of dbaas adapter" Enums(v1, v2) default(v2)
// @Param name path string true "The username for creating user to database"
// @Param body body dto.UserCreateRequest true "Info about user to ensure"
// @Success 201 {object} dto.CreatedUser
// @Failure 500 {string} Token "Error occurred while user creation."
// @Router /{appName}/users/{name} [put]
func (h *DbaasAdapterHandler) CreateUser(c *fiber.Ctx) error {
	userName := c.Params("name")
	return createUser(userName, h.adminService, c)
}

// Physical database registration user godoc
// @Tags Database administration
// @Summary Physical database information
// @Description Adapter belongs only one database cluster and send own physical database information
// @Accept   json
// @Produce  json
// @Param appName path string true "Application name" Enums(postgresql, arangodb, clickhouse, mongodb, cassandra) default(postgresql)
// @Param apiVersion path string true "API version of dbaas adapter" Enums(v1, v2) default(v2)
// @Success 200 {object} service.PhysicalDatabase "Own physical database information"
// @Failure 404 {string} Token "Identifier is not specified"
// @Router /{appName}/physical_database [get]
func (h *DbaasAdapterHandler) PhysicalRegistration(c *fiber.Ctx) error { //TODO Check in dbaas adapter
	phydb := h.physicalService.GetPhysicalDatabase()
	if phydb == nil {
		return c.Status(fiber.StatusNotFound).SendString("Physical database identifier not specified")
	}
	return c.JSON(&phydb)

}

// Collect backup godoc
// @Tags Backup and Restore
// @Summary Collect backup
// @Description Requests database backup daemon to collect backup for specified databases
// @Accept  json
// @Produce json
// @Param body body []string true "Databases to backup"
// @Param appName path string true "Application name" Enums(postgresql, arangodb, clickhouse, mongodb, cassandra) default(postgresql)
// @Param apiVersion path string true "API version of dbaas adapter" Enums(v1, v2) default(v2)
// @Success 202 {object} dto.DatabaseAdapterBaseTrack
// @Failure 500 {string} Token "Unknown error"
// @Router /{appName}/backups/collect [post]
func (h *DbaasAdapterHandler) Collect(c *fiber.Ctx) error {
	var databases []string
	parserErr := c.BodyParser(&databases)
	if parserErr != nil {
		return parserErr
	}
	ctx := getRequestContext(c)
	allowEviction, _ := strconv.ParseBool(checkIfParamExistsOrDefault(c, "allowEviction", "true", "true"))
	keepFromRequest := checkIfParamExistsOrDefault(c, "keep", "", "")
	h.logger.Debug(fmt.Sprintf("Requested to collect backup with %v databases specified. allowEviction = %v and keep =%s", len(databases), allowEviction, keepFromRequest))
	actionTrack := h.backupService.CollectBackup(ctx, databases, keepFromRequest, allowEviction)
	c.Location(locationPath(h.backupPath, "/track/backup/", actionTrack.TrackId))
	h.logger.Debug(fmt.Sprintf("Track: %+v", actionTrack))
	return c.Status(fiber.StatusAccepted).JSON(actionTrack)
}

// Track backup godoc
// @Tags Backup and Restore
// @Summary Track backup
// @Description Return status of backup task
// @Produce json
// @Param appName path string true "Application name" Enums(postgresql, arangodb, clickhouse, mongodb, cassandra) default(postgresql)
// @Param apiVersion path string true "API version of dbaas adapter" Enums(v1, v2) default(v2)
// @Param trackId path string true "trackId"
// @Success 200 {object} dto.DatabaseAdapterBaseTrack
// @Failure 500 {string} Token "Unknown error"
// @Router /{appName}/backups/track/backup/{trackId} [get]
func (h *DbaasAdapterHandler) TrackBackup(c *fiber.Ctx) error {
	trackId := c.Params("trackId")
	ctx := getRequestContext(c)
	track, found := h.backupService.TrackBackup(ctx, trackId)
	h.logger.Debug(fmt.Sprintf("Track: %+v", track))
	if !found {
		return c.Status(fiber.StatusNotFound).SendString("Backup process not found")
	}
	return c.JSON(track)
}

// Track restore godoc
// @Tags Backup and Restore
// @Summary Track restore
// @Description Return status of restore task
// @Produce json
// @Param appName path string true "Application name" Enums(postgresql, arangodb, clickhouse, mongodb, cassandra) default(postgresql)
// @Param apiVersion path string true "API version of dbaas adapter" Enums(v1, v2) default(v2)
// @Param trackId path string true "trackId"
// @Success 200 {object} dto.DatabaseAdapterBaseTrack
// @Failure 500 {string} Token "Unknown error"
// @Router /{appName}/backups/track/restore/{trackId} [get]
func (h *DbaasAdapterHandler) TrackRestore(c *fiber.Ctx) error {
	trackId := c.Params("trackId")
	ctx := getRequestContext(c)
	track, found := h.backupService.TrackRestore(ctx, trackId)
	h.logger.Debug(fmt.Sprintf("Track: %+v", track))
	if !found {
		return c.Status(fiber.StatusNotFound).SendString("Restore process not found")
	}
	return c.JSON(track)
}

// Restore backup godoc
// @Tags Backup and Restore
// @Summary Restore backup
// @Description Requests database backup daemon to restore specified backup with specified databases info
// @Accept   json
// @Produce  json
// @Param body body dto.RestorationRequest true "List of databases to restore"
// @Param appName path string true "Application name" Enums(postgresql, arangodb, clickhouse, mongodb, cassandra) default(postgresql)
// @Param apiVersion path string true "API version of dbaas adapter" Enums(v1, v2) default(v2)
// @Param backupId path string true "Backup identifier"
// @Success 202 {object} dto.DatabaseAdapterRestoreTrack "Restore requested"
// @Failure 500 {string} Token "Unknown error"
// @Failure 501 {string} Token "Cannot restore backup without explicitly specified list of databases in it"
// @Router /{appName}/backups/{backupId}/restoration [post]
func (h *DbaasAdapterHandler) Restoration(c *fiber.Ctx) error {
	var request dto.RestorationRequest
	parserErr := c.BodyParser(&request)
	if parserErr != nil {
		return parserErr
	}
	backupId := c.Params("backupId")
	ctx := getRequestContext(c)
	h.logger.Debug(fmt.Sprintf("Backup %v requested to be restored with %v databases specified, names regeneration = %v", backupId, len(request.Databases), request.RegenerateNames))
	actionTrack, trackErr := h.backupService.RestoreBackup(ctx, backupId, request.Databases, request.RegenerateNames, false)
	if trackErr != nil {
		if _, ok := trackErr.(*dto.BackupRestoresOnlySpecifiedDBsError); ok {
			return c.
				Status(fiber.StatusNotImplemented).
				SendString(trackErr.Error())
		} else {
			return c.
				Status(fiber.StatusInternalServerError).
				SendString(trackErr.Error())
		}
	}
	c.Location(locationPath(h.backupPath, "/track/restore/", actionTrack.TrackId))
	h.logger.Debug(fmt.Sprintf("Track: %+v", actionTrack))
	return c.Status(fiber.StatusAccepted).JSON(&actionTrack)
}

// Restore backup godoc
// @Tags Backup and Restore
// @Summary Restore backup
// @Description Requests database backup daemon to restore specified backup. Deprecated, please use /restoration endpoint instead
// @Accept   json
// @Produce  json
// @Param body body []dto.DbInfo true "List of database names for restore"
// @Param appName path string true "Application name" Enums(postgresql, arangodb, clickhouse, mongodb, cassandra) default(postgresql)
// @Param apiVersion path string true "API version of dbaas adapter" Enums(v1, v2) default(v2)
// @Param backupId path string true "Backup identifier"
// @Param regenerateNames query bool false "If this parameter has value true then restored databases will have new names and will be passed through associative array changedNameDb in response object"
// @Success 202 {object} dto.DatabaseAdapterRestoreTrack
// @Failure 500 {string} Token "Unknown error"
// @Failure 501 {string} Token "Cannot restore backup without explicitly specified list of databases in it"
// @Router /{appName}/backups/{backupId}/restore [post]
// @Deprecated
func (h *DbaasAdapterHandler) Restore(c *fiber.Ctx) error {
	var databases []string
	parserErr := c.BodyParser(&databases)
	if parserErr != nil {
		return parserErr
	}
	backupId := c.Params("backupId")
	ctx := getRequestContext(c)
	regenerateNames, _ := strconv.ParseBool(checkIfParamExistsOrDefault(c, "regenerateNames", "false", "false"))
	h.logger.Debug(fmt.Sprintf("Backup %v requested to be restored with %v databases specified, names regeneration = %v", backupId, len(databases), regenerateNames))
	dbInfos := []dto.DbInfo{}
	for _, database := range databases {
		dbInfos = append(dbInfos, dto.DbInfo{Name: database})
	}

	actionTrack, trackErr := h.backupService.RestoreBackup(ctx, backupId, dbInfos, regenerateNames, true)
	if trackErr != nil {
		if _, ok := trackErr.(*dto.BackupRestoresOnlySpecifiedDBsError); ok {
			return c.
				Status(fiber.StatusNotImplemented).
				SendString(trackErr.Error())
		} else {
			return c.
				Status(fiber.StatusInternalServerError).
				SendString(trackErr.Error())
		}
	}
	c.Location(locationPath(h.backupPath, "/track/restore/", actionTrack.TrackId))
	h.logger.Debug(fmt.Sprintf("Track: %+v", actionTrack))
	return c.Status(fiber.StatusAccepted).JSON(&actionTrack)
}

// Evict backup godoc
// @Tags Backup and Restore
// @Summary Evict backup
// @Description Returns deletion status
// @Accept   json
// @Produce  json
// @Param appName path string true "Application name" Enums(postgresql, arangodb, clickhouse, mongodb, cassandra) default(postgresql)
// @Param apiVersion path string true "API version of dbaas adapter" Enums(v1, v2) default(v2)
// @Param backupId path string true "trackId"
// @Success 200 {string} Token "Succesfull delete"
// @Failure 500 {string} Token "Unknown error"
// @Router /{appName}/backups/{backupId} [delete]
func (h *DbaasAdapterHandler) DeleteBackup(c *fiber.Ctx) error {
	backupId := c.Params("backupId")
	ctx := getRequestContext(c)
	return c.SendString(h.backupService.EvictBackup(ctx, backupId))
}

// Migrate DB password to vault godoc
// @Tags Database administration
// @Summary Migrate DB password to vault
// @Description Run migration of DB password to vault and returns vault secret
// @Accept   json
// @Produce  plain
// @Param appName path string true "Application name" Enums(postgresql, arangodb, clickhouse, mongodb, cassandra) default(postgresql)
// @Param apiVersion path string true "API version of dbaas adapter" Enums(v1, v2) default(v2)
// @Param dbName path string true "Databases to update"
// @Param userName path string true "User whose password should be migrated"
// @Success 200 {string} string "Vault role name"
// @Failure 500 {string} Token "Unknown error"
// @Router /{appName}/databases/{dbName}/migrate-to-vault/{userName} [post]
func (h *DbaasAdapterHandler) MigrateToVault(c *fiber.Ctx) error {
	ctx := getRequestContext(c)
	roleName, err := h.adminService.MigrateToVault(ctx, c.Params("dbName"), c.Params("userName"))
	if err != nil {
		return err
	}
	return c.SendString(roleName)
}

func locationPath(rootPath string, trackPath string, taskId string) string {
	return rootPath + trackPath + taskId
}

func rootPath(version dto.ApiVersion) string {
	return fmt.Sprintf("/api/%s/dbaas/adapter", version)
}

func getRequestContext(c *fiber.Ctx) context.Context {
	requestId := c.Request().Header.Peek("X-Request-ID")
	if len(requestId) == 0 {
		id := uuid.New().String()
		c.Set("X-Request-ID", id)
		requestId = []byte(id)
	}

	bg := context.Background()
	ctx := context.WithValue(bg, "request_id", requestId)
	return ctx
}

var once sync.Once

// @title Dbaas adapter API
// @BasePath /api/{apiVersion}/dbaas/adapter
func BuildFiberDBaaSAdapterHandlers(
	app *fiber.App,
	user string,
	pass string,
	appPath string,
	coreAdminService service.CoreAdministrationServiceIface,
	physicalService *service.PhysicalDatabaseRegistrationService,
	backupService service.BackupAdministrationService,
	supports dto.Supports,
	logger *zap.Logger,
	profiler bool,
	serviceName string) { //TODO changes - added for running multiple tests

	if serviceName == "" {
		serviceName = "dbaas-adapter"
	}
	backupsPath := "/backups"

	supportCopy := make(map[string]bool)
	for key, val := range supports {
		supportCopy[key] = val
	}

	if profiler {
		app.Use(pprof.New())
		logger.Debug("Profiling is activated")
	}

	//this fails fiber restart
	once.Do(
		func() {
			prometheus := fiberprometheus.NewWithRegistry(prometheus.DefaultRegisterer, serviceName, "", "", nil)
			prometheus.RegisterAt(app, "/metrics")
			app.Use(prometheus.Middleware)
		},
	)

	recoverConfig := recover.ConfigDefault
	recoverConfig.EnableStackTrace = true
	recoverConfig.StackTraceHandler = func(c *fiber.Ctx, e interface{}) {
		logger.Error(fmt.Sprintf("Panic: %+v\nStacktrace:\n%s", e, string(debug.Stack())))
	}
	app.Use(recover.New(recoverConfig))
	app.Use(func(c *fiber.Ctx) error {
		// Setting defaults for existed handlers
		c.Request().Header.SetContentType(utils.GetMIME("json"))
		logger.Debug(fmt.Sprintf("%s %s", c.Request().Header.Method(), c.Path()))
		return c.Next()
	})

	general := app.Group(rootPath(coreAdminService.GetVersion()), func(c *fiber.Ctx) error {
		//Common API Handler
		return c.Next()
	})

	app.Get("/swagger/*", swagger.New(swagger.Config{ // custom
		URL:          "/swagger/doc.json",
		DeepLinking:  false,
		ValidatorUrl: "none",
	}))

	// /redis /cassandra etc
	database := general.Group(appPath, func(c *fiber.Ctx) error {
		//DB API Handler
		return c.Next()
	})

	database.Get("/supports", func(c *fiber.Ctx) error {
		return c.JSON(supportCopy)
	})

	database.Use(basicauth.New(basicauth.Config{
		Realm: "This API is for using by dbaas aggregator only",
		Users: map[string]string{
			user: pass,
		},
	}))

	adapterHandler := &DbaasAdapterHandler{
		adminService:    coreAdminService,
		backupService:   backupService,
		physicalService: physicalService,
		logger:          logger,
		backupPath:      rootPath(coreAdminService.GetVersion()) + appPath + backupsPath,
	}

	database.Post("/databases", adapterHandler.CreateDatabase)

	database.Get("/databases", adapterHandler.GetDatabases)

	database.Put("/databases/:dbName/metadata", adapterHandler.UpdateMetadata)

	database.Post("/databases/:dbName/migrate-to-vault/:userName", adapterHandler.MigrateToVault)

	database.Post("/resources/bulk-drop", adapterHandler.BulkDrop)

	database.Post("/describe/databases", adapterHandler.DescribeDatabases)

	database.Get("/physical_database", adapterHandler.PhysicalRegistration) //TODO check in dbaas adapter

	database.Put("/users", adapterHandler.CreateNewUser)

	database.Put("/users/:name", adapterHandler.CreateUser)

	//Backups
	trackBackupPath := "/track/backup/"
	trackRestorePath := "/track/restore/"
	backups := database.Group(backupsPath, func(c *fiber.Ctx) error {
		if backupService == nil {
			panic("Operation not supported")
		}
		return c.Next()
	})

	backups.Post("/collect", adapterHandler.Collect)

	backups.Get(trackBackupPath+":trackId", adapterHandler.TrackBackup)

	backups.Post(":backupId/restore", adapterHandler.Restore)

	backups.Post(":backupId/restoration", adapterHandler.Restoration)

	backups.Get(trackRestorePath+":trackId", adapterHandler.TrackRestore)

	backups.Delete(":backupId", adapterHandler.DeleteBackup)

	general.Get("/physical_database/force_registration", adapterHandler.ForceRegistration)

	health := dto.Health{
		Status: "UP",
		PhysicalDatabaseRegistration: &dto.PhysicalDatabaseRegistrationHealth{
			Status: "UNKNOWN",
		},
	}
	coreAdminService.PreStart()
	physicalService.StartRegister()
	health.PhysicalDatabaseRegistration = &physicalService.Health
	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(&health)
	})
}
