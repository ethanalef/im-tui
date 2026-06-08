package collector

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
)

// redisMetricsPerNode is the number of CloudWatch queries issued per ElastiCache node.
// Must match the addQuery calls in the per-node loop in Collect().
const redisMetricsPerNode = 9

const docdbNamespace = "AWS/DocDB-Elastic"
const cloudWatchLookback = 30 * time.Minute

type docDBShardMetric struct {
	ShardID string
	Dims    []cwtypes.Dimension
}

// MSKConsumerGroupConfig pairs a Kafka consumer group with its topic for CloudWatch queries.
type MSKConsumerGroupConfig struct {
	Group string
	Topic string
}

type CloudWatchCollector struct {
	client            *cloudwatch.Client
	mskClient         *cloudwatch.Client // separate client for MSK (may be cross-account)
	docdbID           string
	docdbName         string
	docdbShardMetrics []docDBShardMetric
	docdbShardChecked bool
	rdsID             string
	redisNodes        []string
	redisRoles        map[string]string // node ID -> "primary"/"replica" (from replication group)
	albNames          []string
	mskClusterName    string
	mskConsumerGroups []MSKConsumerGroupConfig
}

func NewCloudWatchCollector(region, docdbID, docdbName, rdsID string, redisNodes, albNames []string, redisRoles map[string]string, mskClusterName, mskAWSProfile string, mskConsumerGroups []MSKConsumerGroupConfig) (*CloudWatchCollector, error) {
	cfg, err := config.LoadDefaultConfig(context.Background(), config.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("loading AWS config: %w", err)
	}

	c := &CloudWatchCollector{
		client:            cloudwatch.NewFromConfig(cfg),
		mskClient:         cloudwatch.NewFromConfig(cfg), // default: same client
		docdbID:           docdbID,
		docdbName:         docdbName,
		rdsID:             rdsID,
		redisNodes:        redisNodes,
		redisRoles:        redisRoles,
		albNames:          albNames,
		mskClusterName:    mskClusterName,
		mskConsumerGroups: mskConsumerGroups,
	}

	// If MSK uses a different AWS profile (cross-account), create a separate client
	if mskAWSProfile != "" && mskClusterName != "" {
		mskCfg, err := config.LoadDefaultConfig(context.Background(),
			config.WithRegion(region),
			config.WithSharedConfigProfile(mskAWSProfile),
		)
		if err != nil {
			return nil, fmt.Errorf("loading MSK AWS config (profile %s): %w", mskAWSProfile, err)
		}
		c.mskClient = cloudwatch.NewFromConfig(mskCfg)
	}

	return c, nil
}

func (c *CloudWatchCollector) Collect() CloudWatchSnapshot {
	snap := CloudWatchSnapshot{}
	now := time.Now()
	start := now.Add(-cloudWatchLookback)
	period := int32(60)

	queries := []cwtypes.MetricDataQuery{}
	idx := 0
	queryIDs := map[string]string{}

	addQuery := func(label, namespace, metric string, stat string, dims []cwtypes.Dimension) {
		id := fmt.Sprintf("m%d", idx)
		queries = append(queries, cwtypes.MetricDataQuery{
			Id: aws.String(id),
			MetricStat: &cwtypes.MetricStat{
				Metric: &cwtypes.Metric{
					Namespace:  aws.String(namespace),
					MetricName: aws.String(metric),
					Dimensions: dims,
				},
				Period: &period,
				Stat:   aws.String(stat),
			},
		})
		queryIDs[label] = id
		idx++
	}

	// DocDB Elastic metrics — requires BOTH ClusterId (UUID) AND ClusterName dimensions
	docdbDims := []cwtypes.Dimension{
		{Name: aws.String("ClusterId"), Value: aws.String(c.docdbID)},
		{Name: aws.String("ClusterName"), Value: aws.String(c.docdbName)},
	}
	addDocDBAggregateQueries := func(prefix string, dims []cwtypes.Dimension) {
		addQuery(prefix+"_cpu", docdbNamespace, "PrimaryInstanceCPUUtilization", "Average", dims)
		addQuery(prefix+"_pfreemem", docdbNamespace, "PrimaryInstanceFreeableMemory", "Average", dims)
		addQuery(prefix+"_repcpu", docdbNamespace, "ReplicaInstanceCPUUtilization", "Average", dims)
		addQuery(prefix+"_rfreemem", docdbNamespace, "ReplicaInstanceFreeableMemory", "Average", dims)
		addQuery(prefix+"_cursors", docdbNamespace, "DatabaseCursorsTimedOut", "Sum", dims)
		addQuery(prefix+"_conn", docdbNamespace, "DatabaseConnections", "Average", dims)
		addQuery(prefix+"_vol", docdbNamespace, "VolumeBytesUsed", "Average", dims)
		addQuery(prefix+"_insert", docdbNamespace, "OpcountersInsert", "Sum", dims)
		addQuery(prefix+"_query", docdbNamespace, "OpcountersQuery", "Sum", dims)
		addQuery(prefix+"_update", docdbNamespace, "OpcountersUpdate", "Sum", dims)
		addQuery(prefix+"_delete", docdbNamespace, "OpcountersDelete", "Sum", dims)
		addQuery(prefix+"_riops", docdbNamespace, "VolumeReadIOPs", "Average", dims)
		addQuery(prefix+"_wiops", docdbNamespace, "VolumeWriteIOPs", "Average", dims)
	}
	addDocDBShardQueries := func(prefix string, dims []cwtypes.Dimension) {
		addQuery(prefix+"_cpu", docdbNamespace, "PrimaryInstanceCPUUtilization", "Average", dims)
		addQuery(prefix+"_pfreemem", docdbNamespace, "PrimaryInstanceFreeableMemory", "Average", dims)
		addQuery(prefix+"_repcpu", docdbNamespace, "ReplicaInstanceCPUUtilization", "Average", dims)
		addQuery(prefix+"_rfreemem", docdbNamespace, "ReplicaInstanceFreeableMemory", "Average", dims)
		addQuery(prefix+"_cursors", docdbNamespace, "DatabaseCursorsTimedOut", "Sum", dims)
		addQuery(prefix+"_vol", docdbNamespace, "VolumeBytesUsed", "Average", dims)
		addQuery(prefix+"_insert", docdbNamespace, "DocumentsInserted", "Sum", dims)
		addQuery(prefix+"_query", docdbNamespace, "DocumentsReturned", "Sum", dims)
		addQuery(prefix+"_update", docdbNamespace, "DocumentsUpdated", "Sum", dims)
		addQuery(prefix+"_delete", docdbNamespace, "DocumentsDeleted", "Sum", dims)
		addQuery(prefix+"_riops", docdbNamespace, "VolumeReadIOPs", "Average", dims)
		addQuery(prefix+"_wiops", docdbNamespace, "VolumeWriteIOPs", "Average", dims)
		addQuery(prefix+"_replag", docdbNamespace, "DBInstanceReplicaLag", "Maximum", dims)
	}
	addDocDBAggregateQueries("docdb", docdbDims)
	shardCtx, shardCancel := context.WithTimeout(context.Background(), 5*time.Second)
	shards := c.discoverDocDBShards(shardCtx)
	shardCancel()
	for i, shard := range shards {
		addDocDBShardQueries(fmt.Sprintf("docdb_shard_%d", i), shard.Dims)
	}

	// RDS metrics
	rdsDims := []cwtypes.Dimension{{Name: aws.String("DBInstanceIdentifier"), Value: aws.String(c.rdsID)}}
	addQuery("rds_cpu", "AWS/RDS", "CPUUtilization", "Average", rdsDims)
	addQuery("rds_conn", "AWS/RDS", "DatabaseConnections", "Average", rdsDims)
	addQuery("rds_mem", "AWS/RDS", "FreeableMemory", "Average", rdsDims)
	addQuery("rds_rlat", "AWS/RDS", "ReadLatency", "Average", rdsDims)
	addQuery("rds_wlat", "AWS/RDS", "WriteLatency", "Average", rdsDims)
	addQuery("rds_dq", "AWS/RDS", "DiskQueueDepth", "Average", rdsDims)
	addQuery("rds_riops", "AWS/RDS", "ReadIOPS", "Average", rdsDims)
	addQuery("rds_wiops", "AWS/RDS", "WriteIOPS", "Average", rdsDims)

	// ElastiCache metrics per node (keep redisMetricsPerNode in sync)
	for _, node := range c.redisNodes {
		prefix := "redis_" + node
		dims := []cwtypes.Dimension{{Name: aws.String("CacheClusterId"), Value: aws.String(node)}}
		addQuery(prefix+"_cpu", "AWS/ElastiCache", "CPUUtilization", "Average", dims)
		addQuery(prefix+"_ecpu", "AWS/ElastiCache", "EngineCPUUtilization", "Maximum", dims)
		addQuery(prefix+"_mem", "AWS/ElastiCache", "DatabaseMemoryUsagePercentage", "Average", dims)
		addQuery(prefix+"_hit", "AWS/ElastiCache", "CacheHitRate", "Average", dims)
		addQuery(prefix+"_evict", "AWS/ElastiCache", "Evictions", "Sum", dims)
		addQuery(prefix+"_conn", "AWS/ElastiCache", "CurrConnections", "Average", dims)
		addQuery(prefix+"_get", "AWS/ElastiCache", "GetTypeCmds", "Sum", dims)
		addQuery(prefix+"_set", "AWS/ElastiCache", "SetTypeCmds", "Sum", dims)
		addQuery(prefix+"_replag", "AWS/ElastiCache", "ReplicationLag", "Maximum", dims)
	}

	// ALB metrics
	for _, alb := range c.albNames {
		prefix := "alb_" + alb
		dims := []cwtypes.Dimension{{Name: aws.String("LoadBalancer"), Value: aws.String(alb)}}
		addQuery(prefix+"_rt", "AWS/ApplicationELB", "TargetResponseTime", "p99", dims)
		addQuery(prefix+"_5xx", "AWS/ApplicationELB", "HTTPCode_ELB_5XX_Count", "Sum", dims)
		addQuery(prefix+"_conn", "AWS/ApplicationELB", "ActiveConnectionCount", "Sum", dims)
		addQuery(prefix+"_req", "AWS/ApplicationELB", "RequestCount", "Sum", dims)
	}

	if len(queries) == 0 {
		return snap
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	out, err := c.client.GetMetricData(ctx, &cloudwatch.GetMetricDataInput{
		StartTime:         &start,
		EndTime:           &now,
		MetricDataQueries: queries,
		ScanBy:            cwtypes.ScanByTimestampDescending,
	})
	if err != nil {
		snap.Err = fmt.Errorf("GetMetricData: %w", err)
		return snap
	}

	// Build lookup map: id -> latest value
	vals := make(map[string]float64)
	for _, r := range out.MetricDataResults {
		if r.Id != nil && len(r.Values) > 0 {
			vals[*r.Id] = r.Values[0]
		}
	}

	get := func(label string) float64 {
		return vals[queryIDs[label]]
	}
	has := func(label string) bool {
		_, ok := vals[queryIDs[label]]
		return ok
	}

	readDocDBAggregateMetrics := func(prefix, shardID string) DocDBMetrics {
		return DocDBMetrics{
			ShardID:         shardID,
			CPUPercent:      get(prefix + "_cpu"),
			PrimaryFreeMem:  get(prefix + "_pfreemem"),
			ReplicaCPU:      get(prefix + "_repcpu"),
			ReplicaFreeMem:  get(prefix + "_rfreemem"),
			CursorsTimedOut: get(prefix + "_cursors"),
			Connections:     get(prefix + "_conn"),
			VolumeUsed:      get(prefix + "_vol"),
			InsertOps:       get(prefix + "_insert"),
			QueryOps:        get(prefix + "_query"),
			UpdateOps:       get(prefix + "_update"),
			DeleteOps:       get(prefix + "_delete"),
			ReadIOPS:        get(prefix + "_riops"),
			WriteIOPS:       get(prefix + "_wiops"),
			HasDocumentOps:  has(prefix+"_insert") || has(prefix+"_query") || has(prefix+"_update") || has(prefix+"_delete"),
			HasReadIOPS:     has(prefix + "_riops"),
			HasWriteIOPS:    has(prefix + "_wiops"),
			HasPrimaryFree:  has(prefix + "_pfreemem"),
			HasReplicaCPU:   has(prefix + "_repcpu"),
			HasReplicaFree:  has(prefix + "_rfreemem"),
		}
	}
	readDocDBShardMetrics := func(prefix, shardID string) DocDBMetrics {
		return DocDBMetrics{
			ShardID:         shardID,
			CPUPercent:      get(prefix + "_cpu"),
			PrimaryFreeMem:  get(prefix + "_pfreemem"),
			ReplicaCPU:      get(prefix + "_repcpu"),
			ReplicaFreeMem:  get(prefix + "_rfreemem"),
			CursorsTimedOut: get(prefix + "_cursors"),
			VolumeUsed:      get(prefix + "_vol"),
			InsertOps:       get(prefix + "_insert"),
			QueryOps:        get(prefix + "_query"),
			UpdateOps:       get(prefix + "_update"),
			DeleteOps:       get(prefix + "_delete"),
			ReadIOPS:        get(prefix + "_riops"),
			WriteIOPS:       get(prefix + "_wiops"),
			ReplicaLagMS:    get(prefix + "_replag"),
			HasDocumentOps:  has(prefix+"_insert") || has(prefix+"_query") || has(prefix+"_update") || has(prefix+"_delete"),
			HasReadIOPS:     has(prefix + "_riops"),
			HasWriteIOPS:    has(prefix + "_wiops"),
			HasReplicaLag:   has(prefix + "_replag"),
			HasPrimaryFree:  has(prefix + "_pfreemem"),
			HasReplicaCPU:   has(prefix + "_repcpu"),
			HasReplicaFree:  has(prefix + "_rfreemem"),
		}
	}
	snap.DocDB = readDocDBAggregateMetrics("docdb", "")
	for i, shard := range c.docdbShardMetrics {
		snap.DocDBShards = append(snap.DocDBShards, readDocDBShardMetrics(fmt.Sprintf("docdb_shard_%d", i), shard.ShardID))
	}

	snap.RDS = RDSMetrics{
		CPUPercent:   get("rds_cpu"),
		Connections:  get("rds_conn"),
		FreeMemory:   get("rds_mem"),
		ReadLatency:  get("rds_rlat"),
		WriteLatency: get("rds_wlat"),
		DiskQueue:    get("rds_dq"),
		ReadIOPS:     get("rds_riops"),
		WriteIOPS:    get("rds_wiops"),
	}

	for _, node := range c.redisNodes {
		prefix := "redis_" + node
		snap.Redis = append(snap.Redis, RedisNodeMetrics{
			NodeID:            node,
			Role:              c.redisRoles[node],
			CPUPercent:        get(prefix + "_cpu"),
			EngineCPU:         get(prefix + "_ecpu"),
			MemoryPercent:     get(prefix + "_mem"),
			HitRate:           get(prefix + "_hit"),
			Evictions:         get(prefix + "_evict"),
			Connections:       get(prefix + "_conn"),
			GetTypeCmds:       get(prefix + "_get"),
			SetTypeCmds:       get(prefix + "_set"),
			ReplicationLag:    get(prefix + "_replag"),
			HasReplicationLag: has(prefix + "_replag"),
		})
	}

	// ALB — aggregate across all ALBs: sum counters, max of P99
	for _, alb := range c.albNames {
		prefix := "alb_" + alb
		p99 := get(prefix + "_rt")
		if p99 > snap.ALB.ResponseTimeP99 {
			snap.ALB.ResponseTimeP99 = p99
		}
		snap.ALB.Count5XX += get(prefix + "_5xx")
		snap.ALB.ActiveConns += get(prefix + "_conn")
		snap.ALB.RequestCount += get(prefix + "_req")
	}

	// MSK consumer group lag — separate API call (may be cross-account)
	if c.mskClusterName != "" {
		c.collectMSK(&snap, start, now, period)
	}

	return snap
}

func (c *CloudWatchCollector) discoverDocDBShards(ctx context.Context) []docDBShardMetric {
	if c.docdbShardChecked || c.docdbID == "" || c.docdbName == "" {
		return c.docdbShardMetrics
	}
	c.docdbShardChecked = true

	out, err := c.client.ListMetrics(ctx, &cloudwatch.ListMetricsInput{
		Namespace:  aws.String(docdbNamespace),
		MetricName: aws.String("PrimaryInstanceCPUUtilization"),
		Dimensions: []cwtypes.DimensionFilter{
			{Name: aws.String("ClusterId"), Value: aws.String(c.docdbID)},
			{Name: aws.String("ClusterName"), Value: aws.String(c.docdbName)},
			{Name: aws.String("ShardId")},
		},
	})
	if err != nil {
		return c.docdbShardMetrics
	}

	seen := map[string][]cwtypes.Dimension{}
	for _, metric := range out.Metrics {
		shardID := dimensionValue(metric.Dimensions, "ShardId")
		if shardID == "" {
			continue
		}
		seen[shardID] = metric.Dimensions
	}

	var shards []docDBShardMetric
	for shardID, dims := range seen {
		shards = append(shards, docDBShardMetric{ShardID: shardID, Dims: dims})
	}
	sort.Slice(shards, func(i, j int) bool {
		left, leftErr := strconv.Atoi(shards[i].ShardID)
		right, rightErr := strconv.Atoi(shards[j].ShardID)
		if leftErr == nil && rightErr == nil {
			return left < right
		}
		return shards[i].ShardID < shards[j].ShardID
	})
	c.docdbShardMetrics = shards
	return c.docdbShardMetrics
}

func dimensionValue(dims []cwtypes.Dimension, name string) string {
	for _, d := range dims {
		if aws.ToString(d.Name) == name {
			return aws.ToString(d.Value)
		}
	}
	return ""
}

// collectMSK queries MSK consumer group lag using the MSK-specific client (may be cross-account).
func (c *CloudWatchCollector) collectMSK(snap *CloudWatchSnapshot, start, end time.Time, period int32) {
	var queries []cwtypes.MetricDataQuery
	for i, cg := range c.mskConsumerGroups {
		queries = append(queries, cwtypes.MetricDataQuery{
			Id: aws.String(fmt.Sprintf("msk%d", i)),
			MetricStat: &cwtypes.MetricStat{
				Metric: &cwtypes.Metric{
					Namespace:  aws.String("AWS/Kafka"),
					MetricName: aws.String("SumOffsetLag"),
					Dimensions: []cwtypes.Dimension{
						{Name: aws.String("Cluster Name"), Value: aws.String(c.mskClusterName)},
						{Name: aws.String("Consumer Group"), Value: aws.String(cg.Group)},
						{Name: aws.String("Topic"), Value: aws.String(cg.Topic)},
					},
				},
				Period: &period,
				Stat:   aws.String("Maximum"),
			},
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	out, err := c.mskClient.GetMetricData(ctx, &cloudwatch.GetMetricDataInput{
		StartTime:         &start,
		EndTime:           &end,
		MetricDataQueries: queries,
	})
	if err != nil {
		// Don't fail the whole snapshot for MSK errors
		return
	}

	vals := make(map[string]float64)
	for _, r := range out.MetricDataResults {
		if r.Id != nil && len(r.Values) > 0 {
			vals[*r.Id] = r.Values[0]
		}
	}

	for i, cg := range c.mskConsumerGroups {
		lag := vals[fmt.Sprintf("msk%d", i)]
		snap.MSK.ConsumerLag = append(snap.MSK.ConsumerLag, ConsumerGroupLag{
			Group: cg.Group,
			Topic: cg.Topic,
			Lag:   lag,
		})
		snap.MSK.TotalLag += lag
	}
}

// IsReachable tests if we can call CloudWatch.
func (c *CloudWatchCollector) IsReachable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := c.client.ListMetrics(ctx, &cloudwatch.ListMetricsInput{
		Namespace: aws.String("AWS/RDS"),
	})
	return err == nil
}
