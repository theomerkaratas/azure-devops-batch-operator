package azuredevops

import (
	"fmt"
	"sync"
)

var (
	queueCacheMu sync.Mutex
	queueCache   = map[string]map[int]string{}
)

type buildDefinitionsResponse struct {
	Value []struct {
		Queue struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
			Pool struct {
				Name string `json:"name"`
			} `json:"pool"`
		} `json:"queue"`
	} `json:"value"`
}

// GetProjectQueues derives a queueId -> pool/queue name map from a project's build definitions.
// This is the most reliable way to resolve a pool name with only "read" access, since the PAT
// may not have access to the distributedtask queues API.
func (c Config) GetProjectQueues(project string) map[int]string {
	queueCacheMu.Lock()
	if cached, ok := queueCache[project]; ok {
		queueCacheMu.Unlock()
		return cached
	}
	queueCacheMu.Unlock()

	mapping := map[int]string{}
	url := fmt.Sprintf("%s/%s/%s/_apis/build/definitions?api-version=6.0", c.OrgURL, Collection, project)
	var resp buildDefinitionsResponse
	if err := c.Get(url, &resp); err == nil {
		for _, item := range resp.Value {
			if item.Queue.ID == 0 {
				continue
			}
			poolName := item.Queue.Pool.Name
			if poolName == "" {
				poolName = item.Queue.Name
			}
			if poolName != "" {
				mapping[item.Queue.ID] = poolName
			}
		}
	}

	queueCacheMu.Lock()
	queueCache[project] = mapping
	queueCacheMu.Unlock()
	return mapping
}

type distributedTaskQueue struct {
	Name string `json:"name"`
	Pool struct {
		Name string `json:"name"`
	} `json:"pool"`
}

// ResolvePoolName resolves a deployment queueId to a human-readable pool name, preferring the
// build-definitions-derived map and falling back to the distributedtask queues API (which
// requires a higher PAT level than "read").
func (c Config) ResolvePoolName(project string, queueID int) string {
	if queueID == 0 {
		return "Not specified"
	}

	if name, ok := c.GetProjectQueues(project)[queueID]; ok {
		return name
	}

	url := fmt.Sprintf("%s/%s/%s/_apis/distributedtask/queues/%d?api-version=6.0", c.OrgURL, Collection, project, queueID)
	var queue distributedTaskQueue
	if err := c.Get(url, &queue); err == nil {
		if queue.Pool.Name != "" {
			return queue.Pool.Name
		}
		if queue.Name != "" {
			return queue.Name
		}
	}

	return fmt.Sprintf("Queue %d", queueID)
}
