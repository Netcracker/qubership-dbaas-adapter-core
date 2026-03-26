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

package dao

type BackupRestoresOnlySpecifiedDBsError struct{}

func (e *BackupRestoresOnlySpecifiedDBsError) Error() string {
	return "Cannot restore backup without explicitly specified list of databases in it"
}

type BackupStatus string

const (
	QueuedBackupStatus     = BackupStatus("Queued")
	SuccessfulBackupStatus = BackupStatus("Successful")
	ProcessingBackupStatus = BackupStatus("Processing")
	FailedBackupStatus     = BackupStatus("Failed")
)

type BackupTask struct {
	Vault  string       `json:"vault"`
	Status BackupStatus `json:"status"`
	TaskId string       `json:"task_id"`
}

type DatabasesBackupAdapt struct {
	LocalId string `json:"localId,omitempty"`
}

type DatabaseAdapterBackupAdapterTrackStatus string

const (
	FailTrackStatus       = DatabaseAdapterBackupAdapterTrackStatus("FAIL")
	SuccessTrackStatus    = DatabaseAdapterBackupAdapterTrackStatus("SUCCESS")
	ProceedingTrackStatus = DatabaseAdapterBackupAdapterTrackStatus("PROCEEDING")
)

type DatabaseAdapterAction string

const (
	BackupAction  = DatabaseAdapterAction("BACKUP")
	RestoreAction = DatabaseAdapterAction("RESTORE")
)

type DatabaseAdapterBaseTrack struct {
	Action  DatabaseAdapterAction                   `json:"action,omitempty"`
	Details *DatabasesBackupAdapt                   `json:"details,omitempty"`
	Status  DatabaseAdapterBackupAdapterTrackStatus `json:"status,omitempty"`
	TrackId string                                  `json:"trackId,omitempty"`
}

func GetDatabaseAdapterBaseTrackByTask(task BackupTask) DatabaseAdapterBaseTrack {
	daemonStatus := task.Status
	if daemonStatus == "" {
		daemonStatus = FailedBackupStatus
	}
	trackStatus := FailTrackStatus
	var details *DatabasesBackupAdapt = nil
	switch daemonStatus {
	case ProcessingBackupStatus, QueuedBackupStatus:
		trackStatus = ProceedingTrackStatus
		break
	case SuccessfulBackupStatus:
		trackStatus = SuccessTrackStatus
		if task.Vault != "" && task.Vault != "None" {
			details = &DatabasesBackupAdapt{
				LocalId: task.Vault,
			}
		}
		break
	case FailedBackupStatus:
		trackStatus = FailTrackStatus
		break
	}
	return DatabaseAdapterBaseTrack{
		Details: details,
		Status:  trackStatus,
		TrackId: task.TaskId,
	}
}

func GetDatabaseAdapterBackupActionTrack(status DatabaseAdapterBackupAdapterTrackStatus, trackId string) DatabaseAdapterBaseTrack {
	track := DatabaseAdapterBaseTrack{
		Status:  status,
		TrackId: trackId,
	}
	track.Action = BackupAction
	return track
}

func GetDatabaseAdapterBackupActionTrackByTask(task BackupTask) DatabaseAdapterBaseTrack {
	track := GetDatabaseAdapterBaseTrackByTask(task)
	track.Action = BackupAction
	return track
}

type DatabaseAdapterRestoreTrack struct {
	DatabaseAdapterBaseTrack
	ChangedNameDb map[string]string `json:"changedNameDb,omitempty"`
}

func GetDatabaseAdapterRestoreActionTrack(status DatabaseAdapterBackupAdapterTrackStatus, trackId string, changedNameDb map[string]string) DatabaseAdapterRestoreTrack {
	track := DatabaseAdapterRestoreTrack{}
	track.Status = status
	track.TrackId = trackId
	track.Action = RestoreAction
	track.ChangedNameDb = changedNameDb
	return track
}

func GetDatabaseAdapterRestoreActionTrackByTask(task BackupTask) DatabaseAdapterRestoreTrack {
	track := DatabaseAdapterRestoreTrack{}
	track.DatabaseAdapterBaseTrack = GetDatabaseAdapterBaseTrackByTask(task)
	track.Action = RestoreAction
	return track
}

type BackupRequest struct {
	Dbs           []string `json:"dbs"`
	AllowEviction string   `json:"allow_eviction"`
	Keep          string   `json:"keep,omitempty"`
}

type RestoreRequest struct {
	Vault         string            `json:"vault"`
	Dbs           []string          `json:"dbs"`
	ChangeDbNames map[string]string `json:"changeDbNames,omitempty"`
}

type RestorationRequest struct {
	Databases       []DbInfo `json:"databases"`
	RegenerateNames bool     `json:"regenerateNames"`
}

type DbInfo struct {
	Name         string  `json:"name"`
	Microservice string  `json:"microservice"`
	Namespace    string  `json:"namespace"`
	Prefix       *string `json:"prefix,omitempty"`
}
