package collector

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
)

// redisMetricsPerNode is the number of CloudWatch queries issued per ElastiCache node.
// Must match the addQuery calls in the per-node loop in Collect().
const redisMetricsPerNode = 8

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
	start := now.Add(-5 * time.Minute)
	period := int32(60)

	queries := []cwtypes.MetricDataQuery{}
	idx := 0

	addQuery := func(_, namespace, metric string, stat string, dims []cwtypes.Dimension) {
		queries = append(queries, cwtypes.MetricDataQuery{
			Id: aws.String(fmt.Sprintf("m%d", idx)),
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
		idx++
	}

	// DocDB Elastic metrics — requires BOTH ClusterId (UUID) AND ClusterName dimensions
	docdbDims := []cwtypes.Dimension{
		{Name: aws.String("ClusterId"), Value: aws.String(c.docdbID)},
		{Name: aws.String("ClusterName"), Value: aws.String(c.docdbName)},
	}
	addQuery("docdb_cpu", "AWS/DocDB-Elastic", "PrimaryInstanceCPUUtilization", "Average", docdbDims)
	addQuery("docdb_cursors", "AWS/DocDB-Elastic", "DatabaseCursorsTimedOut", "Sum", docdbDims)
	addQuery("docdb_conn", "AWS/DocDB-Elastic", "DatabaseConnections", "Average", docdbDims)
	addQuery("docdb_vol", "AWS/DocDB-Elastic", "VolumeBytesUsed", "Average", docdbDims)
	addQuery("docdb_insert", "AWS/DocDB-Elastic", "OpcountersInsert", "Sum", docdbDims)
	addQuery("docdb_query", "AWS/DocDB-Elastic", "OpcountersQuery", "Sum", docdbDims)
	addQuery("docdb_update", "AWS/DocDB-Elastic", "OpcountersUpdate", "Sum", docdbDims)
	addQuery("docdb_delete", "AWS/DocDB-Elastic", "OpcountersDelete", "Sum", docdbDims)
	addQuery("docdb_riops", "AWS/DocDB-Elastic", "ReadIOPS", "Average", docdbDims)
	addQuery("docdb_wiops", "AWS/DocDB-Elastic", "WriteIOPS", "Average", docdbDims)

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

	// ElastiCache metrics per node (8 queries each — keep redisMetricsPerNode in sync)
	for _, node := range c.redisNodes {
		dims := []cwtypes.Dimension{{Name: aws.String("CacheClusterId"), Value: aws.String(node)}}
		addQuery("redis_cpu_"+node, "AWS/ElastiCache", "CPUUtilization", "Average", dims)
		addQuery("redis_ecpu_"+node, "AWS/ElastiCache", "EngineCPUUtilization", "Maximum", dims)
		addQuery("redis_mem_"+node, "AWS/ElastiCache", "DatabaseMemoryUsagePercentage", "Average", dims)
		addQuery("redis_hit_"+node, "AWS/ElastiCache", "CacheHitRate", "Average", dims)
		addQuery("redis_evict_"+node, "AWS/ElastiCache", "Evictions", "Sum", dims)
		addQuery("redis_conn_"+node, "AWS/ElastiCache", "CurrConnections", "Average", dims)
		addQuery("redis_get_"+node, "AWS/ElastiCache", "GetTypeCmds", "Sum", dims)
		addQuery("redis_set_"+node, "AWS/ElastiCache", "SetTypeCmds", "Sum", dims)
	}

	// ALB metrics
	for _, alb := range c.albNames {
		dims := []cwtypes.Dimension{{Name: aws.String("LoadBalancer"), Value: aws.String(alb)}}
		addQuery("alb_rt_"+alb, "AWS/ApplicationELB", "TargetResponseTime", "p99", dims)
		addQuery("alb_5xx_"+alb, "AWS/ApplicationELB", "HTTPCode_ELB_5XX_Count", "Sum", dims)
		addQuery("alb_conn_"+alb, "AWS/ApplicationELB", "ActiveConnectionCount", "Sum", dims)
		addQuery("alb_req_"+alb, "AWS/ApplicationELB", "RequestCount", "Sum", dims)
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

	get := func(i int) float64 {
		id := fmt.Sprintf("m%d", i)
		return vals[id]
	}

	// DocDB (10 metrics: indices 0-9)
	snap.DocDB = DocDBMetrics{
		CPUPercent:      get(0),
		CursorsTimedOut: get(1),
		Connections:     get(2),
		VolumeUsed:      get(3),
		InsertOps:       get(4),
		QueryOps:        get(5),
		UpdateOps:       get(6),
		DeleteOps:       get(7),
		ReadIOPS:        get(8),
		WriteIOPS:       get(9),
	}

	// RDS (8 metrics: indices 10-17)
	snap.RDS = RDSMetrics{
		CPUPercent:   get(10),
		Connections:  get(11),
		FreeMemory:   get(12),
		ReadLatency:  get(13),
		WriteLatency: get(14),
		DiskQueue:    get(15),
		ReadIOPS:     get(16),
		WriteIOPS:    get(17),
	}

	// Redis (redisMetricsPerNode metrics per node: starting at index 18)
	base := 18
	for i, node := range c.redisNodes {
		offset := base + i*redisMetricsPerNode
		snap.Redis = append(snap.Redis, RedisNodeMetrics{
			NodeID:        node,
			Role:          c.redisRoles[node],
			CPUPercent:    get(offset),
			EngineCPU:     get(offset + 1),
			MemoryPercent: get(offset + 2),
			HitRate:       get(offset + 3),
			Evictions:     get(offset + 4),
			Connections:   get(offset + 5),
			GetTypeCmds:   get(offset + 6),
			SetTypeCmds:   get(offset + 7),
		})
	}

	// ALB — aggregate across all ALBs: sum counters, max of P99
	albBase := base + len(c.redisNodes)*redisMetricsPerNode
	for i := range c.albNames {
		offset := albBase + i*4
		p99 := get(offset)
		if p99 > snap.ALB.ResponseTimeP99 {
			snap.ALB.ResponseTimeP99 = p99
		}
		snap.ALB.Count5XX += get(offset + 1)
		snap.ALB.ActiveConns += get(offset + 2)
		snap.ALB.RequestCount += get(offset + 3)
	}

	// MSK consumer group lag — separate API call (may be cross-account)
	if c.mskClusterName != "" {
		c.collectMSK(&snap, start, now, period)
	}

	return snap
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
