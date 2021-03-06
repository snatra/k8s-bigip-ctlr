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
	cmDeletePending
)

func (am *AS3Manager) prepareUserDefinedAS3Declaration(cm AgentCfgMap, cfg *AS3Config) {
	if am.as3ActiveConfig.configmap.isDeletePending() {
		log.Warningf("[AS3] Cannot Create/Update, deletion of User Defined AS3 cfgMap: %v is in pending state",
			cfg.configmap.Name)
		return
	}

	if am.as3ActiveConfig.configmap.inErrorState(cm.Data) {
		return
	}

	cfg.configmap.tmpData = cm.Data

	data := am.generateUserDefinedAS3Decleration(cm)
	if data == "" {
		log.Errorf("[AS3] Error while processing user defined AS3 cfgMap Name: %v",
			cfg.configmap.Name)
		cfg.configmap.errorState()
		am.as3ActiveConfig.configmap = cfg.configmap
		return
	}

	cfg.configmap.Data = data

	cfg.configmap.activeState()
	return
}

// Takes an AS3 Template and perform service discovery with Kubernetes to generate AS3 Declaration
func (am *AS3Manager) generateUserDefinedAS3Decleration(cm AgentCfgMap) as3Declaration {
	if am.as3Validation == true {
		if ok := am.validateAS3Template(cm.Data); !ok {
			log.Errorf("[AS3] Error validating AS3 template")
			return ""
		}
	}

	if am.as3ActiveConfig.configmap.Data != "" {
		// Handle Delete partitions if modified in cfgMap
		oldTenants := getTenants(am.as3ActiveConfig.configmap.Data)
		newTenants := getTenants(as3Declaration(cm.Data))
		newTntMap := make(map[string]bool)
		for _, tnt := range newTenants {
			newTntMap[tnt] = true
		}

		for _, tnt := range oldTenants {
			if _, ok := newTntMap[tnt]; !ok {
				am.DeleteAS3Partition(tnt)
			}
		}

		if len(newTenants) == 0 {
			return am.getEmptyAs3Declaration("")
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

	return am.buildAS3Declaration(obj, templateObj, cm)
}

// Method to prepare AS3 override declaration
func (c *AS3Config) prepareAS3OverrideDeclaration(data string) {
	if c.overrideConfigmap.inErrorState(data) || c.overrideConfigmap.alreadyProcessed(data) {
		return
	}

	c.overrideConfigmap.tmpData = data

	if !DeepEqualJSON(c.overrideConfigmap.Data, as3Declaration(data)) {
		c.overrideConfigmap.Data = as3Declaration(data)
		if c.unifiedDeclaration != "" && !c.isDefaultAS3PartitionEmpty() {
			c.overrideConfigmap.State = cmActive
			return
		}
		c.overrideConfigmap.State = cmActive
		log.Warningf("[AS3] Saving AS3 override, no active configuration available in CIS")
	}

	return
}

// Method to perform deletion operation on userdefined-as3-cfgmap
func (am AS3Manager) prepareDeleteUserDefinedAS3(cm *AS3ConfigMap) (bool, string) {
	log.Debugf("[AS3] Deleting User Defined Configmap: %v", cm.Name)
	// Fetch all tenants of userdefined-as3-cfgmap
	if tntList := getTenants(cm.Data); tntList != nil {
		for _, tnt := range tntList {
			// Perform deletion for each tenant
			if ok, event := am.DeleteAS3Partition(tnt); !ok {
				am.as3ActiveConfig.configmap.deletePending()
				return false, event
			}
		}
	}
	cm.Data = ""
	cm.Reset()
	return true, ""
}

// method to process AS3 configMaps
func (am *AS3Manager) processAS3ConfigMap(cm AgentCfgMap, cfg *AS3Config) {
	name := cm.Name
	namespace := cm.Namespace
	data := cm.Data

	// Check if the cfgMap is valid, if valid it returns valid
	// label, for further processing
	label, ok := cfg.isValidAS3CfgMap(name, namespace, cm.Label)
	if !ok {
		return
	}

	// Prepare right cfgMap to be processed
	cfg.setCfgMap(label, name, namespace)

	switch label {
	case "overrideAS3":
		if name == cfg.overrideConfigmap.Name {
			cfg.prepareAS3OverrideDeclaration(data)
			if cfg.overrideConfigmap.State == cmActive {
				am.as3ActiveConfig.overrideConfigmap = cfg.overrideConfigmap
			}
			return
		}
	case "as3":
		if name == cfg.configmap.Name {
			am.prepareUserDefinedAS3Declaration(cm, cfg)
			return
		}
	}

	// If none of the above cases doesn't match, reason can be
	// override or userdfined cfgMap might not be configured in CIS.
	cfgMapNotConfigured(label, namespace, name)

	return
}

// Takes AS3 template and AS3 Object and produce AS3 Declaration
func (am *AS3Manager) buildAS3Declaration(obj as3Object, template as3Template, cm AgentCfgMap) as3Declaration {

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
	controlObj := make(as3Control)
	controlObj.initDefault(am.userAgent)
	declarationObj["controls"] = controlObj

	// Initialize Pool members
	members := make(map[Member]struct{})
	isPoolUpdated := false
	for tnt, apps := range obj {
		for app, pools := range apps {
			for _, pn := range pools {
				eps := cm.GetEndpoints(am.getSelector(tnt, app, pn))
				// Handle an empty value
				if len(eps) == 0 {
					continue
				}
				ips := make([]string, 0)
				for _, v := range eps {
					ips = append(ips, v.Address)
					if _, ok := am.ResourceResponse.Members[v]; !ok {
						isPoolUpdated = true
					}
					members[v] = struct{}{}
				}
				port := eps[0].Port
				if isPoolUpdated {
					log.Debugf("[AS3] Updating AS3 Template for tenant '%s' app '%s' pool '%s', ", tnt, app, pn)
					isPoolUpdated = false
				}
				updatePoolMembers(tnt, app, pn, ips, port, templateJSON)
			}
		}
	}

	am.ResourceResponse.Members = members

	declaration, err := json.Marshal(templateJSON)

	if err != nil {
		log.Errorf("[AS3] Issue marshalling AS3 Json")
	}

	return as3Declaration(declaration)
}

// Method to perform delete operations on AS3 cfgMaps(Override and User-define)
func (am *AS3Manager) processAS3CfgMapDelete(name, namespace string, cfg *AS3Config) (bool, string) {
	// Perform delete operation if override-as3-cfgMap
	if name == cfg.overrideConfigmap.Name && namespace == cfg.overrideConfigmap.Namespace {
		log.Debugf("[AS3] Deleting Override Config Map %v", name)
		cfg.overrideConfigmap.Reset()
		cfg.overrideConfigmap.Data = ""
		am.as3ActiveConfig.overrideConfigmap = cfg.overrideConfigmap
		return true, ""
	}

	// Perform delete operation if userdefined-as3-cfgMap
	if name == cfg.configmap.Name && namespace == cfg.configmap.Namespace {
		am.as3ActiveConfig.configmap.Reset()
		am.as3ActiveConfig.configmap.Data = ""
		return am.prepareDeleteUserDefinedAS3(&cfg.configmap)
	}
	return true, ""
}

// Method prepares and returns the label selector in string format
func (am *AS3Manager) getSelector(tenant tenantName, app appName, pool poolName) string {
	return svcTenantLabel + string(tenant) + "," +
		svcAppLabel + string(app) + "," +
		svcPoolLabel + string(pool)
}

// Method to verify if configMap in error state
func (cm *AS3ConfigMap) inErrorState(data string) bool {
	if cm.State == cmError {
		if DeepEqualJSON(as3Declaration(cm.tmpData), as3Declaration(data)) {
			if cm.cfg == "" {
				log.Errorf("[AS3] Configuration in cfgMap %v is invalid, please correct it", cm.Name)
			}
			return true
		}
	}
	return false
}

// Method to verify if the cfgMap in Active State
func (cm *AS3ConfigMap) alreadyProcessed(data string) bool {
	if cm.State == cmActive {
		if DeepEqualJSON(as3Declaration(cm.tmpData), as3Declaration(data)) {
			return true
		}
	}
	return false
}

// Method used to set the configMap into error state
func (cm *AS3ConfigMap) errorState() {
	cm.State = cmError
	if cm.cfg == "" {
		cm.Reset()
	}
}

// Method used to set the configMap into error state
func (cm *AS3ConfigMap) isDeletePending() bool {
	if cm.State == cmDeletePending {
		return true
	}
	return false
}

// Method used to set the configMap into active state
func (cm *AS3ConfigMap) activeState() {
	cm.State = cmActive
}

// Method used to set the configMap into active state
func (cm *AS3ConfigMap) deletePending() {
	cm.State = cmDeletePending
}

// Method to initialize cfgMap
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
	if cm.cfg == "" {
		cm.Name = ""
		cm.Namespace = ""
	}
	cm.State = cmInit
}

func cfgMapNotConfigured(cmType, namespace, name string) {
	switch cmType {
	case "overrideAS3":
		log.Debugf("[AS3] Override AS3 configMap with namespace %v"+
			" and name %v cannot be processed, please check --override-as3-declaration option in CIS",
			namespace, name)
	case "as3":
		log.Debugf("[AS3] User defined AS3 configMap with namespace %v"+
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

func (c AS3Config) isValidAS3CfgMap(name, namespace string, labels map[string]string) (string, bool) {
	label := ""
	if val, ok := labels["f5type"]; ok && val == "virtual-server" {
		if val, ok := labels["overrideAS3"]; ok && val == "true" {
			label = "overrideAS3"
		} else if val, ok := labels["as3"]; ok && val == "true" {
			label = "as3"
		} else {
			return "", false
		}
	}

	switch label {
	case "overrideAS3":
		if c.overrideConfigmap.Namespace != "" || c.overrideConfigmap.Name != "" {
			if c.overrideConfigmap.Namespace != namespace || c.overrideConfigmap.Name != name {
				if c.overrideConfigmap.cfg == "" {
					log.Errorf("[AS3] Invalid override cfgMap with name: %s and namespace %s", name, namespace)
					return "", false
				}
			}
		}
	case "as3":
		if c.configmap.Namespace != "" || c.configmap.Name != "" {
			if c.configmap.Namespace != namespace || c.configmap.Name != name {
				if c.configmap.cfg == "" {
					log.Errorf("[AS3] Invalid user-defined cfgMap with name: %s and namespace %s", name, namespace)
					return "", false
				}
			}
		}
	}

	return label, true
}
