package collector

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
)

// Redis node roles.
const (
	RedisRolePrimary = "primary"
	RedisRoleReplica = "replica"
)

// RedisTopology describes the live members of an ElastiCache replication group.
// Nodes are ordered with the primary first, then replicas by node ID.
type RedisTopology struct {
	Nodes []string          // ordered member cluster IDs
	Roles map[string]string // node ID -> RedisRolePrimary / RedisRoleReplica
}

// DiscoverRedisTopology resolves the live member clusters and their roles for a
// replication group via DescribeReplicationGroups. This adapts automatically as
// replicas are added or removed (e.g. 1P4R -> 1P5R) without changing config.
//
// On any error (no replication group ID, AWS failure, empty result) it returns an
// empty topology so callers can fall back to a static node list from config.
func DiscoverRedisTopology(region, replicationGroupID string) RedisTopology {
	empty := RedisTopology{Roles: map[string]string{}}
	if replicationGroupID == "" {
		return empty
	}

	cfg, err := config.LoadDefaultConfig(context.Background(), config.WithRegion(region))
	if err != nil {
		return empty
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client := elasticache.NewFromConfig(cfg)
	out, err := client.DescribeReplicationGroups(ctx, &elasticache.DescribeReplicationGroupsInput{
		ReplicationGroupId: aws.String(replicationGroupID),
	})
	if err != nil || len(out.ReplicationGroups) == 0 {
		return empty
	}

	rg := out.ReplicationGroups[0]
	roles := make(map[string]string)

	// Roles come from NodeGroupMembers.CurrentRole (cluster-mode-disabled groups).
	for _, ng := range rg.NodeGroups {
		for _, m := range ng.NodeGroupMembers {
			if m.CacheClusterId == nil {
				continue
			}
			role := RedisRoleReplica
			if m.CurrentRole != nil && *m.CurrentRole == RedisRolePrimary {
				role = RedisRolePrimary
			}
			roles[*m.CacheClusterId] = role
		}
	}

	// Order: primary first, then the remaining members in MemberClusters order.
	var primary string
	var replicas []string
	for _, id := range rg.MemberClusters {
		if roles[id] == RedisRolePrimary {
			primary = id
			continue
		}
		if _, ok := roles[id]; !ok {
			roles[id] = RedisRoleReplica // default any unclassified member to replica
		}
		replicas = append(replicas, id)
	}

	nodes := make([]string, 0, len(rg.MemberClusters))
	if primary != "" {
		nodes = append(nodes, primary)
	}
	nodes = append(nodes, replicas...)

	if len(nodes) == 0 {
		return empty
	}
	return RedisTopology{Nodes: nodes, Roles: roles}
}
