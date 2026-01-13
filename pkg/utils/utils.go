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
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/docker/distribution/uuid"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	certificateFilePath         = "/certs/"
	vowels                      = "aeiouyAEIOUY"
	lowestDbNameLengthByPattern = 21
	regexNamePattern            = "[\\-\\_]+"
	regexSeparatorPattern       = "[^\\-\\_]+"

	DBMaxSuffixLength = 12

	cKey  = "classifier"
	nsKey = "namespace"
	msKey = "microserviceName"
)

type HttpClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type ExecutionError struct {
	error
	Msg string
}

func (r *ExecutionError) Error() string {
	return r.Msg
}

func HandleError(err error, log func(msg string, fields ...zap.Field), message string) {
	if err != nil {
		log(message)
	}
}

func PanicError(err error, log func(msg string, fields ...zap.Field), message string) {
	HandleError(err, log, message)
	if err != nil {
		panic(&ExecutionError{Msg: fmt.Sprintf("%s\n%s", message, err.Error())})
	}
}

func Substr(s string, start, end int) string {
	counter, startIdx := 0, 0
	for i := range s {
		if counter == start {
			startIdx = i
		}
		if counter == end {
			return s[startIdx:i]
		}
		counter++
	}
	return s[startIdx:]
}

func GetEnv(key string, defaultVal string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}

	return defaultVal
}

func GetEnvAsInt(name string, defaultVal int) int {
	valueStr := GetEnv(name, "")
	if value, err := strconv.Atoi(valueStr); err == nil {
		return value
	}

	return defaultVal
}

func GetEnvAsBool(name string, defaultVal bool) bool {
	valueStr := GetEnv(name, "")
	if value, err := strconv.ParseBool(valueStr); err == nil {
		return value
	}

	return defaultVal
}

func OptionalString(src string, defaultStr string) string {
	if src == "" {
		return defaultStr
	}

	return src
}

func GetLogger(level ...interface{}) *zap.Logger {
	logLevel := determineLogLevel(level...)
	atom := zap.NewAtomicLevel()
	encoderCfg := getEncoderConfig()
	zapLevel := getLogLevel(logLevel)

	customHandler := &CustomLogHandler{minLevel: zapLevel}

	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderCfg),
		zapcore.AddSync(customHandler),
		atom,
	)

	baseFields := []zap.Field{
		zap.String("request_id", os.Getenv("REQUEST_ID")),
		zap.String("tenant_id", os.Getenv("TENANT_ID")),
		zap.String("thread", os.Getenv("THREAD")),
		zap.String("class", os.Getenv("CLASS")),
	}

	zapLogger := zap.New(core).With(baseFields...)
	atom.SetLevel(zapLevel)

	return zapLogger
}

func determineLogLevel(level ...interface{}) string {
	if len(level) > 0 {
		switch v := level[0].(type) {
		case string:
			return v
		case bool:
			if v {
				return "DEBUG"
			}
			return "INFO"
		}
	}
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "INFO"
	}
	return logLevel
}

func getLogLevel(level string) zapcore.Level {
	switch level {
	case "OFF":
		return zapcore.Level(6)
	case "FATAL":
		return zapcore.FatalLevel
	case "ERROR":
		return zapcore.ErrorLevel
	case "WARN":
		return zapcore.WarnLevel
	case "INFO":
		return zapcore.InfoLevel
	case "DEBUG":
		return zapcore.DebugLevel
	case "TRACE":
		return zapcore.Level(-1)
	default:
		return zapcore.InfoLevel
	}
}

func getEncoderConfig() zapcore.EncoderConfig {
	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.TimeKey = "timestamp"
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	return encoderCfg
}

type CustomLogHandler struct {
	minLevel zapcore.Level
}

func (h *CustomLogHandler) Write(p []byte) (n int, err error) {
	var logEntry map[string]interface{}
	if err := json.Unmarshal(p, &logEntry); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse log message: %s\n", p)
		return 0, fmt.Errorf("failed to parse log message")
	}

	levelStr := strings.ToUpper(fmt.Sprintf("%v", logEntry["level"]))
	timestamp := fmt.Sprintf("%v", logEntry["timestamp"])
	message := fmt.Sprintf("%v", logEntry["msg"])
	requestID := fmt.Sprintf("%v", logEntry["request_id"])
	tenantID := fmt.Sprintf("%v", logEntry["tenant_id"])
	thread := fmt.Sprintf("%v", logEntry["thread"])
	class := fmt.Sprintf("%v", logEntry["class"])

	output := fmt.Sprintf("[%s] [%s] [request_id=%s] [tenant_id=%s] [thread=%s] [class=%s] %s",
		timestamp, levelStr, requestID, tenantID, thread, class, message)

	standardFields := []string{
		"level", "timestamp", "msg",
		"request_id", "tenant_id", "thread", "class",
	}

	var additionalFields []string
	for key, value := range logEntry {
		if !slices.Contains(standardFields, key) {
			additionalFields = append(additionalFields, fmt.Sprintf("%s: %v", key, value))
		}
	}

	if len(additionalFields) > 0 {
		output = fmt.Sprintf("%s, %s", output, strings.Join(additionalFields, ", "))
	}

	fmt.Println(output)

	return len(p), nil
}

func AddLoggerContext(logger *zap.Logger, ctx context.Context) *zap.Logger {
	return logger.With(zap.ByteString("request_id", []byte(func() string {
		if v := ctx.Value("request_id"); v != nil {
			return fmt.Sprintf("%s", v)
		}
		return ""
	}())))
}

func IsTLSEnabledForMainService() bool {
	return GetEnv("TLS_ENABLED", "false") == "true"
}

func IsHttpsEnabled() bool {
	return GetEnv("INTERNAL_TLS_ENABLED", "false") == "true"
}

func ConfigureHttpsForClient(c *http.Client) error {
	return ConfigureHttpsForClientWithCertificate(c, certificateFilePath+"ca.crt")
}

func ConfigureHttpsForClientWithCertificate(c *http.Client, certPath string) error {
	rootCAs := x509.NewCertPool()

	// Read client cert file
	clientCertificates, err := ioutil.ReadFile(certPath)
	if err != nil {
		return err
	}

	// Append certificate to root CA
	if ok := rootCAs.AppendCertsFromPEM(clientCertificates); !ok {
		return err
	}

	c.Transport = &http.Transport{
		TLSClientConfig: &tls.Config{
			RootCAs:    rootCAs,
			MinVersion: tls.VersionTLS12,
			ClientAuth: tls.VerifyClientCertIfGiven,
			ClientCAs:  rootCAs,
		},
	}
	return nil
}

func GetNsAndMsName(metadata map[string]interface{}) (namespace string, msName string, err error) {
	if metadata == nil {
		err = fmt.Errorf("metadata is not provided")
		return
	}

	if classifierInt, ok := metadata[cKey]; ok {
		classifier, ok := classifierInt.(map[string]interface{})
		if !ok {
			err = fmt.Errorf("classifier type is not correct")
			return
		}
		namespace, ok = classifier[nsKey].(string)
		if !ok {
			err = fmt.Errorf("namespace is not string")
			return
		}
		msName, ok = classifier[msKey].(string)
		if !ok {
			err = fmt.Errorf("miscroserviceName is not string")
			return
		}
	} else {
		err = fmt.Errorf("classifier is not specified")
		return
	}

	if len(namespace) == 0 || len(msName) == 0 {
		err = fmt.Errorf("namespace or microservice name length is 0")
	}

	return
}

func GetTimestampStr() string {
	currentTime := time.Now().UTC()
	timestamp := currentTime.Format("150405.000.020106")
	timestamp = strings.ReplaceAll(timestamp, ".", "")
	return timestamp
}

func PrepareDatabaseName(namespace, microserviceName string, dbNameMaxLen int) (string, error) {
	timestamp := GetTimestampStr()
	timestampLen := len(timestamp)
	var currNameLength int

	//Check if provided maximum database length is no less than 'aa_bb_timestamp' pattern
	if dbNameMaxLen < lowestDbNameLengthByPattern {
		return timestamp, errors.New(" provided database name maximum length is lower than 21 symbols, which doesn't correlate with minimum default pattern")
	}

	dbName := fmt.Sprintf("%s_%s", microserviceName, namespace)
	if _, ok := getRealLength(dbNameMaxLen, dbName, timestamp); ok {
		return fmt.Sprintf("%s_%s", dbName, timestamp), nil
	}

	//Shrink name by deleting all vowels
	noVowelsMicroserviceName := excludeVowels(microserviceName)
	noVowelsNamespace := excludeVowels(namespace)
	if _, ok := getRealLength(dbNameMaxLen, noVowelsNamespace, noVowelsMicroserviceName, timestamp); ok {
		return fmt.Sprintf("%s_%s_%s", noVowelsMicroserviceName, noVowelsNamespace, timestamp), nil
	}

	//Shrink name parts one by one
	currNameLength, _ = getRealLength(dbNameMaxLen, dbName, timestamp)
	shortenedNamespace := shrinkName(namespace, currNameLength, dbNameMaxLen)
	if _, ok := getRealLength(dbNameMaxLen, microserviceName, shortenedNamespace, timestamp); ok {
		return fmt.Sprintf("%s_%s_%s", microserviceName, shortenedNamespace, timestamp), nil
	}
	currNameLength, _ = getRealLength(dbNameMaxLen, microserviceName, shortenedNamespace, timestamp)
	shortenedMicroserviceName := shrinkName(microserviceName, currNameLength, dbNameMaxLen)
	if _, ok := getRealLength(dbNameMaxLen, shortenedMicroserviceName, shortenedNamespace, timestamp); ok {
		return fmt.Sprintf("%s_%s_%s", shortenedMicroserviceName, shortenedNamespace, timestamp), nil
	}

	//Delete name parts starting from beginning
	//We assume that name is already looks like aa-bb-cc-dd_aa-dd-cc_timestamp
	dbName = fmt.Sprintf("%s_%s", shortenedMicroserviceName, shortenedNamespace)
	currNameLength, _ = getRealLength(dbNameMaxLen, shortenedMicroserviceName, shortenedNamespace, timestamp)
	toDeletePart := len(shortenedNamespace) + len(shortenedMicroserviceName) + timestampLen + 2 - dbNameMaxLen

	if toDeletePart%3 != 0 {
		toDeletePart = toDeletePart + (3 - toDeletePart%3)
	}
	dbName = dbName[:len(dbName)-toDeletePart]
	return fmt.Sprintf("%s_%s", dbName, timestamp), nil
}

// Shrink name parts until the needed minimum of name length is reached.
func shrinkName(name string, currentNameLen, dbNameMaxLen int) string {
	var result strings.Builder
	shortenedNameParts := regexp.MustCompile(regexNamePattern).Split(name, -1)
	separators := regexp.MustCompile(regexSeparatorPattern).Split(name, -1)
	for i := len(shortenedNameParts) - 1; i >= 0; i-- {
		shortenedNamePartLen := len(shortenedNameParts[i])
		if shortenedNamePartLen > 2 {
			result.WriteByte(shortenedNameParts[i][0])
			result.WriteByte(shortenedNameParts[i][shortenedNamePartLen-1])
			currentNameLen = currentNameLen - shortenedNamePartLen + 2
			shortenedNameParts[i] = result.String()
			result.Reset()
			if currentNameLen <= dbNameMaxLen {
				break
			}
		}
	}
	result.Reset()
	result.WriteString(shortenedNameParts[0])
	for i := 1; i < len(shortenedNameParts); i++ {
		result.WriteString(separators[i])
		result.WriteString(shortenedNameParts[i])
	}
	return result.String()
}

// Delete all vowels for provided name except first charters.
// Add the last symbol in case name is too short after vowels deletion.
// Example: "seo-service" -> "so-sc", not the "seo-service" -> "s-sc"
func excludeVowels(name string) string {
	var result strings.Builder
	nameParts := regexp.MustCompile(regexNamePattern).Split(name, -1)
	separators := regexp.MustCompile(regexSeparatorPattern).Split(name, -1)
	for i, word := range nameParts {
		if len(word) <= 2 {
			continue
		}
		result.Reset()
		result.WriteByte(word[0])
		for j := 1; j < len(word); j++ {
			if !strings.ContainsRune(vowels, rune(word[j])) {
				result.WriteByte(word[j])
			}
		}
		if result.Len() == 1 {
			result.WriteByte(word[len(word)-1])
		}
		nameParts[i] = result.String()
	}
	result.Reset()
	result.WriteString(nameParts[0])
	for i := 1; i < len(nameParts); i++ {
		result.WriteString(separators[i])
		result.WriteString(nameParts[i])
	}
	return result.String()
}

func getRealLength(dbMaxLen int, parts ...string) (int, bool) {
	var realLength int
	for _, part := range parts {
		realLength += len(part)
	}
	realLength += len(parts) - 1
	return realLength, realLength <= dbMaxLen
}

func RegenerateDbName(dbName string, maxDBNameLength int) string {
	var result string
	if maxDBNameLength-len(dbName)-DBMaxSuffixLength-1 >= 0 { // Max database name length - Max allowed suffix length - Delimiter length
		result = dbName + "_" + substr(uuid.Generate().String(), 0, DBMaxSuffixLength)
	} else if maxDBNameLength-len(dbName)-1 > 8 { // Handle the case when hypen is present and will be deleted, so we can add one more character
		result = dbName + "_" + substr(uuid.Generate().String(), 0, maxDBNameLength-len(dbName))
	} else {
		result = dbName + "_" + substr(uuid.Generate().String(), 0, maxDBNameLength-len(dbName)-1)
	}
	return strings.ReplaceAll(result, "-", "")
}

func substr(s string, start, end int) string {
	if end < 0 {
		return ""
	}

	counter, startIdx := 0, 0
	for i := range s {
		if counter == start {
			startIdx = i
		}
		if counter == end {
			return s[startIdx:i]
		}
		counter++
	}
	return s[startIdx:]
}
