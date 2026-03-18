package collector

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	"github.com/aws/aws-sdk-go-v2/service/rds"
)

// FetchInfraSpecs retrieves static infrastructure specs from AWS Describe APIs.
// Called once at startup — these don't change during a TUI session.
// DocDB specs come from config (GetCluster API requires IAM permissions we don't have).
func FetchInfraSpecs(region, rdsInstanceID string, redisNodes []string, docdbSpec DocDBSpec) InfraSpecs {
	specs := InfraSpecs{
		DocDB: docdbSpec,
	}

	cfg, err := config.LoadDefaultConfig(context.Background(), config.WithRegion(region))
	if err != nil {
		return specs
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	specs.RDS = fetchRDSSpec(ctx, cfg, rdsInstanceID)
	specs.Redis = fetchRedisSpecs(ctx, cfg, redisNodes)

	return specs
}

func fetchRDSSpec(ctx context.Context, cfg aws.Config, instanceID string) RDSSpec {
	if instanceID == "" {
		return RDSSpec{}
	}

	client := rds.NewFromConfig(cfg)
	out, err := client.DescribeDBInstances(ctx, &rds.DescribeDBInstancesInput{
		DBInstanceIdentifier: aws.String(instanceID),
	})
	if err != nil || len(out.DBInstances) == 0 {
		return RDSSpec{}
	}

	db := out.DBInstances[0]
	spec := RDSSpec{
		MultiAZ: aws.ToBool(db.MultiAZ),
	}
	if db.DBInstanceClass != nil {
		spec.InstanceClass = *db.DBInstanceClass
	}
	if db.Engine != nil {
		spec.Engine = *db.Engine
	}
	if db.EngineVersion != nil {
		spec.EngineVersion = *db.EngineVersion
	}
	if db.AllocatedStorage != nil {
		spec.AllocatedStorage = *db.AllocatedStorage
	}
	if db.MaxAllocatedStorage != nil {
		spec.MaxStorage = *db.MaxAllocatedStorage
	}
	if db.StorageType != nil {
		spec.StorageType = *db.StorageType
	}
	return spec
}

func fetchRedisSpecs(ctx context.Context, cfg aws.Config, nodes []string) []RedisNodeSpec {
	if len(nodes) == 0 {
		return nil
	}

	client := elasticache.NewFromConfig(cfg)

	var specs []RedisNodeSpec
	for _, nodeID := range nodes {
		out, err := client.DescribeCacheClusters(ctx, &elasticache.DescribeCacheClustersInput{
			CacheClusterId: aws.String(nodeID),
		})
		if err != nil || len(out.CacheClusters) == 0 {
			specs = append(specs, RedisNodeSpec{NodeID: nodeID})
			continue
		}

		cc := out.CacheClusters[0]
		s := RedisNodeSpec{NodeID: nodeID}
		if cc.CacheNodeType != nil {
			s.NodeType = *cc.CacheNodeType
		}
		if cc.Engine != nil {
			s.Engine = *cc.Engine
		}
		if cc.EngineVersion != nil {
			s.EngineVersion = *cc.EngineVersion
		}
		specs = append(specs, s)
	}
	return specs
}

// FormatRDSSpec returns a one-line summary of RDS instance specs.
func FormatRDSSpec(s RDSSpec) string {
	if s.InstanceClass == "" {
		return ""
	}
	line := fmt.Sprintf("%s  %s %s", s.InstanceClass, s.Engine, s.EngineVersion)
	storage := fmt.Sprintf("  %dGiB %s", s.AllocatedStorage, s.StorageType)
	if s.MaxStorage > 0 && s.MaxStorage != s.AllocatedStorage {
		storage = fmt.Sprintf("  %d/%dGiB %s", s.AllocatedStorage, s.MaxStorage, s.StorageType)
	}
	line += storage
	if s.MultiAZ {
		line += "  Multi-AZ"
	}
	return line
}

// FormatDocDBSpec returns a one-line summary of DocumentDB Elastic cluster specs.
func FormatDocDBSpec(s DocDBSpec) string {
	if s.ShardCapacity == 0 {
		return ""
	}
	return fmt.Sprintf("%d shard(s) x %d vCPU", s.ShardCount, s.ShardCapacity)
}

// FormatRedisSpec returns a one-line summary of a Redis node spec.
func FormatRedisSpec(s RedisNodeSpec) string {
	if s.NodeType == "" {
		return ""
	}
	return fmt.Sprintf("%s  %s %s", s.NodeType, s.Engine, s.EngineVersion)
}
