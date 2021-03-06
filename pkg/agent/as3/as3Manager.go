/*-
 * Copyright (c) 2016-2019, F5 Networks, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package as3

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	. "github.com/F5Networks/k8s-bigip-ctlr/pkg/resource"
	log "github.com/F5Networks/k8s-bigip-ctlr/pkg/vlogger"
)

const (
	svcTenantLabel = "cis.f5.com/as3-tenant="
	svcAppLabel    = "cis.f5.com/as3-app="
	svcPoolLabel   = "cis.f5.com/as3-pool="
	baseAS3Config  = `{
  "$schema": "https://raw.githubusercontent.com/F5Networks/f5-appsvcs-extension/master/schema/3.18.0/as3-schema-3.18.0-4.json",
  "class": "AS3",
  "declaration": {
    "class": "ADC",
    "schemaVersion": "3.18.0",
    "id": "urn:uuid:B97DFADF-9F0D-4F6C-8D66-E9B52E593694",
    "label": "CIS Declaration",
    "remark": "Auto-generated by CIS",
    "controls": {
       "class": "Controls",
       "userAgent": "CIS Configured AS3"
    }
  }
}
`
	as3SupportedVersion  = 3.18
	as3tenant            = "Tenant"
	as3class             = "class"
	as3SharedApplication = "Shared"
	as3application       = "Application"
	as3shared            = "shared"
	as3template          = "template"
	//as3SchemaLatestURL   = "https://raw.githubusercontent.com/F5Networks/f5-appsvcs-extension/master/schema/latest/as3-schema.json"
	as3SchemaFileName = "as3-schema-3.18.0-4-cis.json"
)

// AS3Config consists of all the AS3 related configurations
type AS3Config struct {
	adc                as3ADC
	configmap          AS3ConfigMap
	overrideConfigmap  AS3ConfigMap
	unifiedDeclaration as3Declaration
}

// ActiveAS3ConfigMap user defined ConfigMap for global availability.
type AS3ConfigMap struct {
	Name      string         // AS3 specific ConfigMap name
	Namespace string         // AS3 specific ConfigMap namespace
	State     int            // State of the configMap
	cfg       string         // configuration in combination of "name/namespace" of cfgMap
	tmpData   string         // Holds AS3 template received from cfgMap resource.
	Data      as3Declaration // if AS3 Name is present, populate this with AS3 template data.
}

// AS3Manager holds all the AS3 orchestration specific Data
type AS3Manager struct {
	as3Validation             bool
	sslInsecure               bool
	enableTLS                 string
	tls13CipherGroupReference string
	ciphers                   string
	// Active User Defined ConfigMap details
	as3ActiveConfig AS3Config
	As3SchemaLatest string
	// Override existing as3 declaration with this configmap
	OverrideAS3Decl string
	// User defined AS3 declaration
	UserDefinedAS3Decl string
	// Path of schemas reside locally
	SchemaLocalPath string
	// POSTs configuration to BIG-IP using AS3
	PostManager *PostManager
	// To put list of tenants in BIG-IP REST call URL that are in AS3 declaration
	FilterTenants    bool
	DefaultPartition string
	ReqChan          chan MessageRequest
	RspChan          chan interface{}
	userAgent        string
	ResourceRequest
	ResourceResponse
}

// Struct to allow NewManager to receive all or only specific parameters.
type Params struct {
	// Package local for unit testing only
	SchemaLocal               string
	AS3Validation             bool
	SSLInsecure               bool
	EnableTLS                 string
	TLS13CipherGroupReference string
	Ciphers                   string
	//Agent                     string
	OverrideAS3Decl    string
	UserDefinedAS3Decl string
	SchemaLocalPath    string
	FilterTenants      bool
	BIGIPUsername      string
	BIGIPPassword      string
	BIGIPURL           string
	TrustedCerts       string
	AS3PostDelay       int
	//Log the AS3 response body in Controller logs
	LogResponse bool
	RspChan     chan interface{}
	UserAgent   string
}

// Create and return a new app manager that meets the Manager interface
func NewAS3Manager(params *Params) *AS3Manager {
	as3Manager := AS3Manager{
		as3Validation:             params.AS3Validation,
		sslInsecure:               params.SSLInsecure,
		enableTLS:                 params.EnableTLS,
		tls13CipherGroupReference: params.TLS13CipherGroupReference,
		ciphers:                   params.Ciphers,
		SchemaLocalPath:           params.SchemaLocal,
		FilterTenants:             params.FilterTenants,
		RspChan:                   params.RspChan,
		userAgent:                 params.UserAgent,
		as3ActiveConfig: AS3Config{
			configmap:         AS3ConfigMap{cfg: params.UserDefinedAS3Decl},
			overrideConfigmap: AS3ConfigMap{cfg: params.OverrideAS3Decl},
		},
		PostManager: NewPostManager(PostParams{
			BIGIPUsername: params.BIGIPUsername,
			BIGIPPassword: params.BIGIPPassword,
			BIGIPURL:      params.BIGIPURL,
			TrustedCerts:  params.TrustedCerts,
			SSLInsecure:   params.SSLInsecure,
			AS3PostDelay:  params.AS3PostDelay,
			LogResponse:   params.LogResponse}),
	}

	as3Manager.as3ActiveConfig.overrideConfigmap.Init()
	as3Manager.as3ActiveConfig.configmap.Init()

	as3Manager.fetchAS3Schema()

	return &as3Manager
}

func (am *AS3Manager) postAS3Declaration(rsReq ResourceRequest) (bool, string) {

	am.ResourceRequest = rsReq

	as3Config := am.as3ActiveConfig
	// Process cfgMap if present in Resource Request
	if am.ResourceRequest.AgentCfgmap != nil {
		for _, cfgMap := range am.ResourceRequest.AgentCfgmap {
			// Perform delete operation for cfgMap
			if cfgMap.Operation == OprTypeDelete {
				// Empty data is treated as delete operation for cfgMaps
				if ok, event := am.processAS3CfgMapDelete(cfgMap.Name, cfgMap.Namespace, &as3Config); !ok {
					log.Errorf("[AS3] Failed to perform delete cfgMap with name: %s and namespace %s",
						cfgMap.Name, cfgMap.Namespace)
					return ok, event
				}
				continue
			}
			am.processAS3ConfigMap(*cfgMap, &as3Config)
		}
	}

	// Process Route or Ingress
	as3ConfigReq := am.prepareAS3ResourceConfig(as3Config)

	return am.postAS3Config(as3ConfigReq)
}

func (am *AS3Manager) postAS3Config(tempAS3Config AS3Config) (bool, string) {
	unifiedDecl := am.getUnifiedDeclaration(&tempAS3Config)
	if unifiedDecl == "" {
		return true, ""
	}

	if DeepEqualJSON(am.as3ActiveConfig.unifiedDeclaration, unifiedDecl) {
		return true, ""
	}

	if am.as3Validation == true {
		if ok := am.validateAS3Template(string(unifiedDecl)); !ok {
			return true, ""
		}
	}

	log.Debugf("[AS3] Posting AS3 Declaration")

	am.as3ActiveConfig.updateConfig(tempAS3Config)

	am.sendFDBRecords()

	var tenants []string = nil

	if am.FilterTenants {
		tenants = getTenants(unifiedDecl)
	}

	return am.PostManager.postConfig(string(unifiedDecl), tenants)
}

func (cfg *AS3Config) updateConfig(newAS3Cfg AS3Config) {
	cfg.adc = newAS3Cfg.adc
	cfg.unifiedDeclaration = newAS3Cfg.unifiedDeclaration
	cfg.configmap = newAS3Cfg.configmap
}

func (am *AS3Manager) getUnifiedDeclaration(cfg *AS3Config) as3Declaration {
	if cfg.adc == nil && cfg.configmap.Data == "" {
		return ""
	}

	// Need to process Routes
	var as3Obj map[string]interface{}
	if cfg.configmap.Data != "" {
		// Merge activeCfgMap and as3RouteCfg
		_ = json.Unmarshal([]byte(cfg.configmap.Data), &as3Obj)
	} else {
		// Merge base AS3 template and as3RouteCfg
		_ = json.Unmarshal([]byte(baseAS3Config), &as3Obj)
	}

	if cfg.adc != nil {
		adc, _ := as3Obj["declaration"].(map[string]interface{})

		for k, v := range cfg.adc {
			adc[k] = v
		}
	}

	unifiedDecl, err := json.Marshal(as3Obj)
	if err != nil {
		log.Debugf("[AS3] Unified declaration: %v\n", err)
	}

	if string(cfg.overrideConfigmap.Data) == "" {
		cfg.unifiedDeclaration = as3Declaration(unifiedDecl)
		return as3Declaration(unifiedDecl)
	}

	overriddenUnifiedDecl := ValidateAndOverrideAS3JsonData(string(cfg.overrideConfigmap.Data),
		string(unifiedDecl))
	if overriddenUnifiedDecl == "" {
		log.Debug("[AS3] Failed to override AS3 Declaration")
		cfg.overrideConfigmap.errorState()
		am.as3ActiveConfig.overrideConfigmap = cfg.overrideConfigmap
		cfg.unifiedDeclaration = as3Declaration(unifiedDecl)
		return as3Declaration(unifiedDecl)
	}
	cfg.overrideConfigmap.activeState()
	cfg.unifiedDeclaration = as3Declaration(overriddenUnifiedDecl)
	return as3Declaration(overriddenUnifiedDecl)
}

// Function to prepare empty AS3 declaration
func (am *AS3Manager) getEmptyAs3Declaration(partition string) as3Declaration {
	var as3Config map[string]interface{}
	_ = json.Unmarshal([]byte(baseAS3Config), &as3Config)
	decl := as3Config["declaration"].(map[string]interface{})

	controlObj := make(as3Control)
	controlObj.initDefault(am.userAgent)
	decl["controls"] = controlObj
	if partition != "" {

		decl[partition] = map[string]string{"class": "Tenant"}
	}
	data, _ := json.Marshal(as3Config)
	return as3Declaration(data)
}

// Method to delete any AS3 partition
func (am *AS3Manager) DeleteAS3Partition(partition string) (bool, string) {
	tempAS3Config := am.as3ActiveConfig
	tempAS3Config.configmap.Data = am.getEmptyAs3Declaration(partition)
	nilDecl := am.getUnifiedDeclaration(&tempAS3Config)
	if nilDecl == "" {
		return true, ""
	}

	return am.PostManager.postConfig(string(nilDecl), nil)
}

func (c AS3Config) Init(partition string) {
	c.adc = as3ADC{}
	c.adc.initDefault(partition)
	c.configmap.Init()
	c.overrideConfigmap.Init()
}

// fetchAS3Schema ...
func (am *AS3Manager) fetchAS3Schema() {
	log.Debugf("[AS3] Validating AS3 schema with  %v", as3SchemaFileName)
	am.As3SchemaLatest = am.SchemaLocalPath + as3SchemaFileName
	return
}

// configDeployer blocks on ReqChan
// whenever gets unblocked posts active configuration to BIG-IP
func (am *AS3Manager) ConfigDeployer() {
	// For the very first post after starting controller, need not wait to post
	firstPost := true
	for msgReq := range am.ReqChan {

		if !firstPost && am.PostManager.AS3PostDelay != 0 {
			// Time (in seconds) that CIS waits to post the AS3 declaration to BIG-IP.
			_ = <-time.After(time.Duration(am.PostManager.AS3PostDelay) * time.Second)
		}

		// After postDelay expires pick up latest declaration, if available
		select {
		case msgReq = <-am.ReqChan:
		case <-time.After(1 * time.Microsecond):
		}

		posted, event := am.postAS3Declaration(msgReq.ResourceRequest)
		// To handle general errors
		for !posted {
			timeout := getTimeDurationForErrorResponse(event)
			log.Debugf("[AS3] Error handling for event %v", event)
			posted, event = am.postOnEventOrTimeout(timeout)
		}
		firstPost = false
		if event == responseStatusOk {
			log.Debugf("[AS3] Preparing response message to response handler")
			am.sendARPRequest()
			log.Debugf("[AS3] Sent response message to response handler")
		}
	}
}

// Helper method used by configDeployer to handle error responses received from BIG-IP
func (am *AS3Manager) postOnEventOrTimeout(timeout time.Duration) (bool, string) {
	select {
	case msgReq := <-am.ReqChan:
		return am.postAS3Declaration(msgReq.ResourceRequest)
	case <-time.After(timeout):
		if am.as3ActiveConfig.configmap.isDeletePending() {
			return am.processAS3CfgMapDelete(am.as3ActiveConfig.configmap.Name,
				am.as3ActiveConfig.configmap.Namespace,
				&am.as3ActiveConfig)
		}
		tenants := getTenants(am.as3ActiveConfig.unifiedDeclaration)
		unifiedDeclaration := string(am.as3ActiveConfig.unifiedDeclaration)
		return am.PostManager.postConfig(unifiedDeclaration, tenants)
	}
}

// Post FDB records on response channel
func (am *AS3Manager) sendFDBRecords() {
	agRsp := ResourceResponse{}
	agRsp.FdbRecords = true
	am.postAgentResponse(MessageResponse{ResourceResponse: agRsp})
}

// Post ARP entries over response channel
func (am *AS3Manager) sendARPRequest() {
	agRsp := am.ResourceResponse
	agRsp.AdmitStatus = true
	am.postAgentResponse(MessageResponse{ResourceResponse: agRsp})
}

// Method implements posting MessageResponse on Agent Response Channel
func (am *AS3Manager) postAgentResponse(msgRsp MessageResponse) {
	select {
	case am.RspChan <- msgRsp:
	case <-am.RspChan:
		am.RspChan <- msgRsp
	}
}

// Method to verify if App Services are installed or CIS as3 version is
// compatible with BIG-IP, it will return with error if any one of the
// requirements are not met
func (am AS3Manager) IsBigIPAppServicesAvailable() error {
	version, err := am.PostManager.GetBigipAS3Version()
	if err != nil {
		log.Errorf("[AS3] %v ", err)
		return err
	}
	bigIPVersion, err := strconv.ParseFloat(version, 64)
	if err != nil {
		log.Errorf("[AS3] Error while converting AS3 version to float")
		return err
	}
	if bigIPVersion >= as3SupportedVersion {
		log.Debugf("[AS3] BIGIP is serving with AS3 version: %v", version)
		return nil
	}

	return fmt.Errorf("CIS versions >= 2.0 are compatible with AS3 versions >= %v. "+
		"Upgrade AS3 version in BIGIP from %v to %v or above.", as3SupportedVersion,
		bigIPVersion, as3SupportedVersion)
}
