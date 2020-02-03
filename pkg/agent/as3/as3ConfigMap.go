package as3

import (
	"encoding/json"
	"strings"

	. "github.com/F5Networks/k8s-bigip-ctlr/pkg/resource"
	log "github.com/F5Networks/k8s-bigip-ctlr/pkg/vlogger"
)

// cfgMap States
const (
	cmInit = iota + 1
	cmActive
	cmError
)

func (m *AS3Manager) prepareUserDefinedAS3Declaration(cm AgentCfgMap) {
	if m.as3ActiveConfig.configmap.inErrorState(cm.Data) ||
		m.as3ActiveConfig.configmap.alreadyProcessed(cm.Data) {
		return
	}

	m.as3ActiveConfig.configmap.tmpData = cm.Data

	m.as3ActiveConfig.configmap.Data =
		m.generateUserDefinedAS3Decleration(cm)
	if m.as3ActiveConfig.configmap.Data == "" {
		log.Errorf("[AS3] Error while processing user defined AS3 cfgMap Name: %v",
			m.as3ActiveConfig.configmap.Name)
		m.as3ActiveConfig.configmap.errorState()
		return
	}

	m.as3ActiveConfig.configmap.activeState()

	return
}

// Takes an AS3 Template and perform service discovery with Kubernetes to generate AS3 Declaration
func (m *AS3Manager) generateUserDefinedAS3Decleration(cm AgentCfgMap) as3Declaration {

	if m.as3Validation == true {
		if ok := m.validateAS3Template(cm.Data); !ok {
			log.Errorf("[AS3] Error validating AS3 template")
			return ""
		}
	}
	templateObj := as3Template(cm.Data)
	obj, ok := getAS3ObjectFromTemplate(templateObj)

	if !ok {
		log.Errorf("[AS3] Error processing template\n")
		return ""
	}

	_, found := obj[tenantName(DEFAULT_PARTITION)]
	_, foundNetworkPartition := obj[tenantName(strings.TrimSuffix(DEFAULT_PARTITION, "_AS3"))]
	if found || foundNetworkPartition {
		log.Error("[AS3] Error in processing the template")
		log.Errorf("[AS3] CIS managed partitions <%s> and <%s> should not be used in ConfigMap as Tenants",
			DEFAULT_PARTITION, strings.TrimSuffix(DEFAULT_PARTITION, "_AS3"))
		return ""
	}

	return m.buildAS3Declaration(obj, templateObj, cm)
}

func (c *AS3Config) prepareAS3OverrideDeclaration(data string) {
	if c.overrideConfigmap.inErrorState(data) || c.overrideConfigmap.alreadyProcessed(data) {
		return
	}

	c.overrideConfigmap.tmpData = data

	if !DeepEqualJSON(c.overrideConfigmap.Data, as3Declaration(data)) {
		c.overrideConfigmap.Data = as3Declaration(data)
		if c.unifiedDeclaration != "" && !c.isDefaultAS3PartitionEmpty() {
			return
		}
		log.Debugf("[AS3] Saving AS3 override, no active configuration available in CIS")
	}

	return
}

func (cm *AS3ConfigMap) prepareDeleteUserDefinedAS3() bool {
	log.Debugf("[AS3] Deleteing User Defined Configmap: %v", cm.Name)
	defer cm.Reset()
	if tntList := getTenants(cm.Data); tntList != nil {
		var tmpl interface{}
		err := json.Unmarshal([]byte(cm.Data), &tmpl)
		if err != nil {
			log.Errorf("[AS3] JSON unmarshal failed: %v\n", err)
			return false
		}

		// extract as3 declaration from template
		dclr := (tmpl.(map[string]interface{}))["declaration"].(map[string]interface{})
		if dclr == nil {
			log.Error("[AS3] No ADC class declaration found.")
			return false
		}

		for _, tnt := range tntList {
			if tnt == "controls"{ // Skipping Controls Object
				continue
			}
			tntObj := as3Tenant{}
			tntObj.initDefault()
			dclr[tnt] = tntObj
		}

		declaration, err := json.Marshal(tmpl.(map[string]interface{}))
		if err != nil {
			log.Errorf("[AS3] Issue marshalling AS3 Json")
			return false
		}

		cm.Data = as3Declaration(declaration)
	}
	log.Debugf("[AS3] Declaration for Delete User Defined Configmap: %v", cm.Data)
	return true
}

func (m *AS3Manager) processAS3ConfigMap(cm AgentCfgMap) {
	cfg := &m.as3ActiveConfig
	name := cm.Name
	namespace := cm.Namespace
	data := cm.Data

	// Perform delete operation for cfgMap
	if data == "" {
		// Empty data is treated as delete operation for cfgMaps
		if !m.processAS3CfgMapDelete(name) {
			log.Errorf("[AS3] Failed to perform delete cfgMap with name: %s and namespace %s",
				name, namespace)
		}
		return
	}

	label, ok := isValidAS3CfgMap(cm.Label)
	if !ok {
		return
	}

	if cfg.overrideConfigmap.Name == "" || cfg.configmap.Name == ""{
		switch label{
		case "as3":
			cfg.configmap.Name = name
		case "overrideAS3":
			cfg.overrideConfigmap.Name = name
		}
	}

	switch name {
	case cfg.overrideConfigmap.Name:
		m.as3ActiveConfig.prepareAS3OverrideDeclaration(data)
		return

	case cfg.configmap.Name:
		m.prepareUserDefinedAS3Declaration(cm)
		return
	}

	// If none of the above cases doesn't match, reason can be
	// override or userdfined cfgMap might not be configured in CIS.
	cfg.cfgMapNotConfigured(label, namespace, name)

	return
}

// Takes AS3 template and AS3 Object and produce AS3 Declaration
func (m *AS3Manager) buildAS3Declaration(obj as3Object, template as3Template, cm AgentCfgMap) as3Declaration {

	var tmp interface{}

	// unmarshall the template of type string to interface
	err := json.Unmarshal([]byte(template), &tmp)
	if nil != err {
		return ""
	}

	// convert tmp to map[string]interface{}, This conversion will help in traversing the as3 object
	templateJSON := tmp.(map[string]interface{})

	// Support `Controls` class for TEEMs in user-defined AS3 configMap.
	declarationObj := (templateJSON["declaration"]).(map[string]interface{})
	controlObj := make(map[string]interface{})
	controlObj["class"] = "Controls"
	controlObj["userAgent"] = "CIS Configured AS3"
	declarationObj["controls"] = controlObj

	// traverse through the as3 object to fetch the list of services and get endpopints using the servicename
	log.Debugf("[AS3] Started Parsing the AS3 Object")

	// Initialize Pool members
	m.ResourceResponse.Members = make(map[Member]struct{})
	for tnt, apps := range obj {
		for app, pools := range apps {
			for _, pn := range pools {
				eps := cm.GetEndpoints(m.getSelector(tnt, app, pn))
				// Handle an empty value
				if len(eps) == 0 {
					continue
				}
				ips := make([]string, 0)
				for _, v := range eps {
					ips = append(ips, v.Address)
					m.ResourceResponse.Members[v] = struct{}{}
				}
				port := eps[0].Port
				log.Debugf("Updating AS3 Template for tenant '%s' app '%s' pool '%s', ", tnt, app, pn)
				updatePoolMembers(tnt, app, pn, ips, port, templateJSON)
			}
		}
	}

	declaration, err := json.Marshal(templateJSON)

	if err != nil {
		log.Errorf("[AS3] Issue marshalling AS3 Json")
	}
	log.Debugf("[AS3] AS3 Template is populated with the pool members")

	return as3Declaration(declaration)
}

func (appMgr *AS3Manager) processAS3CfgMapDelete(name string) bool {
	switch name {
	case appMgr.as3ActiveConfig.overrideConfigmap.Name:
		log.Debugf("[AS3] Deleting Override Config Map %v", name)
		appMgr.as3ActiveConfig.overrideConfigmap.Reset()
		appMgr.as3ActiveConfig.overrideConfigmap.Data = ""
		return true

	case appMgr.as3ActiveConfig.configmap.Name:
		return appMgr.as3ActiveConfig.configmap.prepareDeleteUserDefinedAS3()
	}
	return false
}

func (m *AS3Manager) getSelector(tenant tenantName, app appName, pool poolName) string {
	log.Debugf("[AS3] Discovering endpoints for pool: [%v -> %v -> %v]", tenant, app, pool)

	tenantKey := "cis.f5.com/as3-tenant="
	appKey := "cis.f5.com/as3-app="
	poolKey := "cis.f5.com/as3-pool="

	return tenantKey + string(tenant) + "," +
		appKey + string(app) + "," +
		poolKey + string(pool)
}

func (cm *AS3ConfigMap) isUniqueName(cmName string) bool {
	if cm.Name == cmName {
		return true
	}
	return false
}

func (cm *AS3ConfigMap) inErrorState(data string) bool {
	if cm.State == cmError {
		if DeepEqualJSON(as3Declaration(cm.tmpData), as3Declaration(data)) {
			log.Errorf("[AS3] Configuration in cfgMap %v is invalid, please correct it", cm.Name)
			return true
		}
	}
	return false
}

func (cm *AS3ConfigMap) alreadyProcessed(data string) bool {
	if cm.State == cmActive {
		if DeepEqualJSON(as3Declaration(cm.tmpData), as3Declaration(data)) {
			return true
		}
	}
	return false
}

func (cm *AS3ConfigMap) errorState() {
	cm.State = cmError
	if cm.cfg == ""{
		cm.Reset()
	}
}

func (cm *AS3ConfigMap) activeState() {
	cm.State = cmActive
}

func (cm *AS3ConfigMap) Init() {
	cfg := strings.Split(cm.cfg, "/")
	if len(cfg) == 2 {
		cm.Namespace = cfg[0]
		cm.Name = cfg[1]
	}
	cm.Data = ""
	cm.tmpData = ""
	cm.State = cmInit
}

func (cm *AS3ConfigMap) Reset() {
	cm.tmpData = ""
	if cm.cfg == ""{
		cm.Name = ""
	}
	cm.State = cmInit
}

func (c AS3Config) cfgMapNotConfigured(cmType, namespace, name string) {
	switch cmType {
	case "overrideAS3":
		log.Debugf("[AS3] User defined AS3 configMap with namespace %v"+
			" and name %v cannot be processed, please check --override-as3-declaration option in CIS",
			namespace, name)
	case "as3":
		log.Debugf("[AS3] Override AS3 configMap with namespace %v"+
			" and name %v cannot be processed, please check --userdefined-as3-declaration option in CIS",
			namespace, name)
	}
}

func (c *AS3Config) setCfgMap(cmType, name, namespace string) {
	switch cmType {
	case "as3":
		c.configmap.Name = name
		c.configmap.Namespace = namespace
	case "overrideAS3":
		c.overrideConfigmap.Name = name
		c.overrideConfigmap.Namespace = namespace
	}
	return
}
