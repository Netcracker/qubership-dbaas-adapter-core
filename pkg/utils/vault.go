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

package utils

import (
	"errors"
	"fmt"
	"os"

	"strings"

	vault "github.com/hashicorp/vault/api"
	"go.uber.org/zap"
)

const (
	NCPrefix            = "nc-"
	VaultPasswordPrefix = "vault:"
	RolePrefix          = "nc-dbaas-"
)

var log = GetLogger(GetEnvAsBool("DEBUG_LOG", true))

type VaultClient struct {
	client *vault.Client
	VaultConfig
	k8sJWT            string
	RotationStatement string
}

type DatabaseRoleOptions struct {
	UserName           string   `json:"username"`
	DbName             string   `json:"db_name"`
	RotationPeriod     string   `json:"rotation_period"`
	RotationStatements []string `json:"rotation_statements"`
}

type VaultConfig struct {
	IsVaultEnabled  bool
	Address         string
	VaultRole       string
	VaultRotPeriod  string
	VaultAuthMethod string
	VaultDBName     string
}

func NewVaultClient(vaultConfig VaultConfig) *VaultClient {
	K8SToken, err := GetK8SToken("")
	if err != nil {
		panic(err)
	}

	config := vault.DefaultConfig()

	config.Address = vaultConfig.Address
	vaultClient, err := vault.NewClient(config)
	if err != nil {
		log.Error("Can not create vault client", zap.Error(err))
		panic(err)
	}

	lr, err := loginByK8S(vaultClient, vaultConfig, K8SToken)
	if err != nil {
		panic(err)
	}

	vaultClient.SetToken(lr.Auth.ClientToken)

	adapterVaultClient := VaultClient{
		client:      vaultClient,
		k8sJWT:      K8SToken,
		VaultConfig: vaultConfig,
	}

	err = adapterVaultClient.RefreshSelfToken()
	if err != nil {
		panic(err)
	}

	return &adapterVaultClient
}

func (vc *VaultClient) CreateVaultRole(cloudPublicHost, namespace, microserviceName, dbRole string) (string, error) {
	err := vc.RefreshSelfToken()
	if err != nil {
		return "", err
	}

	roleName := vc.GetVaultRoleName(cloudPublicHost, namespace, microserviceName, dbRole)
	req := vc.client.NewRequest("POST", "/v1/database/static-roles/"+roleName)

	roleOptions := DatabaseRoleOptions{
		UserName:       dbRole,
		DbName:         vc.VaultDBName,
		RotationPeriod: getRotationPeriod(vc.VaultRotPeriod),
	}
	if vc.RotationStatement != "" {
		roleOptions.RotationStatements = []string{vc.RotationStatement}
	}
	err = req.SetJSONBody(roleOptions)

	if err != nil {
		log.Error("can not create Vault role request body")
		return "", err
	}

	resp, err := vc.client.RawRequest(req)
	if err != nil {
		log.Error("can not create Vault role", zap.Error(err))
		return "", err
	}
	if resp.Error() != nil {
		return "", resp.Error()
	}
	log.Info("Vault role has been created")

	return roleName, nil
}

func (vc *VaultClient) DeleteVaultRole(roleName string) error {
	if err := vc.RefreshSelfToken(); err != nil {
		return err
	}

	req := vc.client.NewRequest("DELETE", "/v1/database/static-roles/"+roleName)
	resp, err := vc.client.RawRequest(req)
	if err != nil {
		log.Error(fmt.Sprintf("can not delete Vault role %s", roleName), zap.Error(err))
		return err
	}
	if err = resp.Error(); err != nil {
		log.Error(fmt.Sprintf("can not delete Vault role %s", roleName), zap.Error(err))
		return err
	}

	log.Info(fmt.Sprintf("Vault role %s has been deleted", roleName))
	return nil
}

func (vc *VaultClient) ForceRefreshCredsFor(vaultRole string) error {
	err := vc.RefreshSelfToken()
	if err != nil {
		return err
	}

	err = vc.ForceRefreshDBCredsFor(vaultRole)
	if err != nil {
		log.Error("Can not create Vault role")
		return err
	}

	return nil
}

func (vc *VaultClient) RefreshSelfToken() error {
	lr, err := loginByK8S(vc.client, vc.VaultConfig, vc.k8sJWT)
	if err != nil {
		return err
	}
	vc.client.SetToken(lr.Auth.ClientToken)
	return nil
}

func (vc *VaultClient) ForceRefreshDBCredsFor(vaultRole string) error {
	req := vc.client.NewRequest("POST", "/v1/database/rotate-role/"+vaultRole)
	resp, err := vc.client.RawRequest(req)
	if err != nil {
		log.Error("Can not update Vault DB creds", zap.Error(err))
		return err
	}
	if resp.Error() != nil {
		return resp.Error()
	}
	log.Info("Vault password has been rotated")
	return nil
}

func (vc *VaultClient) GetVaultRoleName(cloudPublicHost string, namespace string, microserviceName string, dbRole string) string {
	return RolePrefix + cloudPublicHost + "_" + namespace + "_" + microserviceName + "_" + dbRole
}

func (vc *VaultClient) ReadPasswordFromKv(secretPath string) (string, error) {
	secret, err := vc.client.Logical().Read(secretPath)
	if err != nil {
		return "", err
	}
	if password, ok := secret.Data["password"]; ok {
		return password.(string), nil
	} else {
		return "", errors.New("secret is empty")
	}
}

func IsVaultPassword(password string) bool {
	return strings.HasPrefix(password, VaultPasswordPrefix)
}

func GetSecretPath(password string) string {
	return password[len(VaultPasswordPrefix):]
}

func GetK8SToken(fileName string) (string, error) {
	if fileName == "" {
		fileName = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	}

	rawContent, err := os.ReadFile(fileName)
	if err != nil {
		log.Error(fmt.Sprintf("Error during read file %s", fileName), zap.Error(err))
		return "", err
	}
	content := string(rawContent)

	return content, nil
}

type loginResult struct {
	Auth struct {
		ClientToken string `json:"client_token"`
	} `json:"auth"`
}

func loginByK8S(vaultClient *vault.Client, vaultConfig VaultConfig, k8sJWT string) (*loginResult, error) {
	req := vaultClient.NewRequest("POST", "/v1/auth/"+vaultConfig.VaultAuthMethod+"/login")
	err := req.SetJSONBody(map[string]string{"role": vaultConfig.VaultRole, "jwt": k8sJWT})

	if err != nil {
		log.Error("Can not set Json body in vault login request")
		return nil, err
	}

	resp, err := vaultClient.RawRequest(req)
	if err != nil {
		log.Error("Can not login via k8s token", zap.Error(err))
		return nil, err
	}

	var result loginResult
	err = resp.DecodeJSON(&result)
	if err != nil {
		log.Error("Can not decode login result Json")
		return nil, err
	}

	return &result, nil
}

func getRotationPeriod(rotationPeriod string) string {
	if rotationPeriod == "" {
		rotationPeriod = "24h"
	}
	return rotationPeriod
}
