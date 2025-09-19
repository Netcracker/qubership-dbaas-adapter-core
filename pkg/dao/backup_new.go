package dao

// BackupRequestNew represents the request structure for creating backups
type BackupRequestNew struct {
	StorageName string   `json:"storageName"`
	BlobPath    string   `json:"blobPath"`
	Databases   []string `json:"databases"`
}

// CreateBackupRequest represents the request structure for creating backups
type CreateBackupRequest struct {
	StorageName string               `json:"storageName" validate:"required"`
	BlobPath    string               `json:"blobPath" validate:"required"`
	Databases   []BackupDatabaseInfo `json:"databases" validate:"required,dive"`
}

// BackupDatabaseInfo represents a database to be included in the backup
type BackupDatabaseInfo struct {
	DatabaseName string `json:"databaseName" validate:"required"`
}

// BackupRestoreStatus represents the status of backup/restore operations
type BackupRestoreStatus string

const (
	NotStartedStatus = BackupRestoreStatus("notStarted")
	InProgressStatus = BackupRestoreStatus("inProgress")
	CompletedStatus  = BackupRestoreStatus("completed")
	FailedStatus     = BackupRestoreStatus("failed")
)

// LogicalDatabaseBackup represents the status of a backup for a specific database
type LogicalDatabaseBackup struct {
	DatabaseName string              `json:"databaseName" validate:"required"`
	Status       BackupRestoreStatus `json:"status" validate:"required"`
	Size         *int64              `json:"size,omitempty"`
	Duration     *int32              `json:"duration,omitempty"`
	Path         *string             `json:"path,omitempty"`
	ErrorMessage *string             `json:"errorMessage,omitempty"`
	CreationTime *string             `json:"creationTime,omitempty"`
}

// BackupResponse represents the response for a backup operation
type BackupResponse struct {
	Status         BackupRestoreStatus     `json:"status" validate:"required"`
	ErrorMessage   *string                 `json:"errorMessage,omitempty"`
	BackupId       string                  `json:"backupId" validate:"required"`
	CreationTime   string                  `json:"creationTime" validate:"required"`
	CompletionTime *string                 `json:"completionTime,omitempty"`
	StorageName    string                  `json:"storageName" validate:"required"`
	BlobPath       string                  `json:"blobPath" validate:"required"`
	Databases      []LogicalDatabaseBackup `json:"databases" validate:"required"`
}

// RestoreMapping represents the mapping for database restoration
type RestoreMapping struct {
	MicroserviceName string  `json:"microserviceName" validate:"required"`
	DatabaseName     string  `json:"databaseName" validate:"required"`
	Namespace        string  `json:"namespace" validate:"required"`
	Prefix           *string `json:"prefix,omitempty"`
}

// CreateRestoreRequest represents the request structure for creating restores
type CreateRestoreRequest struct {
	StorageName string           `json:"storageName" validate:"required"`
	BlobPath    string           `json:"blobPath" validate:"required"`
	Databases   []RestoreMapping `json:"databases" validate:"required,dive"`
}

type RestoreRequestNew struct {
	StorageName string                 `json:"storageName" validate:"required"`
	BlobPath    string                 `json:"blobPath" validate:"required"`
	Databases   []DaemonRestoreMapping `json:"databases" validate:"required,dive"`
}

type DaemonRestoreMapping struct {
	PreviousDatabaseName string `json:"previousDatabaseName" validate:"required"`
	DatabaseName         string `json:"databaseName" validate:"required"`
}

// LogicalDatabaseRestore represents the status of a restore for a specific database
type LogicalDatabaseRestore struct {
	MicroserviceName     *string             `json:"microserviceName,omitempty"`
	Namespace            *string             `json:"namespace,omitempty"`
	Prefix               *string             `json:"prefix,omitempty"`
	PreviousDatabaseName *string             `json:"previousDatabaseName,omitempty"`
	DatabaseName         string              `json:"databaseName" validate:"required"`
	Status               BackupRestoreStatus `json:"status" validate:"required"`
	Duration             *int32              `json:"duration,omitempty"`
	Path                 *string             `json:"path,omitempty"`
	ErrorMessage         *string             `json:"errorMessage,omitempty"`
	CreationTime         *string             `json:"creationTime,omitempty"`
}

// RestoreResponse represents the response for a restore operation
type RestoreResponse struct {
	Status         BackupRestoreStatus      `json:"status" validate:"required"`
	ErrorMessage   *string                  `json:"errorMessage,omitempty"`
	RestoreId      string                   `json:"restoreId" validate:"required"`
	CreationTime   string                   `json:"creationTime" validate:"required"`
	CompletionTime *string                  `json:"completionTime,omitempty"`
	StorageName    string                   `json:"storageName" validate:"required"`
	BlobPath       string                   `json:"blobPath" validate:"required"`
	Databases      []LogicalDatabaseRestore `json:"databases" validate:"required"`
}

// BadRequestResponse represents a 400 Bad Request error response
type BadRequestResponse struct {
	Error   string   `json:"error"`
	Details []string `json:"details,omitempty"`
}

// ServerErrorResponse represents a 500 Internal Server Error response
type ServerErrorResponse struct {
	Error     string `json:"error"`
	RequestId string `json:"requestId"`
}
