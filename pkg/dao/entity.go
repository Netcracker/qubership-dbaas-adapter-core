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

import "maps"

const (
	RootUrl         = "/api"
	MajorAPIVersion = 2
	MinorAPIVersion = 1
)

const DefaultRouteFormat = "%s/%s/dbaas/adapter"

const StatusRunning Status = "running"
const StatusRun Status = "run"

var SupportedMajorsVersions = []int{2}

type ApiVersion string

type Status string

type Metadata struct {
	ApiVersion     ApiVersion      `json:"apiVersion,omitempty"`
	ApiVersions    ApiVersions     `json:"apiVersions"`
	SupportedRoles []string        `json:"supportedRoles,omitempty"`
	Features       map[string]bool `json:"features,omitempty"`
	ROHost         string          `json:"roHost,omitempty"`
}

type ApiVersions struct {
	Specs []ApiVersionsSpec `json:"specs,omitempty"`
}

type ApiVersionsSpec struct {
	SpecRootUrl     string `json:"specRootUrl,omitempty"`
	Major           int    `json:"major,omitempty"`
	Minor           int    `json:"minor,omitempty"`
	SupportedMajors []int  `json:"supportedMajors,omitempty"`
}

type PhysicalDatabaseRegistrationResponse struct {
	Instruction Instruction `json:"instruction"`
}

type Instruction struct {
	Id              string           `json:"id,omitempty"`
	AdditionalRoles []AdditionalRole `json:"additionalRoles,omitempty"`
}

type AdditionalRole struct {
	Id                   string                 `json:"id,omitempty"`
	DbName               string                 `json:"dbName,omitempty"`
	ConnectionProperties []ConnectionProperties `json:"connectionProperties,omitempty"`
	Resources            []DbResource           `json:"resources,omitempty"`
}

type DbCreateRequest struct {
	Metadata   map[string]any `json:"metadata,omitempty"`
	NamePrefix *string        `json:"namePrefix,omitempty"`
	Password   string         `json:"password,omitempty"`
	DbName     string         `json:"dbName,omitempty"`
	Settings   map[string]any `json:"settings,omitempty"`
	Username   string         `json:"username,omitempty"`
	Role       string         `json:"role,omitempty"`
}

type ConnectionProperties map[string]any

type DbCreateResponse struct {
	Name                 string               `json:"name,omitempty"`
	ConnectionProperties ConnectionProperties `json:"connectionProperties,omitempty"`
	Resources            []DbResource         `json:"resources,omitempty"`
}

type DbCreateResponseMultiUser struct {
	Name                  string                 `json:"name,omitempty"`
	ConnectionProperties  []ConnectionProperties `json:"connectionProperties,omitempty"`
	Resources             []DbResource           `json:"resources,omitempty"`
	ConnectionDescription any                    `json:"connectionDescription,omitempty"`
}

type DbResource struct {
	Kind         string             `json:"kind,omitempty"`
	Name         string             `json:"name,omitempty"`
	ErrorMessage string             `json:"errorMessage,omitempty"`
	Status       DropResourceStatus `json:"status,omitempty"`
}

type DropResourceStatus string

const (
	DELETED       = DropResourceStatus("DELETED")
	DELETE_FAILED = DropResourceStatus("DELETE_FAILED")
)

type LogicalDatabaseDescribed struct {
	ConnectionProperties []ConnectionProperties `json:"connectionProperties,omitempty"`
	Resources            []DbResource           `json:"resources,omitempty"`
}

type LogicalDatabaseDescribedSingle struct {
	ConnectionProperties ConnectionProperties `json:"connectionProperties,omitempty"`
	Resources            []DbResource         `json:"resources,omitempty"`
}

type Supports map[string]bool

type SupportsBase struct {
	Users             bool
	Settings          bool
	DescribeDatabases bool
	AdditionalKeys    Supports
}

func (r *SupportsBase) ToMap() Supports {
	result := make(Supports)
	result["users"] = r.Users
	result["settings"] = r.Settings
	result["describeDatabases"] = r.DescribeDatabases
	if r.AdditionalKeys != nil {
		maps.Copy(result, r.AdditionalKeys)
	}
	return result
}

type PhysicalDatabaseRegistrationHealth struct {
	Status string `json:"status"`
}

type Health struct {
	Status                       string                              `json:"status"`
	PhysicalDatabaseRegistration *PhysicalDatabaseRegistrationHealth `json:"physicalDatabaseRegistration"`
}

type BasicAuth struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type DbaasAggregatorVersion struct {
	Major           int   `json:"major"`
	Minor           int   `json:"minor"`
	SupportedMajors []int `json:"supportedMajors"`
}

type PhysicalDatabaseRegistrationRequest struct {
	AdapterAddress       string            `json:"adapterAddress"`
	HttpBasicCredentials BasicAuth         `json:"httpBasicCredentials"`
	Labels               map[string]string `json:"labels,omitempty"`
	Metadata             Metadata          `json:"metadata"`
	Status               Status            `json:"status,omitempty"`
}

type PhysicalDatabaseRoleRequest struct {
	Success []Success `json:"success,omitempty"`
	Failure *Failure  `json:"failure,omitempty"`
}

type Success struct {
	Id                   string                 `json:"id,omitempty"`
	ConnectionProperties []ConnectionProperties `json:"connectionProperties,omitempty"`
	Resources            []DbResource           `json:"resources,omitempty"`
	DbName               string                 `json:"-"`
}

type Failure struct {
	Id      string `json:"id,omitempty"`
	Message string `json:"message,omitempty"`
}

type UserCreateRequest struct {
	DbName         string `json:"dbName,omitempty"`
	Password       string `json:"password,omitempty"`
	Role           string `json:"role,omitempty"`
	UsernamePrefix string `json:"usernamePrefix,omitempty"`
}

type CreatedUser struct {
	ConnectionProperties ConnectionProperties `json:"connectionProperties,omitempty"`
	Resources            []DbResource         `json:"resources,omitempty"`
	Name                 string               `json:"name,omitempty"`
	Role                 string               `json:"role,omitempty"`
}
