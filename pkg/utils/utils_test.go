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
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPrepareDatabaseName(t *testing.T) {
	t.Run("Simple", func(t *testing.T) {
		d, _ := PrepareDatabaseName("streaming-platform-dto1", "streaming-service", 64)
		assert.Contains(t, d, "streaming-service_streaming-platform-dto1")
		assert.LessOrEqual(t, len(d), 64)
	})
	t.Run("Vowels", func(t *testing.T) {
		d, _ := PrepareDatabaseName("agilenew1-bss-emnetcracker-clonedenv2", "quotation-engine3-service-test", 64)
		assert.Contains(t, d, "qttn-engn3-srvc-tst_aglnw1-bss-emntcrckr-clndnv2")
		assert.LessOrEqual(t, len(d), 64)
	})
	t.Run("Shrinkin only last words only for namespace", func(t *testing.T) {
		d, _ := PrepareDatabaseName("version-num-v3minor3-aragorn-stranger-prod", "possibility-argo-solution-for", 64)
		assert.Contains(t, d, "possibility-argo-solution-for_vn-nm-v3-an-sr-pd")
		assert.LessOrEqual(t, len(d), 64)
	})
	t.Run("Shrinkin only last words for each namespace and microserviceNam", func(t *testing.T) {
		d, _ := PrepareDatabaseName("postgres3-v1-connector-scheduler-test3", "netcracker-argo-major-v3-version-with-prod-environment", 64)
		assert.Contains(t, d, "netcracker-argo-mr-v3-vn-wh-pd-et_p3-v1-cr-sr-t3")
		assert.LessOrEqual(t, len(d), 64)
	})
	t.Run("Shrink works and then cut everything which is higher than the specified limit", func(t *testing.T) {
		d, _ := PrepareDatabaseName("arango-v1-test-clod3-engineenv3-bss-cdc-dpt-mon-tue-sun-ever-du-ha-st", "quotation-engine3-prd-ready-solution-service-test", 64)
		assert.Contains(t, d, "qn-e3-pd-ry-sn-se-tt_ao-v1-tt-c3-e3-bs-cc-dt-mn")
		assert.LessOrEqual(t, len(d), 64)
	})
	t.Run("Error", func(t *testing.T) {
		_, err := PrepareDatabaseName("arango-v1-test-clod3-engineenv3-bss-cdc-dpt-mon-tue-sun-ever-du-ha-st", "quotation-engine3-prd-ready-solution-service-test", 16)
		assert.NotNil(t, err)
	})
}

func TestNsAndMsObtaining(t *testing.T) {
	t.Run("Zero metadata", func(t *testing.T) {
		_, _, err := GetNsAndMsName(nil)
		assert.ErrorContains(t, err, "metadata is not provided")
	})
	t.Run("Classifier is not present", func(t *testing.T) {
		metadata := map[string]any{}
		_, _, err := GetNsAndMsName(metadata)
		assert.ErrorContains(t, err, "classifier is not specified")
	})
	t.Run("Classifier has incorrect type", func(t *testing.T) {
		classifier := map[string]int{}
		metadata := map[string]any{cKey: classifier}
		_, _, err := GetNsAndMsName(metadata)
		assert.ErrorContains(t, err, "classifier type is not correct")
	})
	t.Run("Namespace is not string", func(t *testing.T) {
		classifier := map[string]any{}
		metadata := map[string]any{cKey: classifier}
		_, _, err := GetNsAndMsName(metadata)
		assert.ErrorContains(t, err, "namespace is not string")
	})
	t.Run("MicroserviceName is not string", func(t *testing.T) {
		classifier := map[string]any{nsKey: ""}
		metadata := map[string]any{cKey: classifier}
		_, _, err := GetNsAndMsName(metadata)
		assert.ErrorContains(t, err, "miscroserviceName is not string")
	})
	t.Run("Namespace and microserviceName are empty", func(t *testing.T) {
		classifier := map[string]any{nsKey: "", msKey: ""}
		metadata := map[string]any{cKey: classifier}
		_, _, err := GetNsAndMsName(metadata)
		assert.ErrorContains(t, err, "namespace or microservice name length is 0")
	})
	t.Run("Namespace and Miscroservicename successfully extracted", func(t *testing.T) {
		namespace := "test-namespace"
		microserviceName := "test-micaroservice"

		classifier := map[string]any{nsKey: namespace, msKey: microserviceName}
		metadata := map[string]any{cKey: classifier}

		ns, ms, err := GetNsAndMsName(metadata)
		assert.Nil(t, err)
		assert.Equal(t, ns, namespace)
		assert.Equal(t, ms, microserviceName)
	})
}

func Test_RegenerateDbName(t *testing.T) {
	Limit := 63
	t.Run("Much less 63", func(t *testing.T) {
		newName := RegenerateDbName(randSeq(15), Limit)
		assert.Len(t, newName, 15+12)
	})
	t.Run("Close to 63", func(t *testing.T) {
		newName := RegenerateDbName(randSeq(58), Limit)
		assert.Len(t, newName, 63)
	})
	t.Run("Close to 63 with hypen", func(t *testing.T) {
		newName := RegenerateDbName(randSeq(52), Limit)
		assert.Len(t, newName, 63)
	})
}

var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func randSeq(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}
