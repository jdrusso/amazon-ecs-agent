// Copyright 2014-2015 Amazon.com, Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//	http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package stats

import (
	"path/filepath"
	"time"

	"github.com/docker/libcontainer"
	"golang.org/x/net/context"
)

const (
	// DockerExecDriverPath points to the docker exec driver path.
	DockerExecDriverPath = "execdriver/native"

	// SleepBetweenUsageDataCollection is the sleep duration between collecting usage data for a container.
	SleepBetweenUsageDataCollection = 500 * time.Millisecond

	// ContainerStatsBufferLength is the number of usage metrics stored in memory for a container. It is calculated as
	// Number of usage metrics gathered in a second (2) * 60 * Time duration in minutes to store the data for (2)
	ContainerStatsBufferLength = 240
)

// ContainerStatsCollector defines methods to get container stats. This interface is defined to
// make testing easier.
type ContainerStatsCollector interface {
	getContainerStats(container *CronContainer) (*ContainerStats, error)
}

// LibcontainerStatsCollector implements ContainerStatsCollector.
type LibcontainerStatsCollector struct{}

// StartStatsCron starts a go routine to periodically pull usage data for the container.
func (container *CronContainer) StartStatsCron() {
	// Create the queue to store utilization data from cgroup fs.
	container.statsQueue = NewQueue(ContainerStatsBufferLength)

	// Create the context to handle deletion of container from the manager.
	// The manager can cancel the cronStats go routing by calling StopStatsCron method.
	container.ctx, container.cancel = context.WithCancel(context.Background())
	go container.cronStats()
}

// StopStatsCron stops the periodic collection of usage data for the container..
func (container *CronContainer) StopStatsCron() {
	container.cancel()
}

// newCronContainer creates a CronContainer object.
func newCronContainer(dockerID *string, dockerGraphPath string) *CronContainer {
	statePath := filepath.Join(dockerGraphPath, DockerExecDriverPath, *dockerID)

	container := &CronContainer{
		containerMetadata: &ContainerMetadata{
			DockerID: dockerID,
		},
		statePath: statePath,
	}

	container.statsCollector = &LibcontainerStatsCollector{}
	return container
}

// cronStats periodically pulls usage data for the container from cgroup fs.
func (container *CronContainer) cronStats() {
	for {
		select {
		case <-container.ctx.Done():
			return
		default:
			stats, err := container.statsCollector.getContainerStats(container)
			if err != nil {
				log.Debug("Error getting stats", "error", err, "contianer", container)
			} else {
				container.statsQueue.Add(stats)
			}
			time.Sleep(SleepBetweenUsageDataCollection)
		}
	}
}

// getContainerStats reads usage data of a container from the cgroup fs.
func (collector *LibcontainerStatsCollector) getContainerStats(container *CronContainer) (*ContainerStats, error) {
	state, err := libcontainer.GetState(container.statePath)
	if err != nil {
		// The state file is not created immediately when a container starts.
		// Bubble up the error.
		return nil, err
	}
	// libcontainer.GetStats ignores the config argument. So, don't bother providing one.
	containerStats, err := libcontainer.GetStats(nil, state)
	if err != nil && !isNetworkStatsError(err) {
		log.Error("Error getting libcontainer stats", "err", err)
		return nil, err
	}

	cs := toContainerStats(*containerStats)
	return cs, nil
}
