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

package dbaas

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"

	"net/http"
	"strings"

	"github.com/Netcracker/qubership-dbaas-adapter-core/pkg/dao"
	"github.com/Netcracker/qubership-dbaas-adapter-core/pkg/utils"
)

type Client struct {
	URL         string
	Credentials *dao.BasicAuth
	Client      *http.Client
	version     string
}

// creates new dbaas.Client. (client *http.Client) parameter can be nil.
// err might not be nil if dbaas in unavailable
func NewDbaasClient(url string, credentials *dao.BasicAuth, client *http.Client) (*Client, error) {
	if client == nil {
		client = &http.Client{}
		if strings.Contains(url, "https") {
			if err := utils.ConfigureHttpsForClient(client); err != nil {
				return nil, fmt.Errorf("failed to set up https client, err: %v", err)
			}
		}
	}
	dbaasClient := &Client{
		URL:         url,
		Credentials: credentials,
		Client:      client,
	}
	version, err := dbaasClient.requestAggregatorVersion()
	if version != "" {
		dbaasClient.version = version
		return dbaasClient, err
	}
	return nil, err
}

func (d *Client) GetVersion() (string, error) {
	if d.version == "" {
		return d.requestAggregatorVersion()
	}

	return d.version, nil
}

func (d *Client) requestAggregatorVersion() (string, error) {
	apiVersionUrl := fmt.Sprintf("%s/api-version", d.URL)
	code, body, err := d.sendRequest(http.MethodGet, apiVersionUrl, nil)

	aggrVersion := dao.DbaasAggregatorVersion{}
	if code == http.StatusOK {
		err := json.Unmarshal(body, &aggrVersion)
		if err != nil {
			return "", err
		}
		if slices.Contains(aggrVersion.SupportedMajors, 3) {
			return "v3", nil
		}
		return "v2", nil
	} else if code == http.StatusNotFound {
		return "v2", nil
	} else {
		//we assume that dbaas is unavailable or not installed - so start with the highest version we have.
		//If registration fails - adapter will restart and pick correct version
		return "v3", err
	}

}

func (d *Client) sendRequest(method, url string, payload io.Reader) (int, []byte, error) {
	req, err := http.NewRequest(method, url, payload)
	if err != nil {
		return http.StatusInternalServerError, nil, err
	}

	if d.Credentials != nil {
		req.SetBasicAuth(d.Credentials.Username, d.Credentials.Password)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.Client.Do(req)
	if err != nil {
		if resp != nil {
			return resp.StatusCode, nil, err
		}
		return http.StatusInternalServerError, nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		if resp != nil {
			return resp.StatusCode, nil, err
		}
		return http.StatusInternalServerError, nil, err
	}

	return resp.StatusCode, body, nil
}

func (d *Client) PhysicalDatabaseRegistration(dbName, physicalDatabaseId string, data dao.PhysicalDatabaseRegistrationRequest) (*dao.PhysicalDatabaseRegistrationResponse, error) {
	method := http.MethodPut
	url := fmt.Sprintf("%s/api/%s/dbaas/%s/physical_databases/%s", d.URL, d.version, dbName, physicalDatabaseId)
	codedBody, errm := json.Marshal(data)
	if errm != nil {
		return nil, fmt.Errorf("failed to marshal phydb registration body %v", codedBody)
	}
	payload := strings.NewReader(string(codedBody))

	statusCode, body, err := d.sendRequest(method, url, payload)

	if statusCode < 200 || statusCode > 299 || err != nil {
		return nil, fmt.Errorf(`failed to register physical database:
		uri: %s
		code: %d
		method: %s
		response: %s
		error: %v`, url, statusCode, method, body, err)

	}

	response := dao.PhysicalDatabaseRegistrationResponse{}

	if len(body) > 0 {
		if err = json.Unmarshal(body, &response); err != nil {
			return nil, fmt.Errorf(`failed to Unmarshal PhysicalDatabaseRegistrationResponse,
				statusCode: %d,
				raw response: %s,
				err: %v`, statusCode, body, err)
		}
	}

	return &response, nil
}

func (d *Client) AdditionalRoles(physicalDatabaseId, dbName string, data dao.PhysicalDatabaseRoleRequest, instructionId dao.Instruction) ([]dao.AdditionalRole, error) {
	errMsgPattern := `%v,
		url: %s,
		status code: %d,
		raw response: %s,
		err: %v`
	var response []dao.AdditionalRole
	url := fmt.Sprintf("%s/api/%s/dbaas/%s/physical_databases/%s/instruction/%s/additional-roles", d.URL, d.version, dbName, physicalDatabaseId, instructionId.Id)

	codedBody, errm := json.Marshal(data)
	if errm != nil {
		return nil, fmt.Errorf("failed to marshal PhysicalDatabaseRoleRequest body %v", codedBody)
	}

	payload := strings.NewReader(string(codedBody))

	statusCode, body, err := d.sendRequest(http.MethodPost, url, payload)
	if err != nil || (statusCode != 200 && statusCode != 202) {
		return nil, fmt.Errorf(errMsgPattern, "Failed to request additional roles", url, statusCode, body, err)
	} else if statusCode == 202 {
		if err = json.Unmarshal(body, &response); err != nil {
			return nil, fmt.Errorf(errMsgPattern, "Failed to Unmarshal []entity.AdditionalRole", url, statusCode, body, err)
		}
		return response, nil
	}

	return []dao.AdditionalRole{}, nil

}

func (d *Client) Health() bool {
	url := fmt.Sprintf("%s/health", d.URL)
	statusCode, _, err := d.sendRequest(http.MethodGet, url, nil)

	return err == nil && statusCode == 200
}
