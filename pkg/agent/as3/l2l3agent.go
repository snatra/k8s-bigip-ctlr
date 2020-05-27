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
	"time"

	log "github.com/F5Networks/k8s-bigip-ctlr/pkg/vlogger"
	"github.com/F5Networks/k8s-bigip-ctlr/pkg/writer"
	. "github.com/F5Networks/k8s-bigip-ctlr/pkg/resource")

type L2L3Agent struct {
	configWriter writer.Writer
	eventChan    chan interface{}
}

func (am *AS3Manager) SendFDBEntries() {
	// Organize the data as a map of arrays of resources (per partition)
	resources := PartitionMap{}

	// Filter the configs to only those that have active services
	for _, cfg := range am.Resources.RsCfgs {
		if cfg.MetaData.Active == true {
			initPartitionData(resources, cfg.GetPartition())
		}
	}

	doneCh, errCh, err := am.l2l3Agent.configWriter.SendSection("resources", resources)
	if nil != err {
		log.Warningf("[AS3] Failed to write FDB Records: %v", err)
		return
	}

	select {
	case <-doneCh:
		log.Infof("[AS3] Successfully Sent the FDB Records")
	case e := <-errCh:
		log.Warningf("[AS3] Failed to write FDB Records: %v", e)
	case <-time.After(time.Second):
		log.Warning("[AS3] Did not receive config write response in 1s")
	}
}

func (am *AS3Manager) SendARPEntries() {
	// Get all pool members and write them to VxlanMgr to configure ARP entries
	resources := PartitionMap{}
	var allPoolMembers []Member

	// Filter the configs to only those that have active services
	for _, cfg := range am.Resources.RsCfgs {
		if cfg.MetaData.Active == true {
			initPartitionData(resources, cfg.GetPartition())
			for _, p := range cfg.Pools {
				resources[p.Partition].Pools = appendPool(resources[p.Partition].Pools, p)
			}
		}
	}

	for _, cfg := range resources {
		for _, pool := range cfg.Pools {
			allPoolMembers = append(allPoolMembers, pool.Members...)
		}
	}

	if am.l2l3Agent.eventChan != nil {
		for member := range am.poolMembers {
			allPoolMembers = append(allPoolMembers, member)
		}

		select {
		case am.l2l3Agent.eventChan <- allPoolMembers:
			log.Debugf("[AS3] AppManager wrote endpoints to VxlanMgr")
		case <-time.After(3 * time.Second):
		}
	}
}

// Only append to the list if it isn't already in the list
func appendPool(rsPools []Pool, p Pool) []Pool {
	for i, rp := range rsPools {
		if rp.Name == p.Name &&
			rp.Partition == p.Partition {
			if len(p.Members) > 0 {
				rsPools[i].Members = p.Members
			}
			return rsPools
		}
	}
	return append(rsPools, p)
}
