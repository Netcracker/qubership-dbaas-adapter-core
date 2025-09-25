package fiber

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	dto "github.com/Netcracker/qubership-dbaas-adapter-core/pkg/dao"
	utilsCore "github.com/Netcracker/qubership-dbaas-adapter-core/pkg/utils"
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
)

var validate = validator.New()

type blobPathQuery struct {
	BlobPath string `query:"blobPath" validate:"required"`
}

func requireBlobPath(c *fiber.Ctx) (string, error) {
	var q blobPathQuery
	if err := c.QueryParser(&q); err != nil {
		return "", fiber.NewError(fiber.StatusBadRequest, "invalid query")
	}
	if err := validate.Struct(q); err != nil {
		return "", fiber.NewError(fiber.StatusBadRequest, "query parameter 'blobPath' is required")
	}
	return strings.TrimSpace(q.BlobPath), nil
}

// CollectNew initiates a new backup operation
// @Tags Backup and Restore
// @Summary Initiate database backup
// @Description Initiates a backup of specified databases and stores it in the configured storage. The operation is asynchronous and returns immediately with current status of backup.
// @Accept json
// @Produce json
// @Param appName path string true "Application name" Enums(postgresql, arangodb, clickhouse, mongodb, cassandra) default(postgresql)
// @Param apiVersion path string true "API version of dbaas adapter" Enums(v1, v2) default(v2)
// @Param body body dto.CreateBackupRequest true "Backup configuration details"
// @Success 202 {object} dto.BackupResponse "Backup operation accepted and is being processed"
// @Failure 400 {object} dto.BadRequestResponse "The request was invalid or cannot be served"
// @Failure 401 {string} string "Authentication is required and has failed or has not been provided"
// @Failure 403 {string} string "The request was valid, but the server is refusing action"
// @Failure 404 {string} string "The requested resource could not be found"
// @Failure 500 {object} dto.ServerErrorResponse "An unexpected error occurred on the server"
// @Router /{appName}/backups [post]
func (h *DbaasAdapterHandler) CollectNew(c *fiber.Ctx) error {
	ctx := getRequestContext(c)

	defer h.handlePanicRecovery(c, ctx, "CreateBackup")

	var backupRequest dto.CreateBackupRequest
	if err := c.BodyParser(&backupRequest); err != nil {
		h.logger.Error("Failed to parse backup request", zap.Error(err))
		return c.Status(fiber.StatusBadRequest).JSON(dto.BadRequestResponse{
			Error:   "Invalid request parameters",
			Details: []string{err.Error()},
		})
	}

	err := validate.Struct(backupRequest)
	if err != nil {
		h.logger.Error("Failed to validate backup request", zap.Error(err))
		return c.Status(fiber.StatusBadRequest).JSON(h.handleValidationErrors(err))
	}

	h.logger.Debug(fmt.Sprintf("Backup request: %+v", backupRequest))

	// Convert database names to string slice for the service
	var databaseNames []string
	for _, db := range backupRequest.Databases {
		databaseNames = append(databaseNames, db.DatabaseName)
	}

	// Call the service to create backup
	backupResponse, found := h.backupService.CollectBackupNew(ctx, backupRequest.StorageName, backupRequest.BlobPath, databaseNames)
	if !found {
		h.logger.Info("Database not found")
		return c.Status(fiber.StatusNotFound).SendString("Database not found")
	}

	h.logger.Debug(fmt.Sprintf("Backup response: %+v", backupResponse))
	return c.Status(fiber.StatusAccepted).JSON(backupResponse)
}

// TrackBackupNew retrieves details about a specific backup operation
// @Tags Backup and Restore
// @Summary Get backup details
// @Description Retrieve details about a specific backup operation
// @Produce json
// @Param appName path string true "Application name" Enums(postgresql, arangodb, clickhouse, mongodb, cassandra) default(postgresql)
// @Param apiVersion path string true "API version of dbaas adapter" Enums(v1, v2) default(v2)
// @Param backupId path string true "Unique identifier of the backup operation" Format(uuid)
// @Success 200 {object} dto.BackupResponse "Backup details retrieved successfully"
// @Failure 404 {string} string "The requested resource could not be found"
// @Failure 500 {object} dto.ServerErrorResponse "An unexpected error occurred on the server"
// @Router /{appName}/backups/backup/{backupId} [get]
func (h *DbaasAdapterHandler) TrackBackupNew(c *fiber.Ctx) error {
	backupId := c.Params("backupId")
	blobPath, err := requireBlobPath(c)
	if err != nil {
		return err
	}

	ctx := getRequestContext(c)

	defer h.handlePanicRecovery(c, ctx, "GetBackup")

	h.logger.Debug(fmt.Sprintf("Get backup request for ID: %s", backupId))

	backupResponse, found := h.backupService.TrackBackupNew(ctx, backupId, blobPath)
	if !found {
		h.logger.Info("Backup not found", zap.String("backupId", backupId))
		return c.Status(fiber.StatusNotFound).SendString("Backup not found")
	}

	h.logger.Debug(fmt.Sprintf("Backup response: %+v", backupResponse))
	return c.JSON(backupResponse)
}

// DeleteBackupNew deletes a backup operation
// @Tags Backup and Restore
// @Summary Delete backup
// @Description Delete a backup operation
// @Param appName path string true "Application name" Enums(postgresql, arangodb, clickhouse, mongodb, cassandra) default(postgresql)
// @Param apiVersion path string true "API version of dbaas adapter" Enums(v1, v2) default(v2)
// @Param backupId path string true "Unique identifier of the backup operation" Format(uuid)
// @Success 204 "Backup deleted successfully"
// @Failure 404 {string} string "The requested resource could not be found"
// @Failure 500 {object} dto.ServerErrorResponse "An unexpected error occurred on the server"
// @Router /{appName}/backups/backup/{backupId} [delete]
func (h *DbaasAdapterHandler) DeleteBackupNew(c *fiber.Ctx) error {
	backupId := c.Params("backupId")
	blobPath, err := requireBlobPath(c)
	if err != nil {
		return err
	}
	ctx := getRequestContext(c)

	defer h.handlePanicRecovery(c, ctx, "DeleteBackupNew")

	h.logger.Debug(fmt.Sprintf("Delete backup request for ID: %s", backupId))

	found := h.backupService.EvictBackupNew(ctx, backupId, blobPath)
	if !found {
		h.logger.Info("Backup not found", zap.String("backupId", backupId))
		return c.Status(fiber.StatusNotFound).SendString("Backup not found")
	}

	h.logger.Debug(fmt.Sprintf("Backup deleted successfully: %s", backupId))
	return c.SendStatus(fiber.StatusNoContent)
}

// RestoreBackupNew restores databases from a backup
// @Tags Backup and Restore
// @Summary Restore databases from backup
// @Description Restores databases from a previously created backup. Supports dry-run mode to validate the restore operation without making changes. The operation is asynchronous and returns immediately with current status of restore.
// @Accept json
// @Produce json
// @Param appName path string true "Application name" Enums(postgresql, arangodb, clickhouse, mongodb, cassandra) default(postgresql)
// @Param apiVersion path string true "API version of dbaas adapter" Enums(v1, v2) default(v2)
// @Param backupId path string true "Unique identifier of the backup to restore from" Format(uuid)
// @Param dryRun query bool false "If true, validates the restore operation without making any changes" default(false)
// @Param body body dto.CreateRestoreRequest true "Restore configuration details"
// @Success 202 {object} dto.RestoreResponse "Restore operation accepted and is being processed"
// @Failure 400 {object} dto.BadRequestResponse "The request was invalid or cannot be served"
// @Failure 404 {string} string "The requested resource could not be found"
// @Failure 500 {object} dto.ServerErrorResponse "An unexpected error occurred on the server"
// @Router /{appName}/backups/backup/{backupId}/restore [post]
func (h *DbaasAdapterHandler) RestoreBackupNew(c *fiber.Ctx) error {
	backupId := c.Params("backupId")
	dryRun, _ := strconv.ParseBool(c.Query("dryRun", "false"))
	ctx := getRequestContext(c)

	defer h.handlePanicRecovery(c, ctx, "RestoreBackupNew")

	var restoreRequest dto.CreateRestoreRequest
	if err := c.BodyParser(&restoreRequest); err != nil {
		h.logger.Error("Failed to parse restore request", zap.Error(err))
		return c.Status(fiber.StatusBadRequest).JSON(dto.BadRequestResponse{
			Error:   "Invalid request parameters",
			Details: []string{err.Error()},
		})
	}

	// Validate the restore request
	err := validate.Struct(restoreRequest)
	if err != nil {
		h.logger.Error("Failed to validate restore request", zap.Error(err))
		return c.Status(fiber.StatusBadRequest).JSON(h.handleValidationErrors(err))
	}

	h.logger.Debug(fmt.Sprintf("Restore request: %+v, dryRun: %v", restoreRequest, dryRun))

	restoreResponse, found := h.backupService.RestoreBackupNew(ctx, backupId, restoreRequest, dryRun)
	if !found {
		h.logger.Info("Backup not found", zap.String("backupId", backupId))
		return c.Status(fiber.StatusNotFound).SendString("Backup not found")
	}

	h.logger.Debug(fmt.Sprintf("Restore response: %+v", restoreResponse))
	return c.Status(fiber.StatusAccepted).JSON(restoreResponse)
}

// TrackRestoreNew retrieves details of a restore operation
// @Tags Backup and Restore
// @Summary Get restore details
// @Description Retrieves details of a restore operation
// @Produce json
// @Param appName path string true "Application name" Enums(postgresql, arangodb, clickhouse, mongodb, cassandra) default(postgresql)
// @Param apiVersion path string true "API version of dbaas adapter" Enums(v1, v2) default(v2)
// @Param restoreId path string true "Unique identifier of the restore operation" Format(uuid)
// @Success 200 {object} dto.RestoreResponse "Restore details retrieved successfully"
// @Failure 404 {string} string "The requested resource could not be found"
// @Failure 500 {object} dto.ServerErrorResponse "An unexpected error occurred on the server"
// @Router /{appName}/backups/restore/{restoreId} [get]
func (h *DbaasAdapterHandler) TrackRestoreNew(c *fiber.Ctx) error {
	restoreId := c.Params("restoreId")
	blobPath, err := requireBlobPath(c)
	if err != nil {
		return err
	}
	ctx := getRequestContext(c)

	defer h.handlePanicRecovery(c, ctx, "GetRestore")

	h.logger.Debug(fmt.Sprintf("Get restore request for ID: %s", restoreId))

	restoreResponse, found := h.backupService.TrackRestoreNew(ctx, restoreId, blobPath)
	if !found {
		h.logger.Info("Restore not found", zap.String("restoreId", restoreId))
		return c.Status(fiber.StatusNotFound).SendString("Restore not found")
	}

	h.logger.Debug(fmt.Sprintf("Restore response: %+v", restoreResponse))
	return c.JSON(restoreResponse)
}

// Delete restore godoc
// @Tags Backup and Restore
// @Summary Delete restore
// @Description Delete a restore operation
// @Param appName path string true "Application name" Enums(postgresql, arangodb, clickhouse, mongodb, cassandra) default(postgresql)
// @Param apiVersion path string true "API version of dbaas adapter" Enums(v1, v2) default(v2)
// @Param restoreId path string true "Unique identifier of the restore operation" Format(uuid)
// @Success 204 "Restore deleted successfully"
// @Failure 404 {string} string "The requested resource could not be found"
// @Failure 500 {object} dto.ServerErrorResponse "An unexpected error occurred on the server"
// @Router /{appName}/backups/restore/{restoreId} [delete]
func (h *DbaasAdapterHandler) DeleteRestoreNew(c *fiber.Ctx) error {
	restoreId := c.Params("restoreId")
	blobPath, err := requireBlobPath(c)
	if err != nil {
		return err
	}
	ctx := getRequestContext(c)

	defer h.handlePanicRecovery(c, ctx, "DeleteRestore")

	h.logger.Debug(fmt.Sprintf("Delete restore request for ID: %s", restoreId))

	found := h.backupService.EvictRestoreNew(ctx, restoreId, blobPath)
	if !found {
		h.logger.Info("Restore not found", zap.String("restoreId", restoreId))
		return c.Status(fiber.StatusNotFound).SendString("Restore not found")
	}

	h.logger.Debug(fmt.Sprintf("Restore deleted successfully: %s", restoreId))
	return c.SendStatus(fiber.StatusNoContent)
}

// handlePanicRecovery handles panic recovery for handlers
func (h *DbaasAdapterHandler) handlePanicRecovery(c *fiber.Ctx, ctx context.Context, operation string) {
	if r := recover(); r != nil {
		logger := utilsCore.AddLoggerContext(h.logger, ctx)
		logger.Error("Panic recovered in handler",
			zap.String("operation", operation),
			zap.Any("panic", r),
			zap.Stack("stack"))

		requestId := string(ctx.Value("request_id").([]byte))
		c.Status(fiber.StatusInternalServerError).JSON(dto.ServerErrorResponse{
			Error:     "Internal server error",
			RequestId: requestId,
		})
	}
}

// handleValidationErrors extracts and formats validation errors
func (h *DbaasAdapterHandler) handleValidationErrors(err error) dto.BadRequestResponse {
	var validationErrors []string
	if validationErrs, ok := err.(validator.ValidationErrors); ok {
		for _, validationErr := range validationErrs {
			// Get the field name with namespace for nested structs
			fieldName := validationErr.Namespace()
			if fieldName == "" {
				fieldName = validationErr.Field()
			}

			// Create a more descriptive error message
			errorMsg := fmt.Sprintf("Field '%s' failed validation: %s", fieldName, validationErr.Tag())

			validationErrors = append(validationErrors, errorMsg)
		}
	} else {
		validationErrors = []string{err.Error()}
	}

	return dto.BadRequestResponse{
		Error:   "Invalid request parameters",
		Details: validationErrors,
	}
}
